package command

import (
	"errors"
	"strconv"
	"time"

	"path"

	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// SetCommand SET 命令
type SetCommand struct{}

func (c *SetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'set' command"))
	}

	nx := false
	xx := false
	var ttlMs int64
	keepTTL := false

	for i := 2; i < len(args); i++ {
		opt := args[i]
		if len(opt) == 0 {
			return protocol.MakeError(errors.New("ERR syntax error"))
		}
		switch {
		case equalsIgnoreCase(opt, []byte("NX")):
			if xx {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			nx = true
		case equalsIgnoreCase(opt, []byte("XX")):
			if nx {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			xx = true
		case equalsIgnoreCase(opt, []byte("KEEPTTL")):
			if ttlMs != 0 {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			keepTTL = true
		case equalsIgnoreCase(opt, []byte("EX")):
			if ttlMs != 0 || keepTTL {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			if i+1 >= len(args) {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			n, err := protocol.ParseInt(args[i+1])
			if err != nil || n <= 0 {
				return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
			}
			ttlMs = int64(n) * 1000
			i++
		case equalsIgnoreCase(opt, []byte("PX")):
			if ttlMs != 0 || keepTTL {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			if i+1 >= len(args) {
				return protocol.MakeError(errors.New("ERR syntax error"))
			}
			n, err := protocol.ParseInt(args[i+1])
			if err != nil || n <= 0 {
				return protocol.MakeError(errors.New("ERR value is not an integer or out of range"))
			}
			ttlMs = int64(n)
			i++
		default:
			return protocol.MakeError(errors.New("ERR syntax error"))
		}
	}

	set, err := db.SetStringBytesWithOptions(args[0], args[1], ttlMs, keepTTL, nx, xx)
	if err != nil {
		return protocol.MakeError(err)
	}
	if !set {
		return protocol.MakeNull()
	}
	return protocol.MakeSimpleString("OK")
}

// GetCommand GET 命令
type GetCommand struct{}

func (c *GetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'get' command"))
	}

	s, ok, err := db.GetStringCopy(args[0])
	if err != nil {
		return protocol.MakeError(err)
	}
	if !ok {
		return protocol.MakeNull()
	}
	return protocol.MakeBulkString(s)
}

// DelCommand DEL 命令
type DelCommand struct{}

func (c *DelCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) == 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'del' command"))
	}

	count := 0
	for i := range args {
		ok, err := db.DeleteWithError(argString(args, i))
		if err != nil {
			return protocol.MakeError(err)
		}
		if ok {
			count++
		}
	}

	return protocol.MakeInteger(int64(count))
}

// ExistsCommand EXISTS 命令
type ExistsCommand struct{}

func (c *ExistsCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) == 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'exists' command"))
	}

	count := 0
	for i := range args {
		if db.Exists(argString(args, i)) {
			count++
		}
	}

	return protocol.MakeInteger(int64(count))
}

// ExpireCommand EXPIRE 命令
type ExpireCommand struct{}

func (c *ExpireCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'expire' command"))
	}

	key := argString(args, 0)
	ttl, err := strconv.ParseInt(argString(args, 1), 10, 64)
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

func (c *KeysCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'keys' command"))
	}

	pattern := argString(args, 0)
	keys := db.Keys()

	var result []string
	if pattern == "*" {
		result = keys
	} else {
		result = make([]string, 0, len(keys))
		for _, k := range keys {
			ok, err := path.Match(pattern, k)
			if err != nil {
				return protocol.MakeError(errors.New("ERR invalid pattern"))
			}
			if ok {
				result = append(result, k)
			}
		}
	}

	return protocol.MakeArray(result)
}

// FlushDBCommand FLUSHDB 命令
type FlushDBCommand struct{}

func (c *FlushDBCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'flushdb' command"))
	}
	if err := db.Clear(); err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeSimpleString("OK")
}

// DBSizeCommand DBSIZE 命令
type DBSizeCommand struct{}

func (c *DBSizeCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'dbsize' command"))
	}
	size := db.Size()
	return protocol.MakeInteger(int64(size))
}

// PingCommand PING 命令
type PingCommand struct{}

func (c *PingCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) > 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'ping' command"))
	}

	if len(args) == 0 {
		return protocol.MakeSimpleString("PONG")
	}

	return protocol.MakeBulkString(argString(args, 0))
}

// TtlCommand TTL 命令
type TtlCommand struct{}

func (c *TtlCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'ttl' command"))
	}

	key := argString(args, 0)
	expireAt, exists := db.GetExpireTime(key)
	if !exists {
		return protocol.MakeInteger(-2) // key 不存在
	}

	if expireAt == 0 {
		return protocol.MakeInteger(-1) // 永不过期
	}

	// 计算剩余时间（秒）
	now := time.Now().UnixMilli()
	remaining := (expireAt - now) / 1000
	if remaining <= 0 {
		return protocol.MakeInteger(-2) // 已过期
	}

	return protocol.MakeInteger(remaining)
}

// PttlCommand PTTL 命令
type PttlCommand struct{}

func (c *PttlCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'pttl' command"))
	}

	key := argString(args, 0)
	expireAt, exists := db.GetExpireTime(key)
	if !exists {
		return protocol.MakeInteger(-2) // key 不存在
	}

	if expireAt == 0 {
		return protocol.MakeInteger(-1) // 永不过期
	}

	// 计算剩余时间（毫秒）
	remaining := expireAt - time.Now().UnixMilli()
	if remaining <= 0 {
		return protocol.MakeInteger(-2) // 已过期
	}

	return protocol.MakeInteger(remaining)
}

// IncrCommand INCR 命令
type IncrCommand struct{}

func (c *IncrCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'incr' command"))
	}

	newValue, err := db.IncrStringBytes(args[0])
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(newValue)
}

// DecrCommand DECR 命令
type DecrCommand struct{}

func (c *DecrCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 1 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'decr' command"))
	}

	newValue, err := db.DecrStringBytes(args[0])
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeInteger(newValue)
}

// MsetCommand MSET 命令
type MsetCommand struct{}

func (c *MsetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) < 2 || len(args)%2 != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'mset' command"))
	}

	if err := db.MSetStringBytes(args); err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeSimpleString("OK")
}

// MgetCommand MGET 命令
type MgetCommand struct{}

func (c *MgetCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) == 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'mget' command"))
	}

	values, ok, err := db.MGetStringCopies(args)
	if err != nil {
		return protocol.MakeError(err)
	}

	resp := make([]*protocol.Response, len(values))
	for i := range values {
		if !ok[i] {
			resp[i] = protocol.MakeNull()
			continue
		}
		resp[i] = protocol.MakeBulkString(values[i])
	}
	return protocol.MakeArrayResponses(resp)
}

// RenameCommand RENAME 命令
type RenameCommand struct{}

func (c *RenameCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'rename' command"))
	}

	oldKey := argString(args, 0)
	newKey := argString(args, 1)
	_, err := db.Rename(oldKey, newKey, false)
	if err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeSimpleString("OK")
}

// RenamenxCommand RENAMENX 命令（只有当新 key 不存在时才重命名）
type RenamenxCommand struct{}

func (c *RenamenxCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 2 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'renamenx' command"))
	}

	oldKey := argString(args, 0)
	newKey := argString(args, 1)
	ok, err := db.Rename(oldKey, newKey, true)
	if err != nil {
		return protocol.MakeError(err)
	}
	if !ok {
		return protocol.MakeInteger(0)
	}
	return protocol.MakeInteger(1)
}
