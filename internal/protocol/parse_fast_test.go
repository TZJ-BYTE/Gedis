package protocol

import "testing"

func TestParseOneRequestFast_HMGET(t *testing.T) {
	b := []byte("*4\r\n$5\r\nHMGET\r\n$1\r\nh\r\n$2\r\nf1\r\n$2\r\nf2\r\n")
	req, n, err := ParseOneRequestFast(b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n == 0 || req == nil {
		t.Fatalf("n=%d req=%v", n, req)
	}
	if req.Cmd != "HMGET" {
		t.Fatalf("cmd=%q", req.Cmd)
	}
	if len(req.Args) != 3 {
		t.Fatalf("args=%d", len(req.Args))
	}
	ReleaseRequest(req)
}

