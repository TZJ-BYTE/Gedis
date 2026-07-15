package protocol

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseOneRequestFast_RejectsOversizedBulk(t *testing.T) {
	SetParseLimits(4, 0)
	defer SetParseLimits(0, 0)

	_, _, err := ParseOneRequestFast([]byte("*1\r\n$5\r\nHELLO\r\n"))
	if err == nil || !strings.Contains(err.Error(), "invalid bulk string length") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseOneRequestFast_RejectsOversizedArray(t *testing.T) {
	SetParseLimits(0, 1)
	defer SetParseLimits(0, 0)

	_, _, err := ParseOneRequestFast([]byte("*2\r\n$3\r\nGET\r\n$1\r\na\r\n"))
	if err == nil || !strings.Contains(err.Error(), "invalid array length") {
		t.Fatalf("err=%v", err)
	}
}

func TestParser_RejectsOversizedBulkWithoutReadingBody(t *testing.T) {
	SetParseLimits(4, 0)
	defer SetParseLimits(0, 0)

	p := NewParser(bytes.NewBufferString("*1\r\n$5\r\n"))
	_, err := p.ParseRequest()
	if err == nil || !strings.Contains(err.Error(), "invalid bulk string length") {
		t.Fatalf("err=%v", err)
	}
}
