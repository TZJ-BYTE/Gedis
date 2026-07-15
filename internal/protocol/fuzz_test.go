package protocol

import "testing"

func FuzzParseOneRequestFast(f *testing.F) {
	SetParseLimits(1<<20, 1<<20)
	f.Add([]byte("*1\r\n$4\r\nPING\r\n"))
	f.Add([]byte("*2\r\n$3\r\nGET\r\n$1\r\na\r\n"))
	f.Add([]byte("*3\r\n$3\r\nSET\r\n$1\r\na\r\n$1\r\nb\r\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = ParseOneRequestFast(data)
		_, _, _ = ParseOneRequest(data)
	})
}
