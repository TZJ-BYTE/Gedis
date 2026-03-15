package server

import (
	"github.com/TZJ-BYTE/RediGo/internal/database"
)

// ConnectionContext 连接上下文
// 存储每个客户端连接的私有状态
type ConnectionContext struct {
	// 当前选中的数据库
	DB *database.Database

	// 认证状态（未来支持）
	Authenticated bool

	// 事务状态（未来支持）
	// MultiState *MultiState
}

// NewConnectionContext 创建新的连接上下文
func NewConnectionContext(defaultDB *database.Database) *ConnectionContext {
	return &ConnectionContext{
		DB:            defaultDB,
		Authenticated: false,
	}
}
