package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Dispatcher struct {
	db    *pgxpool.Pool
	queue *asynq.Client
}

// NewDispatcher 创建事务 Outbox 派发器。
func NewDispatcher(db *pgxpool.Pool, queue *asynq.Client) *Dispatcher {
	return &Dispatcher{db: db, queue: queue}
}

// DispatchPending 批量锁定待派发 Outbox 事件并可靠写入 Asynq 队列。
func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	tx, err := d.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// SKIP LOCKED 允许多个派发器并行消费同一张 Outbox 表，不会因一条繁忙记录相互阻塞。
	// 队列与数据库没有分布式事务，可靠性由固定 TaskID 和可重试的 pending 记录共同保证。
	rows, err := tx.Query(ctx, `
		SELECT id::text,tenant_id::text,aggregate_id::text
		FROM outbox_events
		WHERE status='pending' AND event_type='ai.run.queued' AND available_at<=NOW()
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}
	type event struct{ id, tenantID, runID string }
	events := make([]event, 0)
	for rows.Next() {
		var item event
		if err := rows.Scan(&item.id, &item.tenantID, &item.runID); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	dispatched := 0
	for _, item := range events {
		// Redis TaskID 是第二道幂等防线；冲突表示之前的尝试已经入队，可按成功处理。
		payload, _ := EncodeTask(TaskPayload{RunID: item.runID, TenantID: item.tenantID})
		_, enqueueErr := d.queue.EnqueueContext(ctx, asynq.NewTask(TaskTypeRun, payload), asynq.Queue("default"), asynq.MaxRetry(5), asynq.TaskID("run:"+item.runID))
		if enqueueErr != nil && !errors.Is(enqueueErr, asynq.ErrTaskIDConflict) {
			// 保持事件为 pending 并采用有上限的线性退避；队列临时不可用不能直接把业务任务判为失败。
			_, _ = tx.Exec(ctx, `
				UPDATE outbox_events SET attempts=attempts+1,available_at=NOW()+LEAST(attempts+1,60)*INTERVAL '1 second'
				WHERE id=$1
			`, item.id)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status='processed',processed_at=NOW(),attempts=attempts+1 WHERE id=$1`, item.id); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	if err := tx.Commit(ctx); err != nil {
		return dispatched, fmt.Errorf("commit outbox dispatch: %w", err)
	}
	return dispatched, nil
}

// RecoverExpiredLeases 将租约超时的运行转为 unknown，等待人工或对账流程处理。
func (d *Dispatcher) RecoverExpiredLeases(ctx context.Context) (int64, error) {
	// 不自动重放运行中的超时任务：Worker 消失前 Dify 可能已经接收请求，
	// 自动重试可能造成重复推理和重复成本，因此必须进入明确的未知状态。
	command, err := d.db.Exec(ctx, `
		UPDATE ai_runs SET status='unknown',unknown_reason='worker lease expired; upstream execution requires reconciliation',
		lease_owner=NULL,lease_expires_at=NULL,updated_at=NOW()
		WHERE status IN ('dispatching','running') AND lease_expires_at<NOW()
	`)
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}
