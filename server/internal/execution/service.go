package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-dify-saas/server/internal/platform/ids"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("run not found")

type Service struct {
	db    *pgxpool.Pool
	queue *asynq.Client
}

type CreateInput struct {
	AgentVersionID string         `json:"agentVersionId" binding:"required,uuid"`
	ConversationID string         `json:"conversationId"`
	Query          string         `json:"query" binding:"required,min=1,max=20000"`
	Inputs         map[string]any `json:"inputs"`
}

type Run struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversationId,omitempty"`
	Status         string         `json:"status"`
	Input          map[string]any `json:"input"`
	Output         map[string]any `json:"output,omitempty"`
	ErrorCode      string         `json:"errorCode,omitempty"`
	ErrorMessage   string         `json:"errorMessage,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type Event struct {
	Sequence  int64          `json:"sequence"`
	EventType string         `json:"eventType"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"createdAt"`
}

func NewService(db *pgxpool.Pool, queue *asynq.Client) *Service {
	return &Service{db: db, queue: queue}
}

func (s *Service) Create(ctx context.Context, tenantID, userID, idempotencyKey string, input CreateInput) (Run, error) {
	if idempotencyKey == "" {
		var err error
		idempotencyKey, err = ids.New()
		if err != nil {
			return Run{}, err
		}
	}
	runID, err := ids.New()
	if err != nil {
		return Run{}, err
	}
	conversationID := strings.TrimSpace(input.ConversationID)
	messageID, err := ids.New()
	if err != nil {
		return Run{}, err
	}
	requestBody := map[string]any{"query": strings.TrimSpace(input.Query), "inputs": input.Inputs}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return Run{}, fmt.Errorf("encode run input: %w", err)
	}
	hash := sha256.Sum256(encoded)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_versions WHERE id = $1 AND status = 'published')`, input.AgentVersionID).Scan(&exists); err != nil || !exists {
		return Run{}, fmt.Errorf("agent version is not available")
	}
	if conversationID == "" {
		conversationID, err = ids.New()
		if err != nil {
			return Run{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO conversations (id, tenant_id, owner_user_id, agent_version_id, title)
			VALUES ($1, $2, $3, $4, $5)
		`, conversationID, tenantID, userID, input.AgentVersionID, truncateRunes(strings.TrimSpace(input.Query), 80))
		if err != nil {
			return Run{}, fmt.Errorf("create conversation: %w", err)
		}
	} else {
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM conversations WHERE id = $1 AND tenant_id = $2 AND owner_user_id = $3 AND status = 'active')
		`, conversationID, tenantID, userID).Scan(&exists); err != nil || !exists {
			return Run{}, fmt.Errorf("conversation is not available")
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messages (id, tenant_id, conversation_id, role, content)
		VALUES ($1, $2, $3, 'user', $4)
	`, messageID, tenantID, conversationID, strings.TrimSpace(input.Query)); err != nil {
		return Run{}, fmt.Errorf("save user message: %w", err)
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO ai_runs
			(id, tenant_id, user_id, conversation_id, agent_version_id, idempotency_key, request_hash, status, input)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'queued', $8)
	`, runID, tenantID, userID, conversationID, input.AgentVersionID, idempotencyKey, hex.EncodeToString(hash[:]), encoded)
	if err != nil {
		if isUniqueViolation(err) {
			tx.Rollback(ctx)
			return s.getByIdempotency(ctx, tenantID, userID, idempotencyKey)
		}
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_events (tenant_id, run_id, sequence, event_type, payload)
		VALUES ($1, $2, 1, 'run.queued', '{"status":"queued"}')
	`, tenantID, runID); err != nil {
		return Run{}, err
	}
	outboxID, _ := ids.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, tenant_id, aggregate_type, aggregate_id, event_type, payload)
		VALUES ($1, $2, 'ai_run', $3, 'ai.run.queued', $4)
	`, outboxID, tenantID, runID, encoded); err != nil {
		return Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	payload, _ := EncodeTask(TaskPayload{RunID: runID, TenantID: tenantID})
	if _, err := s.queue.EnqueueContext(ctx, asynq.NewTask(TaskTypeRun, payload), asynq.Queue("default"), asynq.MaxRetry(5)); err != nil {
		return Run{}, fmt.Errorf("enqueue run: %w", err)
	}
	return Run{ID: runID, ConversationID: conversationID, Status: "queued", Input: requestBody, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Service) Get(ctx context.Context, tenantID, userID, runID string) (Run, error) {
	var item Run
	var input, output []byte
	var conversationID, errorCode, errorMessage *string
	err := s.db.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, status, input, output, error_code, error_message, created_at, updated_at
		FROM ai_runs WHERE id = $1 AND tenant_id = $2 AND user_id = $3
	`, runID, tenantID, userID).Scan(&item.ID, &conversationID, &item.Status, &input, &output, &errorCode, &errorMessage, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	if conversationID != nil {
		item.ConversationID = *conversationID
	}
	if errorCode != nil {
		item.ErrorCode = *errorCode
	}
	if errorMessage != nil {
		item.ErrorMessage = *errorMessage
	}
	_ = json.Unmarshal(input, &item.Input)
	if len(output) > 0 {
		_ = json.Unmarshal(output, &item.Output)
	}
	return item, nil
}

func (s *Service) Cancel(ctx context.Context, tenantID, userID, runID string) error {
	result, err := s.db.Exec(ctx, `
		UPDATE ai_runs SET status = 'cancel_requested', updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND user_id = $3 AND status IN ('queued', 'dispatching', 'running')
	`, runID, tenantID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Events(ctx context.Context, tenantID, userID, runID string, after int64) ([]Event, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_runs WHERE id=$1 AND tenant_id=$2 AND user_id=$3)`, runID, tenantID, userID).Scan(&exists); err != nil || !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT sequence, event_type, payload, created_at FROM run_events
		WHERE run_id = $1 AND tenant_id = $2 AND sequence > $3 ORDER BY sequence LIMIT 100
	`, runID, tenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Event, 0)
	for rows.Next() {
		var event Event
		var payload []byte
		if err := rows.Scan(&event.Sequence, &event.EventType, &payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &event.Payload)
		items = append(items, event)
	}
	return items, rows.Err()
}

func (s *Service) getByIdempotency(ctx context.Context, tenantID, userID, key string) (Run, error) {
	var id string
	if err := s.db.QueryRow(ctx, `SELECT id::text FROM ai_runs WHERE tenant_id=$1 AND user_id=$2 AND idempotency_key=$3`, tenantID, userID, key).Scan(&id); err != nil {
		return Run{}, err
	}
	return s.Get(ctx, tenantID, userID, id)
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "SQLSTATE 23505")
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
