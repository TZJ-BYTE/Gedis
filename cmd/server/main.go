package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	
	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/server"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func main() {
	// 加载配置
	cfg := config.DefaultConfig()
	
	// 初始化日志
	if err := logger.Init(cfg.LogPath, cfg.LogLevel); err != nil {
		fmt.Printf("初始化日志失败：%v\n", err)
		os.Exit(1)
	}
	
	logger.Info("正在启动 Gedis 服务器...")
	logger.Info("配置：Host=%s, Port=%d, DBCount=%d", cfg.Host, cfg.Port, cfg.DBCount)
	
	// 创建服务器
	srv := server.NewServer(cfg)
	
	// 处理信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		<-sigChan
		logger.Info("收到退出信号，正在关闭...")
		srv.Stop()
		os.Exit(0)
	}()
	
	// 启动服务器
	if err := srv.Start(); err != nil {
		logger.Error("服务器启动失败：%v", err)
		fmt.Printf("服务器启动失败：%v\n", err)
		os.Exit(1)
	}
}
