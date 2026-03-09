package command

import (
	"errors"
	"strings"
	
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// CommandRegistry 命令注册表
type CommandRegistry struct {
	commands map[string]Command
}

// NewCommandRegistry 创建命令注册表
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]Command),
	}
}

// Register 注册命令
func (r *CommandRegistry) Register(name string, cmd Command) {
	r.commands[strings.ToUpper(name)] = cmd
}

// Get 获取命令
func (r *CommandRegistry) Get(name string) (Command, bool) {
	cmd, exists := r.commands[strings.ToUpper(name)]
	return cmd, exists
}

// Execute 执行命令
func (r *CommandRegistry) Execute(db *database.Database, cmdName string, args []string) *protocol.Response {
	cmd, exists := r.Get(cmdName)
	if !exists {
		return protocol.MakeError(errors.New("ERR unknown command '" + cmdName + "'"))
	}
	
	return cmd.Execute(db, args)
}

// DefaultRegistry 默认命令注册表
var DefaultRegistry = NewCommandRegistry()

// InitDefaultCommands 初始化默认命令
func InitDefaultCommands() {
	// 字符串命令
	DefaultRegistry.Register("SET", &SetCommand{})
	DefaultRegistry.Register("GET", &GetCommand{})
	DefaultRegistry.Register("DEL", &DelCommand{})
	DefaultRegistry.Register("EXISTS", &ExistsCommand{})
	DefaultRegistry.Register("EXPIRE", &ExpireCommand{})
	DefaultRegistry.Register("KEYS", &KeysCommand{})
	DefaultRegistry.Register("FLUSHDB", &FlushDBCommand{})
	DefaultRegistry.Register("DBSIZE", &DBSizeCommand{})
	
	// 列表命令
	DefaultRegistry.Register("LPUSH", &LPushCommand{})
	DefaultRegistry.Register("RPUSH", &RPushCommand{})
	DefaultRegistry.Register("LPOP", &LPopCommand{})
	DefaultRegistry.Register("RPOP", &RPopCommand{})
	DefaultRegistry.Register("LLEN", &LLenCommand{})
	DefaultRegistry.Register("LRANGE", &LRangeCommand{})
	
	// 哈希命令
	DefaultRegistry.Register("HSET", &HSetCommand{})
	DefaultRegistry.Register("HGET", &HGetCommand{})
	DefaultRegistry.Register("HMSET", &HMSetCommand{})
	DefaultRegistry.Register("HMGET", &HMGetCommand{})
	DefaultRegistry.Register("HDEL", &HDelCommand{})
	DefaultRegistry.Register("HLEN", &HLenCommand{})
	DefaultRegistry.Register("HEXISTS", &HExistsCommand{})
	DefaultRegistry.Register("HKEYS", &HKeysCommand{})
	DefaultRegistry.Register("HVALS", &HValsCommand{})
	DefaultRegistry.Register("HGETALL", &HGetAllCommand{})
	DefaultRegistry.Register("HINCRBY", &HIncrByCommand{})
	DefaultRegistry.Register("HINCRBYFLOAT", &HIncrByFloatCommand{})
}
