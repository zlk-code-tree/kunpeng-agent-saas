package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gin-dify-saas/server/internal/app"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/platform/database"

	"github.com/redis/go-redis/v9"
)

// main 启动 API 服务，校验依赖和安装状态，并在退出信号到达时优雅关闭。
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := database.ValidateInstallation(ctx, db, cfg.Mode, 5); err != nil {
		slog.Error("installation validation failed", "error", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer func() { _ = redisClient.Close() }()
	api, err := app.NewAPI(cfg, db, redisClient)
	if err != nil {
		slog.Error("api application startup failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = api.Close() }()

	// 流式响应可能持续较长时间，因此不设置全局 WriteTimeout；具体的上游调用
	// 由 Dify 客户端自己的超时和请求上下文负责终止。
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		slog.Info("api server started", "address", cfg.HTTPAddr, "mode", cfg.Mode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	// 收到退出信号后停止接收新请求，并给予正在处理的请求最多 15 秒完成。
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}
