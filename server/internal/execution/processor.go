package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"gin-dify-saas/server/internal/billing"
	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errRunCanceled = errors.New("run cancellation requested")

type Processor struct {
	db       *pgxpool.Pool
	box      *secretbox.Box
	client   *dify.Client
	billing  *billing.Service
	workerID string
}

// NewProcessor 创建运行任务处理器，并为当前 Worker 进程生成唯一租约所有者标识。
func NewProcessor(db *pgxpool.Pool, box *secretbox.Box, client *dify.Client, billingService *billing.Service) *Processor {
	// 进程级 owner id 用于区分当前 Worker、重启后的 Worker 和并发 Worker。
	workerID, _ := ids.New()
	return &Processor{db: db, box: box, client: client, billing: billingService, workerID: workerID}
}

// HandleRun 领取运行租约、流式调用 Dify、保存消息和用量，并完成最终计费。
func (p *Processor) HandleRun(ctx context.Context, task *asynq.Task) error {
	var payload TaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode run task: %w", err)
	}
	// 通过状态条件更新实现 compare-and-set 领取。只有成功把 queued/dispatching 改为 running
	// 的 Worker 才能调用 Dify，重复投递的队列任务不会再次调用模型。
	result, err := p.db.Exec(ctx, `
		UPDATE ai_runs SET status='running',started_at=COALESCE(started_at,NOW()),lease_owner=$3,
		lease_expires_at=NOW()+INTERVAL '2 minutes',dispatch_attempts=dispatch_attempts+1,updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND status IN ('queued', 'dispatching')
	`, payload.RunID, payload.TenantID, p.workerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		// 取消可能发生在 Worker 领取任务之前，此时尚未产生上游调用和用量，可安全释放全部预占。
		var status string
		if err := p.db.QueryRow(ctx, `SELECT status FROM ai_runs WHERE id=$1 AND tenant_id=$2`, payload.RunID, payload.TenantID).Scan(&status); err == nil && status == "cancel_requested" {
			_, _ = p.db.Exec(ctx, `UPDATE ai_runs SET status='stopped',lease_owner=NULL,lease_expires_at=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1 AND status='cancel_requested'`, payload.RunID)
			_ = p.billing.Release(ctx, payload.RunID, "canceled before dispatch")
			_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.stopped", map[string]any{"status": "stopped"})
		}
		return nil
	}
	_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.started", map[string]any{"status": "running"})

	var appType, baseURL, userID, conversationID, upstreamConversationID string
	var encryptedKey, inputJSON []byte
	// 租户专属绑定优先于平台共享绑定，并排除停用或不可用实例，
	// 防止任务错误路由到其他租户的独享 Dify 实例。
	err = p.db.QueryRow(ctx, `
		SELECT v.app_type, i.base_url, b.encrypted_api_key, r.input, r.user_id::text,
		       COALESCE(r.conversation_id::text, ''), COALESCE(c.upstream_conversation_id, '')
		FROM ai_runs r
		JOIN agent_versions v ON v.id = r.agent_version_id
		JOIN dify_app_bindings b ON b.agent_version_id = r.agent_version_id
		  AND b.status='active' AND (b.tenant_id=r.tenant_id OR b.tenant_id IS NULL)
		JOIN dify_instances i ON i.id=b.dify_instance_id AND i.status='active' AND i.health_status<>'unavailable'
		LEFT JOIN conversations c ON c.id=r.conversation_id
		WHERE r.id=$1 AND r.tenant_id=$2
		ORDER BY (b.tenant_id IS NOT NULL) DESC LIMIT 1
	`, payload.RunID, payload.TenantID).Scan(&appType, &baseURL, &encryptedKey, &inputJSON, &userID, &conversationID, &upstreamConversationID)
	if err != nil {
		p.fail(ctx, payload, "DIFY_BINDING_MISSING", "Dify app binding is missing")
		return nil
	}
	apiKey, err := p.box.Open(encryptedKey)
	if err != nil {
		p.fail(ctx, payload, "CREDENTIAL_DECRYPT_FAILED", "Dify credential cannot be decrypted")
		return nil
	}
	var runInput struct {
		Query  string         `json:"query"`
		Inputs map[string]any `json:"inputs"`
	}
	if err := json.Unmarshal(inputJSON, &runInput); err != nil {
		return err
	}
	runRequest := dify.RunRequest{
		BaseURL: baseURL, APIKey: apiKey, AppType: appType, User: userID,
		Query: runInput.Query, Inputs: runInput.Inputs, ConversationID: upstreamConversationID,
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	// 取消监控同时续租。用户取消请求保存在数据库中，流式回调不能只依赖当前请求上下文判断取消。
	var userCanceled atomic.Bool
	monitorDone := make(chan struct{})
	go p.monitorCancellation(ctx, payload.RunID, &userCanceled, cancelStream, monitorDone)

	var upstreamTaskID string
	runResult, err := p.client.RunStreaming(streamCtx, runRequest, func(event dify.StreamEvent) error {
		if event.TaskID != "" && upstreamTaskID == "" {
			upstreamTaskID = event.TaskID
			_, updateErr := p.db.Exec(ctx, `UPDATE ai_runs SET upstream_task_id=$2, updated_at=NOW() WHERE id=$1`, payload.RunID, event.TaskID)
			if updateErr != nil {
				return updateErr
			}
		}
		if userCanceled.Load() {
			return errRunCanceled
		}
		switch event.Event {
		case "agent_message", "message":
			if event.Answer != "" {
				return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.delta", map[string]any{"delta": event.Answer})
			}
		case "message_replace":
			return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.replace", map[string]any{"content": event.Answer})
		case "text_chunk":
			if text, ok := event.Data["text"].(string); ok && text != "" {
				return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.delta", map[string]any{"delta": text})
			}
		case "agent_thought":
			return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.agent_thought", event.Raw)
		case "node_started", "node_finished":
			return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.workflow."+event.Event, event.Raw)
		}
		return nil
	})
	cancelStream()
	close(monitorDone)

	if userCanceled.Load() || errors.Is(err, errRunCanceled) {
		// 已获得上游 task id 时主动请求 Dify 停止，并结算已经上报的用量；
		// 只有完全没有确认用量时才释放全部预占。
		taskID := runResult.TaskID
		if taskID == "" {
			taskID = upstreamTaskID
		}
		var stopError string
		if taskID != "" {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if stopErr := p.client.Stop(stopCtx, runRequest, taskID); stopErr != nil {
				stopError = stopErr.Error()
			}
			stopCancel()
		}
		outputJSON, _ := json.Marshal(runResult)
		_, updateErr := p.db.Exec(ctx, `
			UPDATE ai_runs SET status='stopped',output=$2,upstream_task_id=$3,lease_owner=NULL,lease_expires_at=NULL,
			upstream_workflow_run_id=$4, finished_at=NOW(), updated_at=NOW()
			WHERE id=$1
		`, payload.RunID, outputJSON, nullString(taskID), nullString(runResult.WorkflowRunID))
		if updateErr != nil {
			return updateErr
		}
		if len(runResult.Usage) > 0 {
			_, _ = p.saveUsage(ctx, payload, runResult.Usage)
			_ = p.billing.Settle(ctx, payload.RunID, runResult.Usage)
		} else {
			_ = p.billing.Release(ctx, payload.RunID, "canceled without confirmed usage")
		}
		payloadBody := map[string]any{"status": "stopped", "output": runResult}
		if stopError != "" {
			payloadBody["upstreamStopError"] = stopError
		}
		return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.stopped", payloadBody)
	}
	if err != nil {
		p.fail(ctx, payload, "DIFY_REQUEST_FAILED", err.Error())
		return nil
	}
	outputJSON, _ := json.Marshal(runResult)
	status := "succeeded"
	var currentStatus string
	_ = p.db.QueryRow(ctx, `SELECT status FROM ai_runs WHERE id=$1`, payload.RunID).Scan(&currentStatus)
	if currentStatus == "cancel_requested" {
		// 取消请求可能与 Dify 终态事件并发到达。保留用户的 stopped 语义，
		// 但仍需保存已生成输出并结算已经上报的用量。
		status = "stopped"
	}
	_, err = p.db.Exec(ctx, `
		UPDATE ai_runs SET status=$2,output=$3,upstream_task_id=$4,lease_owner=NULL,lease_expires_at=NULL,
		upstream_workflow_run_id=$5, finished_at=NOW(), updated_at=NOW()
		WHERE id=$1
	`, payload.RunID, status, outputJSON, nullString(runResult.TaskID), nullString(runResult.WorkflowRunID))
	if err != nil {
		return err
	}
	if conversationID != "" && runResult.ConversationID != "" {
		_, _ = p.db.Exec(ctx, `UPDATE conversations SET upstream_conversation_id=$2, updated_at=NOW() WHERE id=$1`, conversationID, runResult.ConversationID)
	}
	if conversationID != "" && runResult.Answer != "" {
		messageID, _ := ids.New()
		metadata, _ := json.Marshal(map[string]any{"usage": runResult.Usage})
		_, _ = p.db.Exec(ctx, `
			INSERT INTO messages (id, tenant_id, conversation_id, role, content, upstream_message_id, metadata)
			VALUES ($1, $2, $3, 'assistant', $4, $5, $6)
		`, messageID, payload.TenantID, conversationID, runResult.Answer, nullString(runResult.MessageID), metadata)
	}
	if len(runResult.Usage) > 0 {
		_, _ = p.saveUsage(ctx, payload, runResult.Usage)
	}
	if settleErr := p.billing.Settle(ctx, payload.RunID, runResult.Usage); settleErr != nil {
		_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.billing_review", map[string]any{"message": settleErr.Error()})
	}
	return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run."+status, map[string]any{"status": status, "output": runResult})
}

// saveUsage 幂等保存 Dify 用量，并进一步生成供应商成本明细。
func (p *Processor) saveUsage(ctx context.Context, payload TaskPayload, usage map[string]any) (string, error) {
	usageID, _ := ids.New()
	usageJSON, _ := json.Marshal(usage)
	inputTokens, outputTokens, totalTokens := billing.UsageTokenCounts(usage)
	provider, _ := usage["provider"].(string)
	model, _ := usage["model"].(string)
	// 每次运行只有一条权威 Dify 用量记录。Upsert 可安全处理流重试和迟到的终态事件，
	// 供应商成本记录另有唯一约束防止重复生成。
	err := p.db.QueryRow(ctx, `
		INSERT INTO usage_records (
			id,tenant_id,run_id,source,provider,model,input_tokens,output_tokens,total_tokens,upstream_usage
		) VALUES ($1,$2,$3,'dify',$4,$5,$6,$7,$8,$9)
		ON CONFLICT (run_id,source) DO UPDATE SET
			provider=EXCLUDED.provider,model=EXCLUDED.model,input_tokens=EXCLUDED.input_tokens,
			output_tokens=EXCLUDED.output_tokens,total_tokens=EXCLUDED.total_tokens,
			upstream_usage=EXCLUDED.upstream_usage
		RETURNING id::text
	`, usageID, payload.TenantID, payload.RunID, nullString(provider), nullString(model), inputTokens, outputTokens, totalTokens, usageJSON).Scan(&usageID)
	if err != nil {
		return "", err
	}
	if err := p.billing.RecordProviderCost(ctx, payload.TenantID, payload.RunID, usageID, usage); err != nil {
		return usageID, err
	}
	return usageID, nil
}

// monitorCancellation 轮询取消状态并定期续期当前 Worker 持有的运行租约。
func (p *Processor) monitorCancellation(ctx context.Context, runID string, canceled *atomic.Bool, cancel context.CancelFunc, done <-chan struct{}) {
	// 取消状态采用高频轮询保证响应速度，租约心跳采用较低频率减少数据库压力。
	ticker := time.NewTicker(400 * time.Millisecond)
	leaseTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer leaseTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			var status string
			if err := p.db.QueryRow(ctx, `SELECT status FROM ai_runs WHERE id=$1`, runID).Scan(&status); err == nil && status == "cancel_requested" {
				canceled.Store(true)
				cancel()
				return
			}
		case <-leaseTicker.C:
			_, _ = p.db.Exec(ctx, `UPDATE ai_runs SET lease_expires_at=NOW()+INTERVAL '2 minutes',updated_at=NOW() WHERE id=$1 AND lease_owner=$2 AND status='running'`, runID, p.workerID)
		}
	}
}

// fail 将运行置为失败终态，释放额度并追加失败事件。
func (p *Processor) fail(ctx context.Context, payload TaskPayload, code, message string) {
	// 先持久化失败终态再释放资金；释放操作本身幂等，队列重试不会重复解冻同一笔预占。
	_, _ = p.db.Exec(ctx, `UPDATE ai_runs SET status='failed',error_code=$2,error_message=$3,lease_owner=NULL,lease_expires_at=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1`, payload.RunID, code, message)
	_ = p.billing.Release(ctx, payload.RunID, code)
	_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.failed", map[string]any{"status": "failed", "code": code, "message": message})
}

// appendEvent 为运行追加一个单调递增序号的事件，供 SSE 客户端按序消费。
func (p *Processor) appendEvent(ctx context.Context, tenantID, runID, eventType string, payload map[string]any) error {
	// 已领取的运行只由一个 Processor 持续写入，因此 MAX(sequence)+1 能保持流式事件顺序；
	// 其他写入方只在状态边界追加事件，数据库唯一约束作为最终并发保护。
	encoded, _ := json.Marshal(payload)
	_, err := p.db.Exec(ctx, `
		INSERT INTO run_events (tenant_id, run_id, sequence, event_type, payload)
		SELECT $1, $2, COALESCE(MAX(sequence),0)+1, $3, $4 FROM run_events WHERE run_id=$2
	`, tenantID, runID, eventType, encoded)
	return err
}

// nullString 将空字符串转换为数据库 NULL，避免把“未知”保存成有意义的空值。
func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
