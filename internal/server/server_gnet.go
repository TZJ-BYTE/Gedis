package server

import (
	"context"
	"fmt"
	"time"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/command"
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/network"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
	"github.com/panjf2000/gnet/v2"
)

type GnetServer struct {
	config    *config.Config
	dbManager *database.DBManager
	registry  *command.CommandRegistry
	eng       gnet.Engine
}

func NewGnetServer(cfg *config.Config) network.Server {
	command.InitDefaultCommands()
	return &GnetServer{
		config:    cfg,
		dbManager: database.NewDBManager(cfg),
		registry:  command.DefaultRegistry,
	}
}

func (s *GnetServer) Start() error {
	addr := fmt.Sprintf("tcp://%s:%d", s.config.Host, s.config.Port)
	logger.Info("Gedis 服务器启动在 %s (gnet)", addr)
	return gnet.Run(s, addr, gnet.WithMulticore(true), gnet.WithReusePort(true))
}

func (s *GnetServer) Stop() error {
	logger.Info("Stopping gnet server...")
	// 关闭 DBManager
	if s.dbManager != nil {
		s.dbManager.Close()
	}
	return s.eng.Stop(context.Background())
}

func (s *GnetServer) OnBoot(eng gnet.Engine) gnet.Action {
	s.eng = eng
	logger.Info("gnet engine is running")
	return gnet.None
}

func (s *GnetServer) OnTick() (delay time.Duration, action gnet.Action) {
	return 1 * time.Second, gnet.None
}

func (s *GnetServer) OnShutdown(eng gnet.Engine) {
	logger.Info("gnet server shutdown")
}

func (s *GnetServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	logger.Info("New connection: %s", c.RemoteAddr().String())
	c.SetContext(NewConnectionContext(s.dbManager.GetDefaultDB()))
	return nil, gnet.None
}

func (s *GnetServer) OnClose(c gnet.Conn, err error) gnet.Action {
	logger.Info("Connection closed: %s", c.RemoteAddr().String())
	return gnet.None
}

func (s *GnetServer) OnTraffic(c gnet.Conn) gnet.Action {
	// 处理粘包
	data, _ := c.Peek(-1)
	if len(data) == 0 {
		return gnet.None
	}

	offset := 0
	connCtx := c.Context().(*ConnectionContext)

	for {
		req, n, err := protocol.ParseOneRequest(data[offset:])
		if err != nil {
			logger.Error("Parser error: %v", err)
			resp := protocol.MakeError(err)
			c.Write(protocol.EncodeResponse(resp))
			return gnet.Close
		}

		if n == 0 {
			// 数据不足，等待更多数据
			break
		}

		// 特殊处理 SELECT 命令
		if req.Cmd == "SELECT" {
			s.handleSelectCommand(c, connCtx, req.Args)
		} else {
			// 执行命令
			resp := s.registry.Execute(connCtx.DB, req.Cmd, req.Args)
			c.Write(protocol.EncodeResponse(resp))
		}

		offset += n

		if offset >= len(data) {
			break
		}
	}

	// 丢弃已处理的数据
	c.Discard(offset)

	return gnet.None
}

// handleSelectCommand 处理 SELECT 命令
func (s *GnetServer) handleSelectCommand(c gnet.Conn, ctx *ConnectionContext, args []string) {
	if len(args) != 1 {
		resp := protocol.MakeError(fmt.Errorf("ERR wrong number of arguments for 'select' command"))
		c.Write(protocol.EncodeResponse(resp))
		return
	}

	index := 0
	fmt.Sscanf(args[0], "%d", &index)

	// 使用 GetDBByIndex 验证索引有效性，但不改变全局 currentDB
	db, err := s.dbManager.GetDBByIndex(index)
	if err == nil {
		// 更新当前连接的上下文
		ctx.DB = db

		resp := protocol.MakeSimpleString("OK")
		c.Write(protocol.EncodeResponse(resp))
	} else {
		resp := protocol.MakeError(fmt.Errorf("ERR DB index is out of range"))
		c.Write(protocol.EncodeResponse(resp))
	}
}
