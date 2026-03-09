package command

import (
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// Command 命令接口
type Command interface {
	Execute(db *database.Database, args []string) *protocol.Response
}

// CommandFunc 命令函数类型
type CommandFunc func(db *database.Database, args []string) *protocol.Response
