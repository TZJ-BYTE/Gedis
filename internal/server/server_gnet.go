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
	manager := database.NewDBManager(cfg)
	command.SetDBManager(manager)
	return &GnetServer{
		config:    cfg,
		dbManager: manager,
		registry:  command.DefaultRegistry,
	}
}

func (s *GnetServer) Start() error {
	addr := fmt.Sprintf("tcp://%s:%d", s.config.Host, s.config.Port)
	logger.Info("RediGo 服务器启动在 %s (gnet)", addr)
	return gnet.Run(s, addr, gnet.WithMulticore(true), gnet.WithReusePort(true))
}

func (s *GnetServer) Stop() error {
	logger.Info("Stopping gnet server...")
	// 关闭 DBManager
	if s.dbManager != nil {
		if err := s.dbManager.Close(); err != nil {
			logger.Warn("关闭 DBManager 失败：%v", err)
		}
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
	data, err := c.Peek(-1)
	if err != nil {
		logger.Error("Peek error: %v", err)
		return gnet.Close
	}
	if len(data) == 0 {
		return gnet.None
	}

	offset := 0
	connCtx := c.Context().(*ConnectionContext)
	connCtx.writeBuf = connCtx.writeBuf[:0]

	for {
		req, n, err := protocol.ParseOneRequestFast(data[offset:])
		if err != nil {
			logger.Error("Parser error: %v", err)
			resp := protocol.MakeError(err)
			connCtx.writeBuf = protocol.AppendResponse(connCtx.writeBuf, resp)
			protocol.ReleaseResponse(resp)
			if _, werr := c.Write(connCtx.writeBuf); werr != nil {
				logger.Error("Write error: %v", werr)
			}
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
			if out, ok := fastPathExecute(connCtx.writeBuf, connCtx.DB, req.Cmd, req.Args); ok {
				connCtx.writeBuf = out
			} else {
				resp := s.registry.Execute(connCtx.DB, req.Cmd, req.Args)
				connCtx.writeBuf = protocol.AppendResponse(connCtx.writeBuf, resp)
				protocol.ReleaseResponse(resp)
			}
		}
		protocol.ReleaseRequest(req)

		offset += n

		if offset >= len(data) {
			break
		}
	}

	// 丢弃已处理的数据
	if _, err := c.Discard(offset); err != nil {
		logger.Error("Discard error: %v", err)
		return gnet.Close
	}
	if len(connCtx.writeBuf) > 0 {
		if _, err := c.Write(connCtx.writeBuf); err != nil {
			logger.Error("Write error: %v", err)
			return gnet.Close
		}
	}

	return gnet.None
}

// handleSelectCommand 处理 SELECT 命令
func (s *GnetServer) handleSelectCommand(c gnet.Conn, ctx *ConnectionContext, args [][]byte) {
	if len(args) != 1 {
		resp := protocol.MakeError(fmt.Errorf("ERR wrong number of arguments for 'select' command"))
		ctx.writeBuf = protocol.AppendResponse(ctx.writeBuf, resp)
		protocol.ReleaseResponse(resp)
		return
	}

	index, err := protocol.ParseInt(args[0])
	if err != nil {
		resp := protocol.MakeError(fmt.Errorf("ERR invalid DB index"))
		ctx.writeBuf = protocol.AppendResponse(ctx.writeBuf, resp)
		protocol.ReleaseResponse(resp)
		return
	}

	// 使用 GetDBByIndex 验证索引有效性，但不改变全局 currentDB
	db, err := s.dbManager.GetDBByIndex(index)
	if err == nil {
		// 更新当前连接的上下文
		ctx.DB = db

		resp := protocol.MakeSimpleString("OK")
		ctx.writeBuf = protocol.AppendResponse(ctx.writeBuf, resp)
		protocol.ReleaseResponse(resp)
	} else {
		resp := protocol.MakeError(fmt.Errorf("ERR DB index is out of range"))
		ctx.writeBuf = protocol.AppendResponse(ctx.writeBuf, resp)
		protocol.ReleaseResponse(resp)
	}
}
