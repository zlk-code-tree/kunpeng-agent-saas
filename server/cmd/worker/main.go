package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/platform/database"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/hibiken/asynq"
)

// main 启动异步任务 Worker、Outbox 派发、租约恢复和集成健康检查。
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
	if err := database.ValidateInstallation(context.Background(), db, cfg.Mode, 5); err != nil {
		slog.Error("installation validation failed", "error", err)
		os.Exit(1)
	}
	credentialBox, err := secretbox.New(cfg.CredentialKey)
	if err != nil {
		slog.Error("credential encryption startup failed", "error", err)
		os.Exit(1)
	}
	processor := execution.NewProcessor(db, credentialBox, dify.NewClient(cfg.Dify.Timeout), billing.NewService(db))
	queueClient := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer queueClient.Close()
	dispatcher := execution.NewDispatcher(db, queueClient)
	integrationService := integration.NewService(db, credentialBox)
	// 后台循环维护两类异步控制面：高频处理 Outbox 和运行租约，低频检查外部集成健康。
	// 两类任务均设计为幂等，可安全运行多个 Worker 实例。
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		healthTicker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		defer healthTicker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				dispatched, dispatchErr := dispatcher.DispatchPending(ctx, 100)
				recovered, recoveryErr := dispatcher.RecoverExpiredLeases(ctx)
				cancel()
				if dispatchErr != nil {
					slog.Error("outbox dispatch failed", "error", dispatchErr)
				} else if dispatched > 0 {
					slog.Info("outbox events dispatched", "count", dispatched)
				}
				if recoveryErr != nil {
					slog.Error("run lease recovery failed", "error", recoveryErr)
				} else if recovered > 0 {
					slog.Warn("expired run leases moved to unknown", "count", recovered)
				}
			case <-healthTicker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				difyCount, providerCount := integrationService.CheckAll(ctx)
				cancel()
				slog.Info("integration health checks completed", "dify_instances", difyCount, "model_providers", providerCount)
			}
		}
	}()
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
