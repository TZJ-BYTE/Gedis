package tests

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
	"github.com/TZJ-BYTE/RediGo/internal/server"
)

func TestGnetServer(t *testing.T) {
	// 随机端口
	port := 16380
	cfg := &config.Config{
		Host:        "127.0.0.1",
		Port:        port,
		NetworkType: "gnet",
		DBCount:     16,
	}

	srv := server.NewServer(cfg)

	go func() {
		if err := srv.Start(); err != nil {
			// 如果 gnet 启动失败（可能是端口占用或环境问题），记录日志但不 fail
			// 因为测试环境可能受限
			t.Logf("Server start error: %v", err)
		}
	}()

	// 等待启动
	time.Sleep(1 * time.Second)
	defer srv.Stop()

	// 连接
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// 发送 PING
	writeAll(t, conn, []byte("*1\r\n$4\r\nPING\r\n"))

	var rbuf bytes.Buffer
	v := readResp(t, conn, &rbuf)
	if s, ok := v.(string); !ok || s != "PONG" {
		t.Fatalf("PING resp=%#v", v)
	}

	// 测试 SELECT
	writeAll(t, conn, []byte("*2\r\n$6\r\nSELECT\r\n$1\r\n1\r\n"))
	v = readResp(t, conn, &rbuf)
	if s, ok := v.(string); !ok || s != "OK" {
		t.Fatalf("SELECT resp=%#v", v)
	}

	// SET k1 hello
	writeAll(t, conn, []byte("*3\r\n$3\r\nSET\r\n$2\r\nk1\r\n$5\r\nhello\r\n"))
	v = readResp(t, conn, &rbuf)
	if s, ok := v.(string); !ok || s != "OK" {
		t.Fatalf("SET resp=%#v", v)
	}

	// MGET k1 k2(missing) => ["hello", nil]
	writeAll(t, conn, []byte("*3\r\n$4\r\nMGET\r\n$2\r\nk1\r\n$2\r\nk2\r\n"))
	v = readResp(t, conn, &rbuf)
	arr, ok := v.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "hello" || arr[1] != nil {
		t.Fatalf("MGET resp=%#v", v)
	}

	// HSET h f1 v1
	writeAll(t, conn, []byte("*4\r\n$4\r\nHSET\r\n$1\r\nh\r\n$2\r\nf1\r\n$2\r\nv1\r\n"))
	v = readResp(t, conn, &rbuf)
	if n, ok := v.(int64); !ok || n != 1 {
		t.Fatalf("HSET resp=%#v", v)
	}

	// HMGET h f1 f2(missing) => ["v1", nil]
	writeAll(t, conn, []byte("*4\r\n$5\r\nHMGET\r\n$1\r\nh\r\n$2\r\nf1\r\n$2\r\nf2\r\n"))
	v = readResp(t, conn, &rbuf)
	arr, ok = v.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "v1" || arr[1] != nil {
		t.Fatalf("HMGET resp=%#v", v)
	}

	// SET with options should not be intercepted by fastpath
	writeAll(t, conn, []byte("*5\r\n$3\r\nSET\r\n$2\r\nk2\r\n$5\r\nhello\r\n$2\r\nEX\r\n$1\r\n1\r\n"))
	v = readResp(t, conn, &rbuf)
	if s, ok := v.(string); !ok || s != "OK" {
		t.Fatalf("SET EX resp=%#v", v)
	}

	// FLUSHALL clears across dbs
	writeAll(t, conn, []byte("*1\r\n$8\r\nFLUSHALL\r\n"))
	v = readResp(t, conn, &rbuf)
	if s, ok := v.(string); !ok || s != "OK" {
		t.Fatalf("FLUSHALL resp=%#v", v)
	}

	// After FLUSHALL, key should be nil
	writeAll(t, conn, []byte("*2\r\n$3\r\nGET\r\n$2\r\nk1\r\n"))
	v = readResp(t, conn, &rbuf)
	if v != nil {
		t.Fatalf("GET after flushall resp=%#v", v)
	}
}

func writeAll(t *testing.T, conn net.Conn, p []byte) {
	t.Helper()
	for len(p) > 0 {
		n, err := conn.Write(p)
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		p = p[n:]
	}
}

func readExpect(t *testing.T, conn net.Conn, rbuf *bytes.Buffer, expected []byte) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	tmp := make([]byte, 4096)
	for rbuf.Len() < len(expected) {
		n, err := conn.Read(tmp)
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
		rbuf.Write(tmp[:n])
	}
	got := rbuf.Bytes()[:len(expected)]
	if !bytes.Equal(got, expected) {
		t.Fatalf("Expected %q, got %q", string(expected), string(rbuf.Bytes()))
	}
	rbuf.Next(len(expected))
}

func readResp(t *testing.T, conn net.Conn, rbuf *bytes.Buffer) interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	tmp := make([]byte, 4096)
	for {
		v, n, ok, err := parseResp(rbuf.Bytes())
		if err != nil {
			t.Fatalf("parse err: %v", err)
		}
		if ok {
			rbuf.Next(n)
			return v
		}
		nr, err := conn.Read(tmp)
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
		rbuf.Write(tmp[:nr])
	}
}

func parseResp(b []byte) (interface{}, int, bool, error) {
	if len(b) < 1 {
		return nil, 0, false, nil
	}
	switch b[0] {
	case '+', '-', ':', '$', '*':
	default:
		return nil, 0, false, nil
	}

	readLine := func(off int) ([]byte, int, bool) {
		i := bytes.Index(b[off:], []byte("\r\n"))
		if i < 0 {
			return nil, 0, false
		}
		line := b[off : off+i]
		return line, off + i + 2, true
	}

	switch b[0] {
	case '+':
		line, next, ok := readLine(1)
		if !ok {
			return nil, 0, false, nil
		}
		return string(line), next, true, nil
	case '-':
		line, next, ok := readLine(1)
		if !ok {
			return nil, 0, false, nil
		}
		return fmt.Errorf("%s", string(line)), next, true, nil
	case ':':
		line, next, ok := readLine(1)
		if !ok {
			return nil, 0, false, nil
		}
		n, err := protocol.ParseInt(line)
		if err != nil {
			return nil, 0, false, err
		}
		return int64(n), next, true, nil
	case '$':
		line, next, ok := readLine(1)
		if !ok {
			return nil, 0, false, nil
		}
		n, err := protocol.ParseInt(line)
		if err != nil {
			return nil, 0, false, err
		}
		if n == -1 {
			return nil, next, true, nil
		}
		if n < 0 {
			return nil, 0, false, fmt.Errorf("invalid bulk len")
		}
		end := next + n + 2
		if end > len(b) {
			return nil, 0, false, nil
		}
		if b[next+n] != '\r' || b[next+n+1] != '\n' {
			return nil, 0, false, fmt.Errorf("invalid bulk trailer")
		}
		return string(b[next : next+n]), end, true, nil
	case '*':
		line, next, ok := readLine(1)
		if !ok {
			return nil, 0, false, nil
		}
		n, err := protocol.ParseInt(line)
		if err != nil {
			return nil, 0, false, err
		}
		if n == -1 {
			return nil, next, true, nil
		}
		if n < 0 {
			return nil, 0, false, fmt.Errorf("invalid array len")
		}
		out := make([]interface{}, 0, n)
		off := next
		for i := 0; i < n; i++ {
			v, nn, ok, err := parseResp(b[off:])
			if err != nil {
				return nil, 0, false, err
			}
			if !ok {
				return nil, 0, false, nil
			}
			out = append(out, v)
			off += nn
		}
		return out, off, true, nil
	default:
		return nil, 0, false, nil
	}
}
