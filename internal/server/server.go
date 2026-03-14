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
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

// Server Redis 服务器
type Server struct {
	config     *config.Config
	listener   net.Listener
	dbManager  *database.DBManager
	registry   *command.CommandRegistry
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewServer 创建服务器
func NewServer(cfg *config.Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	// 初始化命令注册表
	command.InitDefaultCommands()
	
	return &Server{
		config:    cfg,
		dbManager: database.NewDBManager(cfg),
		registry:  command.DefaultRegistry,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", addr, err)
	}
	
	s.listener = listener
	logger.Info("Gedis 服务器启动在 %s", addr)
	
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
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	
	logger.Info("新连接：%s", conn.RemoteAddr().String())
	
	parser := protocol.NewParser(conn)
	currentDB := s.dbManager.GetDB()
	
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
			s.handleSelectCommand(conn, currentDB, req.Args)
			continue
		}
		
		// 执行命令
		resp := s.registry.Execute(currentDB, req.Cmd, req.Args)
		
		// 发送响应
		conn.Write(protocol.EncodeResponse(resp))
	}
}

// handleSelectCommand 处理 SELECT 命令
func (s *Server) handleSelectCommand(conn net.Conn, db *database.Database, args []string) {
	if len(args) != 1 {
		resp := protocol.MakeError(fmt.Errorf("ERR wrong number of arguments for 'select' command"))
		conn.Write(protocol.EncodeResponse(resp))
		return
	}
	
	index := 0
	fmt.Sscanf(args[0], "%d", &index)
	
	success := s.dbManager.SelectDB(index)
	if success {
		resp := protocol.MakeSimpleString("OK")
		conn.Write(protocol.EncodeResponse(resp))
	} else {
		resp := protocol.MakeError(fmt.Errorf("ERR DB index is out of range"))
		conn.Write(protocol.EncodeResponse(resp))
	}
}

// Stop 停止服务器
func (s *Server) Stop() {
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
		} else {
			logger.Info("Database manager closed successfully")
		}
	}
	
	logger.Info("=== Gedis 服务器已停止 ===")
}
