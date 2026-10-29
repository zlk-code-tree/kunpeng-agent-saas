package execution

import "time"

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
