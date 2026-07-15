package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/server"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func main() {
	// 加载配置
	configPath := flag.String("config", "config.yaml", "")
	flag.Parse()

	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = config.DefaultConfig()
		} else {
			fmt.Fprintf(os.Stderr, "配置文件加载失败：%v\n", err)
			os.Exit(1)
		}
	}

	// 初始化日志
	if err := logger.Init(cfg.LogPath, cfg.LogLevel); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败：%v\n", err)
		os.Exit(1)
	}

	if err != nil {
		logger.Warn("配置文件加载失败，已回退到默认配置：%v", err)
	} else {
		logger.Info("已加载配置文件：%s", *configPath)
	}

	if cfg.PprofEnabled {
		go func() {
			if err := http.ListenAndServe(cfg.PprofAddr, nil); err != nil {
				logger.Warn("pprof server error: %v", err)
			}
		}()
	}

	logger.Info("正在启动 RediGo 服务器...")
	logger.Info("配置：Host=%s, Port=%d, DBCount=%d", cfg.Host, cfg.Port, cfg.DBCount)

	// 创建服务器
	srv := server.NewServer(cfg)

	// 启动服务器（在 goroutine 中）
	go func() {
		if err := srv.Start(); err != nil {
			logger.Error("服务器启动失败：%v", err)
			os.Exit(1)
		}
	}()

	// 等待退出信号（阻塞）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("收到退出信号，正在关闭...")
	srv.Stop()
	// 等待日志 flush
	time.Sleep(200 * time.Millisecond)
	logger.Info("程序退出")
}
