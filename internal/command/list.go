package command

import (
	"errors"
	"strconv"
	
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// LPushCommand LPUSH 命令
type LPushCommand struct{}

func (c *LPushCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'lpush' command"))
	}
	
	key := args[0]
	values := args[1:]
	
	value, exists := db.Get(key)
	var list *datastruct.List
	
	if !exists {
		list = &datastruct.List{
			Data: make([]string, 0),
		}
	} else {
		l, ok := value.Value.(*datastruct.List)
		if !ok {
			return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
		}
		list = l
	}
	
	for _, v := range values {
		list.PushLeft(v)
	}
	
	db.Set(key, &datastruct.DataValue{
		Value:      list,
		ExpireTime: 0,
	})
	
	return protocol.MakeInteger(int64(len(list.Data)))
}

// RPushCommand RPUSH 命令
type RPushCommand struct{}

func (c *RPushCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'rpush' command"))
	}
	
	key := args[0]
	values := args[1:]
	
	value, exists := db.Get(key)
	var list *datastruct.List
	
	if !exists {
		list = &datastruct.List{
			Data: make([]string, 0),
		}
	} else {
		l, ok := value.Value.(*datastruct.List)
		if !ok {
			return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
		}
		list = l
	}
	
	for _, v := range values {
		list.PushRight(v)
	}
	
	db.Set(key, &datastruct.DataValue{
		Value:      list,
		ExpireTime: 0,
	})
	
	return protocol.MakeInteger(int64(len(list.Data)))
}

// LPopCommand LPOP 命令
type LPopCommand struct{}

func (c *LPopCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'lpop' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeNull()
	}
	
	list, ok := value.Value.(*datastruct.List)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	val, success := list.PopLeft()
	if !success {
		return protocol.MakeNull()
	}
	
	return protocol.MakeBulkString(val)
}

// RPopCommand RPOP 命令
type RPopCommand struct{}

func (c *RPopCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'rpop' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeNull()
	}
	
	list, ok := value.Value.(*datastruct.List)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	val, success := list.PopRight()
	if !success {
		return protocol.MakeNull()
	}
	
	return protocol.MakeBulkString(val)
}

// LLenCommand LLEN 命令
type LLenCommand struct{}

func (c *LLenCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'llen' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeInteger(0)
	}
	
	list, ok := value.Value.(*datastruct.List)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	return protocol.MakeInteger(int64(len(list.Data)))
}

// LRangeCommand LRANGE 命令
type LRangeCommand struct{}

func (c *LRangeCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'lrange' command"))
	}
	
	key := args[0]
	start := parseInt(args[1])
	stop := parseInt(args[2])
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeArray([]string{})
	}
	
	list, ok := value.Value.(*datastruct.List)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	result := list.Range(start, stop)
	return protocol.MakeArray(result)
}

// parseInt 解析整数（辅助函数）
func parseInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
