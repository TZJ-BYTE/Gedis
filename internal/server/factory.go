package server

import (
	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/network"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

// NewServer 根据配置创建服务器
func NewServer(cfg *config.Config) network.Server {
	protocol.SetParseLimits(cfg.RespMaxBulkLen, cfg.RespMaxArrayLen)
	if cfg.NetworkType == "gnet" {
		return NewGnetServer(cfg)
	}
	return NewStdServer(cfg)
}
