package command

import (
	"errors"
	"strconv"
	
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// HSetCommand HSET 命令
type HSetCommand struct{}

func (c *HSetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hset' command"))
	}
	
	key := args[0]
	field := args[1]
	value := args[2]
	
	valueObj, exists := db.Get(key)
	var hash *datastruct.Hash
	
	if !exists {
		hash = &datastruct.Hash{
			Data: make(map[string]string),
		}
	} else {
		h, ok := valueObj.Value.(*datastruct.Hash)
		if !ok {
			return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
		}
		hash = h
	}
	
	// 检查字段是否已存在
	_, fieldExists := hash.Data[field]
	hash.Set(field, value)
	
	db.Set(key, &datastruct.DataValue{
		Value:      hash,
		ExpireTime: 0,
	})
	
	// 如果字段不存在，返回 1；如果字段已存在并更新了值，返回 0
	if !fieldExists {
		return protocol.MakeInteger(1)
	}
	return protocol.MakeInteger(0)
}

// HGetCommand HGET 命令
type HGetCommand struct{}

func (c *HGetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hget' command"))
	}
	
	key := args[0]
	field := args[1]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeNull()
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	fieldValue, fieldExists := hash.Get(field)
	if !fieldExists {
		return protocol.MakeNull()
	}
	
	return protocol.MakeBulkString(fieldValue)
}

// HMSetCommand HMSET 命令
type HMSetCommand struct{}

func (c *HMSetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 3 || (len(args)-1)%2 != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hmset' command"))
	}
	
	key := args[0]
	
	valueObj, exists := db.Get(key)
	var hash *datastruct.Hash
	
	if !exists {
		hash = &datastruct.Hash{
			Data: make(map[string]string),
		}
	} else {
		h, ok := valueObj.Value.(*datastruct.Hash)
		if !ok {
			return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
		}
		hash = h
	}
	
	// 批量设置字段值
	for i := 1; i < len(args); i += 2 {
		field := args[i]
		value := args[i+1]
		hash.Set(field, value)
	}
	
	db.Set(key, &datastruct.DataValue{
		Value:      hash,
		ExpireTime: 0,
	})
	
	return protocol.MakeSimpleString("OK")
}

// HMGetCommand HMGET 命令
type HMGetCommand struct{}

func (c *HMGetCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hmget' command"))
	}
	
	key := args[0]
	fields := args[1:]
	
	value, exists := db.Get(key)
	if !exists {
		// 返回 null 数组
		result := make([]string, len(fields))
		return protocol.MakeArray(result)
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 获取所有字段的值
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if fieldValue, fieldExists := hash.Get(field); fieldExists {
			result = append(result, fieldValue)
		} else {
			result = append(result, "") // 不存在的字段返回空字符串
		}
	}
	
	return protocol.MakeArray(result)
}

// HDelCommand HDEL 命令
type HDelCommand struct{}

func (c *HDelCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hdel' command"))
	}
	
	key := args[0]
	fields := args[1:]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeInteger(0)
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 删除所有指定字段
	count := 0
	for _, field := range fields {
		if hash.Delete(field) {
			count++
		}
	}
	
	// 如果哈希为空，删除整个键
	if hash.Size() == 0 {
		db.Delete(key)
	}
	
	return protocol.MakeInteger(int64(count))
}

// HLenCommand HLEN 命令
type HLenCommand struct{}

func (c *HLenCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hlen' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeInteger(0)
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	return protocol.MakeInteger(int64(hash.Size()))
}

// HExistsCommand HEXISTS 命令
type HExistsCommand struct{}

func (c *HExistsCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hexists' command"))
	}
	
	key := args[0]
	field := args[1]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeInteger(0)
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	_, fieldExists := hash.Get(field)
	if fieldExists {
		return protocol.MakeInteger(1)
	}
	return protocol.MakeInteger(0)
}

// HKeysCommand HKEYS 命令
type HKeysCommand struct{}

func (c *HKeysCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hkeys' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeArray([]string{})
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 获取所有字段名
	keys := make([]string, 0, hash.Size())
	for field := range hash.Data {
		keys = append(keys, field)
	}
	
	return protocol.MakeArray(keys)
}

// HValsCommand HVALS 命令
type HValsCommand struct{}

func (c *HValsCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hvals' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeArray([]string{})
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 获取所有字段值
	values := make([]string, 0, hash.Size())
	for _, val := range hash.Data {
		values = append(values, val)
	}
	
	return protocol.MakeArray(values)
}

// HGetAllCommand HGETALL 命令
type HGetAllCommand struct{}

func (c *HGetAllCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hgetall' command"))
	}
	
	key := args[0]
	
	value, exists := db.Get(key)
	if !exists {
		return protocol.MakeArray([]string{})
	}
	
	hash, ok := value.Value.(*datastruct.Hash)
	if !ok {
		return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
	}
	
	// 获取所有字段和值（交替返回）
	result := make([]string, 0, hash.Size()*2)
	for field, val := range hash.Data {
		result = append(result, field)
		result = append(result, val)
	}
	
	return protocol.MakeArray(result)
}

// HIncrByCommand HINCRBY 命令
type HIncrByCommand struct{}

func (c *HIncrByCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hincrby' command"))
	}
	
	key := args[0]
	field := args[1]
	delta, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
	}
	
	value, exists := db.Get(key)
	var hash *datastruct.Hash
	
	if !exists {
		hash = &datastruct.Hash{
			Data: make(map[string]string),
		}
	} else {
		h, ok := value.Value.(*datastruct.Hash)
		if !ok {
			return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
		}
		hash = h
	}
	
	// 获取当前值
	currentStr, _ := hash.Get(field)
	var current int64
	if currentStr != "" {
		current, err = strconv.ParseInt(currentStr, 10, 64)
		if err != nil {
			return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
		}
	}
	
	// 增加
	newValue := current + delta
	hash.Set(field, strconv.FormatInt(newValue, 10))
	
	db.Set(key, &datastruct.DataValue{
		Value:      hash,
		ExpireTime: 0,
	})
	
	return protocol.MakeInteger(newValue)
}

// HIncrByFloatCommand HINCRBYFLOAT 命令
type HIncrByFloatCommand struct{}

func (c *HIncrByFloatCommand) Execute(db *database.Database, args []string) *protocol.Response {
	if len(args) != 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hincrbyfloat' command"))
	}
	
	key := args[0]
	field := args[1]
	delta, err := strconv.ParseFloat(args[2], 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR value is not a float or out of range"))
	}
	
	value, exists := db.Get(key)
	var hash *datastruct.Hash
	
	if !exists {
		hash = &datastruct.Hash{
			Data: make(map[string]string),
		}
	} else {
		h, ok := value.Value.(*datastruct.Hash)
		if !ok {
			return protocol.MakeError(errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"))
		}
		hash = h
	}
	
	// 获取当前值
	currentStr, _ := hash.Get(field)
	var current float64
	if currentStr != "" {
		current, err = strconv.ParseFloat(currentStr, 64)
		if err != nil {
			return protocol.MakeError(errors.New("ERR value is not a float or out of range"))
		}
	}
	
	// 增加
	newValue := current + delta
	hash.Set(field, strconv.FormatFloat(newValue, 'f', -1, 64))
	
	db.Set(key, &datastruct.DataValue{
		Value:      hash,
		ExpireTime: 0,
	})
	
	return protocol.MakeBulkString(strconv.FormatFloat(newValue, 'f', -1, 64))
}
