package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/command"
	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/network"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

// StdServer 基于标准库 net 的 Redis 服务器
type StdServer struct {
	config    *config.Config
	listener  net.Listener
	dbManager *database.DBManager
	registry  *command.CommandRegistry
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewStdServer 创建标准服务器
func NewStdServer(cfg *config.Config) network.Server {
	ctx, cancel := context.WithCancel(context.Background())

	// 初始化命令注册表
	command.InitDefaultCommands()

	return &StdServer{
		config:    cfg,
		dbManager: database.NewDBManager(cfg),
		registry:  command.DefaultRegistry,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start 启动服务器
func (s *StdServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", addr, err)
	}

	s.listener = listener
	logger.Info("Gedis 服务器启动在 %s (Standard Net)", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return nil
			default:
				logger.Error("接受连接失败：%v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

// handleConnection 处理客户端连接
func (s *StdServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	logger.Info("新连接：%s", conn.RemoteAddr().String())

	parser := protocol.NewParser(conn)
	connCtx := NewConnectionContext(s.dbManager.GetDefaultDB())

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// 解析请求
		req, err := parser.ParseRequest()
		if err != nil {
			if err == io.EOF {
				logger.Info("连接关闭：%s", conn.RemoteAddr().String())
				return
			}
			logger.Error("解析请求错误：%v", err)

			// 发送错误响应
			resp := protocol.MakeError(err)
			conn.Write(protocol.EncodeResponse(resp))
			continue
		}

		// 特殊处理 SELECT 命令
		if req.Cmd == "SELECT" {
			s.handleSelectCommand(conn, connCtx, req.Args)
			continue
		}

		// 执行命令
		resp := s.registry.Execute(connCtx.DB, req.Cmd, req.Args)

		// 发送响应
		conn.Write(protocol.EncodeResponse(resp))
	}
}

// handleSelectCommand 处理 SELECT 命令
func (s *StdServer) handleSelectCommand(conn net.Conn, ctx *ConnectionContext, args []string) {
	if len(args) != 1 {
		resp := protocol.MakeError(fmt.Errorf("ERR wrong number of arguments for 'select' command"))
		conn.Write(protocol.EncodeResponse(resp))
		return
	}

	index := 0
	_, err := fmt.Sscanf(args[0], "%d", &index)
	if err != nil {
		resp := protocol.MakeError(fmt.Errorf("ERR invalid DB index"))
		conn.Write(protocol.EncodeResponse(resp))
		return
	}

	newDB, err := s.dbManager.GetDBByIndex(index)
	if err != nil {
		resp := protocol.MakeError(fmt.Errorf("ERR DB index is out of range"))
		conn.Write(protocol.EncodeResponse(resp))
		return
	}

	ctx.DB = newDB
	resp := protocol.MakeSimpleString("OK")
	conn.Write(protocol.EncodeResponse(resp))
}

// Stop 停止服务器
func (s *StdServer) Stop() error {
	logger.Info("=== Server Stop called ===")

	s.cancel()

	// 异步关闭 listener，设置超时避免阻塞
	if s.listener != nil {
		logger.Info("Closing listener with timeout...")
		done := make(chan struct{})
		go func() {
			err := s.listener.Close()
			if err != nil {
				logger.Warn("Error closing listener: %v", err)
			} else {
				logger.Info("Listener closed successfully")
			}
			close(done)
		}()

		// 等待最多 1 秒
		select {
		case <-done:
			// Listener 已关闭
		case <-time.After(1 * time.Second):
			logger.Warn("Listener close timeout, continuing shutdown...")
		}
	}

	// 关闭数据库管理器（会关闭所有数据库和 LSM 引擎）
	if s.dbManager != nil {
		logger.Info("Closing database manager...")
		err := s.dbManager.Close()
		if err != nil {
			logger.Error("Failed to close database manager: %v", err)
			return err
		} else {
			logger.Info("Database manager closed successfully")
		}
	}

	logger.Info("=== Gedis 服务器已停止 ===")
	return nil
}
