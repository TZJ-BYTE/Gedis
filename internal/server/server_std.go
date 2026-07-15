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

func writeAllConn(conn net.Conn, b []byte) error {
	for len(b) > 0 {
		n, err := conn.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

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
	manager := database.NewDBManager(cfg)
	command.SetDBManager(manager)

	return &StdServer{
		config:    cfg,
		dbManager: manager,
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
	logger.Info("RediGo 服务器启动在 %s (Standard Net)", addr)

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

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
			_ = tcpConn.SetReadBuffer(1 << 20)
			_ = tcpConn.SetWriteBuffer(1 << 20)
		}

		go s.handleConnection(conn)
	}
}

// handleConnection 处理客户端连接
func (s *StdServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	logger.Info("新连接：%s", conn.RemoteAddr().String())

	connCtx := NewConnectionContext(s.dbManager.GetDefaultDB())
	buf := make([]byte, 256*1024)
	start, end := 0, 0

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		if end == len(buf) {
			if start > 0 {
				copy(buf, buf[start:end])
				end -= start
				start = 0
			} else {
				nb := make([]byte, len(buf)*2)
				copy(nb, buf)
				buf = nb
			}
		}

		n, err := conn.Read(buf[end:])
		if err != nil {
			if err == io.EOF {
				logger.Info("连接关闭：%s", conn.RemoteAddr().String())
				return
			}
			logger.Error("读取请求错误：%v", err)
			resp := protocol.MakeError(err)
			connCtx.respBuf = protocol.EncodeResponseInto(connCtx.respBuf, resp)
			protocol.ReleaseResponse(resp)
			_ = writeAllConn(conn, connCtx.respBuf)
			continue
		}
		if n == 0 {
			continue
		}

		end += n
		connCtx.writeBuf = connCtx.writeBuf[:0]

		for {
			req, consumed, perr := protocol.ParseOneRequestFast(buf[start:end])
			if perr != nil {
				resp := protocol.MakeError(perr)
				connCtx.writeBuf = protocol.AppendResponse(connCtx.writeBuf, resp)
				protocol.ReleaseResponse(resp)
				_ = writeAllConn(conn, connCtx.writeBuf)
				return
			}
			if consumed == 0 {
				break
			}

			if req.Cmd == "SELECT" {
				s.handleSelectCommand(conn, connCtx, req.Args)
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
			start += consumed
			if start >= end {
				start, end = 0, 0
				break
			}
		}

		if len(connCtx.writeBuf) > 0 {
			if err := writeAllConn(conn, connCtx.writeBuf); err != nil {
				logger.Error("写入响应错误：%v", err)
				return
			}
		}
	}
}

// handleSelectCommand 处理 SELECT 命令
func (s *StdServer) handleSelectCommand(conn net.Conn, ctx *ConnectionContext, args [][]byte) {
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

	newDB, err := s.dbManager.GetDBByIndex(index)
	if err != nil {
		resp := protocol.MakeError(fmt.Errorf("ERR DB index is out of range"))
		ctx.writeBuf = protocol.AppendResponse(ctx.writeBuf, resp)
		protocol.ReleaseResponse(resp)
		return
	}

	ctx.DB = newDB
	resp := protocol.MakeSimpleString("OK")
	ctx.writeBuf = protocol.AppendResponse(ctx.writeBuf, resp)
	protocol.ReleaseResponse(resp)
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

	logger.Info("=== RediGo 服务器已停止 ===")
	return nil
}
