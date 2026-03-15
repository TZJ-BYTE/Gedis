package tests

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/TZJ-BYTE/RediGo/config"
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
	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	response := string(buf[:n])
	if response != "+PONG\r\n" {
		t.Errorf("Expected +PONG\\r\\n, got %q", response)
	}

	// 测试 SELECT
	_, err = conn.Write([]byte("*2\r\n$6\r\nSELECT\r\n$1\r\n1\r\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	response = string(buf[:n])
	if response != "+OK\r\n" {
		t.Errorf("Expected +OK\\r\\n, got %q", response)
	}
}
