package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/config"
	"gin-dify-saas/server/internal/execution"
	"gin-dify-saas/server/internal/integration"
	"gin-dify-saas/server/internal/platform/dify"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker 表示异步执行进程。运行编排仍属于 execution 领域，app 只管理进程装配和生命周期。
type Worker struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	queue       *asynq.Client
	dispatcher  *execution.Dispatcher
	integration *integration.IntegrationService
}

// NewWorker 创建队列处理器、Outbox 派发器和集成健康检查服务。
func NewWorker(cfg config.Config, db *pgxpool.Pool) (*Worker, error) {
	credentialBox, err := secretbox.New(cfg.CredentialKey)
	if err != nil {
		return nil, fmt.Errorf("create credential encryption: %w", err)
	}
	redisOptions := asynq.RedisClientOpt{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	}
	queue := asynq.NewClient(redisOptions)
	billingService := billing.NewBillingService(db)
	processor := execution.NewProcessor(db, credentialBox, dify.NewClient(cfg.Dify.Timeout), billingAdapter{service: billingService})

	mux := asynq.NewServeMux()
	mux.HandleFunc(execution.TaskTypeRun, processor.HandleRun)
	// 知识库异步索引暂保留独立任务类型，后续实现时可直接迁移到 knowledge Worker。
	mux.HandleFunc("knowledge:index", func(_ context.Context, task *asynq.Task) error {
		slog.Info("received knowledge indexing task", "payload_bytes", len(task.Payload()))
		return nil
	})

	return &Worker{
		server: asynq.NewServer(redisOptions, asynq.Config{
			Concurrency: 10,
			Queues:      map[string]int{"critical": 6, "default": 3, "low": 1},
		}),
		mux:         mux,
		queue:       queue,
		dispatcher:  execution.NewDispatcher(db, queue),
		integration: integration.NewIntegrationService(db, credentialBox),
	}, nil
}

// Run 启动队列消费和后台控制循环，并在上下文取消后优雅停止。
func (w *Worker) Run(ctx context.Context) error {
	if err := w.server.Start(w.mux); err != nil {
		return err
	}
	go w.runControlLoop(ctx)
	<-ctx.Done()
	w.server.Shutdown()
	return nil
}

// Close 关闭 Worker 持有的队列客户端。
func (w *Worker) Close() error {
	if w == nil || w.queue == nil {
		return nil
	}
	return w.queue.Close()
}

// runControlLoop 定期派发 Outbox、恢复过期租约并检查外部集成健康状态。
func (w *Worker) runControlLoop(ctx context.Context) {
	dispatchTicker := time.NewTicker(5 * time.Second)
	healthTicker := time.NewTicker(30 * time.Second)
	defer dispatchTicker.Stop()
	defer healthTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-dispatchTicker.C:
			operationCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			dispatched, dispatchErr := w.dispatcher.DispatchPending(operationCtx, 100)
			recovered, recoveryErr := w.dispatcher.RecoverExpiredLeases(operationCtx)
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
			operationCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			difyCount, providerCount := w.integration.CheckAll(operationCtx)
			cancel()
			slog.Info("integration health checks completed", "dify_instances", difyCount, "model_providers", providerCount)
		}
	}
}
