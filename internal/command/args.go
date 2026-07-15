package command

import (
	"bytes"

	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

func argString(args [][]byte, i int) string {
	return protocol.BytesToString(args[i])
}

func argStringCopy(args [][]byte, i int) string {
	return protocol.BytesToStringCopy(args[i])
}

func equalsIgnoreCase(a []byte, b []byte) bool {
	return bytes.EqualFold(a, b)
}
