package server

import (
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

func fastPathExecute(dst []byte, db *database.Database, cmd string, args [][]byte) ([]byte, bool) {
	switch cmd {
	case "GET":
		if len(args) != 1 {
			return protocol.AppendErrorString(dst, "ERR wrong number of arguments for 'get' command"), true
		}
		b, ok, err := db.GetStringBytesCopy(args[0])
		if err != nil {
			return protocol.AppendErrorString(dst, err.Error()), true
		}
		if !ok {
			return protocol.AppendNull(dst), true
		}
		return protocol.AppendBulkBytes(dst, b), true

	case "SET":
		if len(args) != 2 {
			return dst, false
		}
		if err := db.SetStringBytes(args[0], args[1]); err != nil {
			return protocol.AppendErrorString(dst, err.Error()), true
		}
		return protocol.AppendSimpleString(dst, "OK"), true

	case "INCR":
		if len(args) != 1 {
			return protocol.AppendErrorString(dst, "ERR wrong number of arguments for 'incr' command"), true
		}
		newValue, err := db.IncrStringBytes(args[0])
		if err != nil {
			return protocol.AppendErrorString(dst, err.Error()), true
		}
		return protocol.AppendInteger(dst, newValue), true
	}

	return dst, false
}
