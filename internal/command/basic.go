package command

import (
	"errors"
	"strconv"
	
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// SetCommand SET 命令
type SetCommand struct{}

func (c *SetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'set' command"))
	}
	
	key := args[0]
	value := args[1]
	
	dataValue := &datastruct.DataValue{
		Value:      &datastruct.String{Data: value},
		ExpireTime: 0,
	}
	
	db.Set(key, dataValue)
	return protocol.MakeSimpleString("OK")
}

// GetCommand GET 命令
type GetCommand struct{}

func (c *GetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'get' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeNull()
	}
	
	str, ok := value.Value.(*datastruct.String)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	return protocol.MakeBulkString(str.Data)
}

// DelCommand DEL 命令
type DelCommand struct{}

func (c *DelCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) == 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'del' command"))
	}
	
	count := 0
	for _, key := range args {
		if db.Delete(key) {
			count++
		}
	}
	
	return protocol.MakeInteger(int64(count))
}

// ExistsCommand EXISTS 命令
type ExistsCommand struct{}

func (c *ExistsCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) == 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'exists' command"))
	}
	
	count := 0
	for _, key := range args {
		if db.Exists(key) {
			count++
		}
	}
	
	return protocol.MakeInteger(int64(count))
}

// ExpireCommand EXPIRE 命令
type ExpireCommand struct{}

func (c *ExpireCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'expire' command"))
	}
	
	key := args[0]
	ttl, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR invalid expire time"))
	}
	
	success := db.Expire(key, ttl*1000) // 转换为毫秒
	if success {
		return protocol.MakeInteger(1)
	}
	return protocol.MakeInteger(0)
}

// KeysCommand KEYS 命令
type KeysCommand struct{}

func (c *KeysCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'keys' command"))
	}
	
	pattern := args[0]
	keys := db.Keys()
	
	// 简单的模式匹配（只支持 *)
	var result []string
	if pattern == "*" {
		result = keys
	} else {
		// TODO: 实现完整的模式匹配
		result = keys
	}
	
	return protocol.MakeArray(result)
}

// FlushDBCommand FLUSHDB 命令
type FlushDBCommand struct{}

func (c *FlushDBCommand) Execute(db *database.Database, args []string) *protocol.Response {
	db.Clear()
	return protocol.MakeSimpleString("OK")
}

// DBSizeCommand DBSIZE 命令
type DBSizeCommand struct{}

func (c *DBSizeCommand) Execute(db *database.Database, args []string) *protocol.Response {
	size := db.Size()
	return protocol.MakeInteger(int64(size))
}
