package command

import (
	"errors"

	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// LPushCommand LPUSH 命令
type LPushCommand struct{}

func (c *LPushCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'lpush' command"))
	}

	key := argString(args, 0)
	vals := make([]string, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		vals = append(vals, argString(args, i))
	}
	n, err := db.LPush(key, vals)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(int64(n))
}

// RPushCommand RPUSH 命令
type RPushCommand struct{}

func (c *RPushCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'rpush' command"))
	}

	key := argString(args, 0)
	vals := make([]string, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		vals = append(vals, argString(args, i))
	}
	n, err := db.RPush(key, vals)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(int64(n))
}

// LPopCommand LPOP 命令
type LPopCommand struct{}

func (c *LPopCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'lpop' command"))
	}

	key := argString(args, 0)
	val, ok, err := db.LPop(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	if !ok {
		return protocol.MakeNull()
	}
	return protocol.MakeBulkString(val)
}

// RPopCommand RPOP 命令
type RPopCommand struct{}

func (c *RPopCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'rpop' command"))
	}

	key := argString(args, 0)
	val, ok, err := db.RPop(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	if !ok {
		return protocol.MakeNull()
	}
	return protocol.MakeBulkString(val)
}

// LLenCommand LLEN 命令
type LLenCommand struct{}

func (c *LLenCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'llen' command"))
	}

	key := argString(args, 0)
	n, err := db.LLen(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(int64(n))
}

// LRangeCommand LRANGE 命令
type LRangeCommand struct{}

func (c *LRangeCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'lrange' command"))
	}

	key := argString(args, 0)
	start := parseIntBytes(args[1])
	stop := parseIntBytes(args[2])
	result, err := db.LRange(key, start, stop)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeArray(result)
}

func parseIntBytes(b []byte) int {
	n, _ := protocol.ParseInt(b)
	return n
}
