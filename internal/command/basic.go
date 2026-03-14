package command

import (
	"errors"
	"strconv"
	"time"
	
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

// PingCommand PING 命令
type PingCommand struct{}

func (c *PingCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) > 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'ping' command"))
	}
	
	if len(args) == 0 {
		return protocol.MakeSimpleString("PONG")
	}
	
	return protocol.MakeBulkString(args[0])
}

// TtlCommand TTL 命令
type TtlCommand struct{}

func (c *TtlCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'ttl' command"))
	}
	
	key := args[0]
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeInteger(-2) // key 不存在
	}
	
	if value.ExpireTime == 0 {
		return protocol.MakeInteger(-1) // 永不过期
	}
	
	// 计算剩余时间（秒）
	now := time.Now().UnixMilli()
	remaining := (value.ExpireTime - now) / 1000
	if remaining <= 0 {
		return protocol.MakeInteger(-2) // 已过期
	}
	
	return protocol.MakeInteger(remaining)
}

// PttlCommand PTTL 命令
type PttlCommand struct{}

func (c *PttlCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'pttl' command"))
	}
	
	key := args[0]
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeInteger(-2) // key 不存在
	}
	
	if value.ExpireTime == 0 {
		return protocol.MakeInteger(-1) // 永不过期
	}
	
	// 计算剩余时间（毫秒）
	remaining := value.ExpireTime - time.Now().UnixMilli()
	if remaining <= 0 {
		return protocol.MakeInteger(-2) // 已过期
	}
	
	return protocol.MakeInteger(remaining)
}

// IncrCommand INCR 命令
type IncrCommand struct{}

func (c *IncrCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'incr' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		// key 不存在，初始化为 0 然后 +1
		newValue := &datastruct.DataValue{
			Value:      &datastruct.String{Data: "1"},
			ExpireTime: 0,
		}
		db.Set(key, newValue)
		return protocol.MakeInteger(1)
	}
	
	str, ok := value.Value.(*datastruct.String)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 解析当前值
	current, err := strconv.ParseInt(str.Data, 10, 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
	}
	
	// 增加 1
	newValue := current + 1
	
	// 更新值
	dataValue := &datastruct.DataValue{
		Value:      &datastruct.String{Data: strconv.FormatInt(newValue, 10)},
		ExpireTime: value.ExpireTime,
	}
	db.Set(key, dataValue)
	
	return protocol.MakeInteger(newValue)
}

// DecrCommand DECR 命令
type DecrCommand struct{}

func (c *DecrCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'decr' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		// key 不存在，初始化为 0 然后 -1
		newValue := &datastruct.DataValue{
			Value:      &datastruct.String{Data: "-1"},
			ExpireTime: 0,
		}
		db.Set(key, newValue)
		return protocol.MakeInteger(-1)
	}
	
	str, ok := value.Value.(*datastruct.String)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 解析当前值
	current, err := strconv.ParseInt(str.Data, 10, 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
	}
	
	// 减少 1
	newValue := current - 1
	
	// 更新值
	dataValue := &datastruct.DataValue{
		Value:      &datastruct.String{Data: strconv.FormatInt(newValue, 10)},
		ExpireTime: value.ExpireTime,
	}
	db.Set(key, dataValue)
	
	return protocol.MakeInteger(newValue)
}

// MsetCommand MSET 命令
type MsetCommand struct{}

func (c *MsetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 2 || len(args)%2 != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'mset' command"))
	}
	
	// 批量设置键值对
	for i := 0; i < len(args); i += 2 {
		key := args[i]
		value := args[i+1]
		
		dataValue := &datastruct.DataValue{
			Value:      &datastruct.String{Data: value},
			ExpireTime: 0,
		}
		db.Set(key, dataValue)
	}
	
	return protocol.MakeSimpleString("OK")
}

// MgetCommand MGET 命令
type MgetCommand struct{}

func (c *MgetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) == 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'mget' command"))
	}
	
	results := make([]string, len(args))
	
	for i, key := range args {
		value, exists := db.Get(key)
		if !exists {
			results[i] = ""
			continue
		}
		
		str, ok := value.Value.(*datastruct.String)
		if !ok {
			results[i] = ""
			continue
		}
		
		results[i] = str.Data
	}
	
	return protocol.MakeArray(results)
}

// RenameCommand RENAME 命令
type RenameCommand struct{}

func (c *RenameCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'rename' command"))
	}
	
	oldKey := args[0]
	newKey := args[1]
	
	// 获取旧值
	value, exists := db.Get(oldKey)
	if !exists {
		return protocol.MakeError(errors.New("ERR no such key"))
	}
	
	// 如果新旧 key 相同，直接返回
	if oldKey == newKey {
		return protocol.MakeSimpleString("OK")
	}
	
	// 删除旧 key，设置新 key
	db.Delete(oldKey)
	db.Set(newKey, value)
	
	return protocol.MakeSimpleString("OK")
}

// RenamenxCommand RENAMENX 命令（只有当新 key 不存在时才重命名）
type RenamenxCommand struct{}

func (c *RenamenxCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'renamenx' command"))
	}
	
	oldKey := args[0]
	newKey := args[1]
	
	// 获取旧值
	value, exists := db.Get(oldKey)
	if !exists {
		return protocol.MakeError(errors.New("ERR no such key"))
	}
	
	// 如果新 key 已存在，返回 0
	if db.Exists(newKey) {
		return protocol.MakeInteger(0)
	}
	
	// 删除旧 key，设置新 key
	db.Delete(oldKey)
	db.Set(newKey, value)
	
	return protocol.MakeInteger(1)
}
