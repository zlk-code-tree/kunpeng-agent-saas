package main

import (
	"context"
	"log/slog"
	"os"

	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/platform/database"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/hibiken/asynq"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	db, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	credentialBox, err := secretbox.New(cfg.CredentialKey)
	if err != nil {
		slog.Error("credential encryption startup failed", "error", err)
		os.Exit(1)
	}
	processor := execution.NewProcessor(db, credentialBox, dify.NewClient(cfg.Dify.Timeout))
	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB},
		asynq.Config{
			Concurrency: 10,
			Queues:      map[string]int{"critical": 6, "default": 3, "low": 1},
		},
	)
	mux := asynq.NewServeMux()
	mux.HandleFunc(execution.TaskTypeRun, processor.HandleRun)
	mux.HandleFunc("knowledge:index", func(ctx context.Context, task *asynq.Task) error {
		slog.Info("received knowledge indexing task", "payload_bytes", len(task.Payload()))
		return nil
	})
	slog.Info("worker started", "redis", cfg.Redis.Addr)
	if err := server.Run(mux); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
