package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gin-dify-saas/server/internal/app"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/platform/database"
)

// main 只负责读取配置、连接数据库和管理进程信号；业务依赖由 app.Worker 统一装配。
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	worker, err := app.NewWorker(cfg, db)
	if err != nil {
		slog.Error("worker application startup failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = worker.Close() }()

	slog.Info("worker started", "redis", cfg.Redis.Addr)
	if err := worker.Run(ctx); err != nil {
		slog.Error("worker stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
