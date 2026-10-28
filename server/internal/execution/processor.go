package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"gin-dify-saas/server/internal/integration/dify"
	"gin-dify-saas/server/internal/platform/ids"
	"gin-dify-saas/server/internal/platform/secretbox"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errRunCanceled = errors.New("run cancellation requested")

type Processor struct {
	db     *pgxpool.Pool
	box    *secretbox.Box
	client *dify.Client
}

func NewProcessor(db *pgxpool.Pool, box *secretbox.Box, client *dify.Client) *Processor {
	return &Processor{db: db, box: box, client: client}
}

func (p *Processor) HandleRun(ctx context.Context, task *asynq.Task) error {
	var payload TaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode run task: %w", err)
	}
	result, err := p.db.Exec(ctx, `
		UPDATE ai_runs SET status='running', started_at=COALESCE(started_at, NOW()), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND status IN ('queued', 'dispatching')
	`, payload.RunID, payload.TenantID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var status string
		if err := p.db.QueryRow(ctx, `SELECT status FROM ai_runs WHERE id=$1 AND tenant_id=$2`, payload.RunID, payload.TenantID).Scan(&status); err == nil && status == "cancel_requested" {
			_, _ = p.db.Exec(ctx, `UPDATE ai_runs SET status='stopped', finished_at=NOW(), updated_at=NOW() WHERE id=$1 AND status='cancel_requested'`, payload.RunID)
			_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.stopped", map[string]any{"status": "stopped"})
		}
		return nil
	}
	_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.started", map[string]any{"status": "running"})

	var appType, baseURL, userID, conversationID, upstreamConversationID string
	var encryptedKey, inputJSON []byte
	err = p.db.QueryRow(ctx, `
		SELECT v.app_type, i.base_url, b.encrypted_api_key, r.input, r.user_id::text,
		       COALESCE(r.conversation_id::text, ''), COALESCE(c.upstream_conversation_id, '')
		FROM ai_runs r
		JOIN agent_versions v ON v.id = r.agent_version_id
		JOIN dify_app_bindings b ON b.agent_version_id = r.agent_version_id
		  AND b.status='active' AND (b.tenant_id=r.tenant_id OR b.tenant_id IS NULL)
		JOIN dify_instances i ON i.id=b.dify_instance_id AND i.status='active'
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
			UPDATE ai_runs SET status='stopped', output=$2, upstream_task_id=$3,
			upstream_workflow_run_id=$4, finished_at=NOW(), updated_at=NOW()
			WHERE id=$1
		`, payload.RunID, outputJSON, nullString(taskID), nullString(runResult.WorkflowRunID))
		if updateErr != nil {
			return updateErr
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
		status = "stopped"
	}
	_, err = p.db.Exec(ctx, `
		UPDATE ai_runs SET status=$2, output=$3, upstream_task_id=$4,
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
		usageID, _ := ids.New()
		usageJSON, _ := json.Marshal(runResult.Usage)
		_, _ = p.db.Exec(ctx, `
			INSERT INTO usage_records (id, tenant_id, run_id, source, upstream_usage)
			VALUES ($1, $2, $3, 'dify', $4)
		`, usageID, payload.TenantID, payload.RunID, usageJSON)
	}
	return p.appendEvent(ctx, payload.TenantID, payload.RunID, "run."+status, map[string]any{"status": status, "output": runResult})
}

func (p *Processor) monitorCancellation(ctx context.Context, runID string, canceled *atomic.Bool, cancel context.CancelFunc, done <-chan struct{}) {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
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
		}
	}
}

func (p *Processor) fail(ctx context.Context, payload TaskPayload, code, message string) {
	_, _ = p.db.Exec(ctx, `UPDATE ai_runs SET status='failed', error_code=$2, error_message=$3, finished_at=NOW(), updated_at=NOW() WHERE id=$1`, payload.RunID, code, message)
	_ = p.appendEvent(ctx, payload.TenantID, payload.RunID, "run.failed", map[string]any{"status": "failed", "code": code, "message": message})
}

func (p *Processor) appendEvent(ctx context.Context, tenantID, runID, eventType string, payload map[string]any) error {
	encoded, _ := json.Marshal(payload)
	_, err := p.db.Exec(ctx, `
		INSERT INTO run_events (tenant_id, run_id, sequence, event_type, payload)
		SELECT $1, $2, COALESCE(MAX(sequence),0)+1, $3, $4 FROM run_events WHERE run_id=$2
	`, tenantID, runID, eventType, encoded)
	return err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
