package command

import (
	"errors"
	"strconv"

	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// HSetCommand HSET 命令
type HSetCommand struct{}

func (c *HSetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hset' command"))
	}

	key := argString(args, 0)
	field := argString(args, 1)
	value := argString(args, 2)
	created, err := db.HSet(key, field, value)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(created)
}

// HGetCommand HGET 命令
type HGetCommand struct{}

func (c *HGetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hget' command"))
	}

	key := argString(args, 0)
	field := argString(args, 1)
	v, ok, err := db.HGet(key, field)
	if err != nil {
		return protocol.MakeError(err)
	}
	if !ok {
		return protocol.MakeNull()
	}
	return protocol.MakeBulkString(v)
}

// HMSetCommand HMSET 命令
type HMSetCommand struct{}

func (c *HMSetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 3 || (len(args)-1)%2 != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hmset' command"))
	}

	key := argString(args, 0)
	fields := make([]string, 0, (len(args)-1)/2)
	values := make([]string, 0, (len(args)-1)/2)
	for i := 1; i < len(args); i += 2 {
		fields = append(fields, argString(args, i))
		values = append(values, argString(args, i+1))
	}
	if err := db.HMSet(key, fields, values); err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeSimpleString("OK")
}

// HMGetCommand HMGET 命令
type HMGetCommand struct{}

func (c *HMGetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hmget' command"))
	}

	key := argString(args, 0)
	fields := make([]string, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		fields = append(fields, argString(args, i))
	}
	result, ok, err := db.HMGetWithExists(key, fields)
	if err != nil {
		return protocol.MakeError(err)
	}
	resp := make([]*protocol.Response, len(result))
	for i := range result {
		if !ok[i] {
			resp[i] = protocol.MakeNull()
			continue
		}
		resp[i] = protocol.MakeBulkString(result[i])
	}
	return protocol.MakeArrayResponses(resp)
}

// HDelCommand HDEL 命令
type HDelCommand struct{}

func (c *HDelCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hdel' command"))
	}

	key := argString(args, 0)
	fields := make([]string, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		fields = append(fields, argString(args, i))
	}
	n, err := db.HDel(key, fields)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(n)
}

// HLenCommand HLEN 命令
type HLenCommand struct{}

func (c *HLenCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hlen' command"))
	}

	key := argString(args, 0)
	n, err := db.HLen(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(n)
}

// HExistsCommand HEXISTS 命令
type HExistsCommand struct{}

func (c *HExistsCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hexists' command"))
	}

	key := argString(args, 0)
	field := argString(args, 1)
	ok, err := db.HExists(key, field)
	if err != nil {
		return protocol.MakeError(err)
	}
	if ok {
		return protocol.MakeInteger(1)
	}
	return protocol.MakeInteger(0)
}

// HKeysCommand HKEYS 命令
type HKeysCommand struct{}

func (c *HKeysCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hkeys' command"))
	}

	key := argString(args, 0)
	keys, err := db.HKeys(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeArray(keys)
}

// HValsCommand HVALS 命令
type HValsCommand struct{}

func (c *HValsCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hvals' command"))
	}

	key := argString(args, 0)
	values, err := db.HVals(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeArray(values)
}

// HGetAllCommand HGETALL 命令
type HGetAllCommand struct{}

func (c *HGetAllCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hgetall' command"))
	}

	key := argString(args, 0)
	result, err := db.HGetAll(key)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeArray(result)
}

// HIncrByCommand HINCRBY 命令
type HIncrByCommand struct{}

func (c *HIncrByCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hincrby' command"))
	}

	key := argString(args, 0)
	field := argString(args, 1)
	delta, err := strconv.ParseInt(argString(args, 2), 10, 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
	}
	out, err := db.HIncrBy(key, field, delta)
	if err != nil {
		return protocol.MakeError(errors.New(err.Error()))
	}
	return protocol.MakeInteger(out)
}

// HIncrByFloatCommand HINCRBYFLOAT 命令
type HIncrByFloatCommand struct{}

func (c *HIncrByFloatCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 3 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'hincrbyfloat' command"))
	}

	key := argString(args, 0)
	field := argString(args, 1)
	delta, err := strconv.ParseFloat(argString(args, 2), 64)
	if err != nil {
		return protocol.MakeError(errors.New("ERR value is not a float or out of range"))
	}
	out, err := db.HIncrByFloat(key, field, delta)
	if err != nil {
		return protocol.MakeError(errors.New(err.Error()))
	}
	return protocol.MakeBulkString(strconv.FormatFloat(out, 'f', -1, 64))
}
