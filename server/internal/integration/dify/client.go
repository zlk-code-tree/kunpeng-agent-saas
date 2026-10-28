package dify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
}

type RunRequest struct {
	BaseURL        string
	APIKey         string
	AppType        string
	User           string
	Query          string
	Inputs         map[string]any
	ConversationID string
}

type RunResult struct {
	Answer         string         `json:"answer,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	MessageID      string         `json:"messageId,omitempty"`
	TaskID         string         `json:"taskId,omitempty"`
	WorkflowRunID  string         `json:"workflowRunId,omitempty"`
	Outputs        map[string]any `json:"outputs,omitempty"`
	Usage          map[string]any `json:"usage,omitempty"`
}

type StreamEvent struct {
	Event          string         `json:"event"`
	TaskID         string         `json:"task_id,omitempty"`
	MessageID      string         `json:"message_id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	WorkflowRunID  string         `json:"workflow_run_id,omitempty"`
	Answer         string         `json:"answer,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Code           string         `json:"code,omitempty"`
	Message        string         `json:"message,omitempty"`
	Status         int            `json:"status,omitempty"`
	Raw            map[string]any `json:"-"`
}

type StreamCallback func(StreamEvent) error

func NewClient(timeout time.Duration) *Client {
	return &Client{httpClient: &http.Client{Timeout: timeout}}
}

// RunStreaming invokes a Dify chat, agent, chatflow, or workflow app in
// streaming mode. It returns the content accumulated before the stream ends,
// including when the callback asks the caller to stop processing.
func (c *Client) RunStreaming(ctx context.Context, input RunRequest, callback StreamCallback) (RunResult, error) {
	path, body := runPayload(input, "streaming")
	resp, err := c.doJSON(ctx, http.MethodPost, input.BaseURL, path, input.APIKey, body)
	if err != nil {
		return RunResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return RunResult{}, fmt.Errorf("read Dify error response: %w", readErr)
		}
		return RunResult{}, fmt.Errorf("Dify returned %d: %s", resp.StatusCode, truncate(string(responseBody), 500))
	}

	var result RunResult
	terminal := false
	err = scanSSE(resp.Body, func(eventName string, data []byte) error {
		var event StreamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("decode Dify stream event: %w", err)
		}
		if event.Event == "" {
			event.Event = eventName
		}
		if event.Event == "ping" {
			return nil
		}
		_ = json.Unmarshal(data, &event.Raw)
		mergeStreamResult(&result, event)
		if callback != nil {
			if err := callback(event); err != nil {
				return err
			}
		}
		switch event.Event {
		case "message_end", "workflow_finished":
			terminal = true
		case "error":
			terminal = true
			message := strings.TrimSpace(event.Message)
			if message == "" {
				message = "unknown streaming error"
			}
			return fmt.Errorf("Dify stream error %s: %s", event.Code, message)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if !terminal {
		return result, errors.New("Dify stream ended before a terminal event")
	}
	return result, nil
}

// Stop requests cancellation of an upstream Dify task.
func (c *Client) Stop(ctx context.Context, input RunRequest, taskID string) error {
	if strings.TrimSpace(taskID) == "" {
		return nil
	}
	path := "/chat-messages/" + taskID + "/stop"
	if input.AppType == "workflow" {
		path = "/workflows/tasks/" + taskID + "/stop"
	}
	resp, err := c.doJSON(ctx, http.MethodPost, input.BaseURL, path, input.APIKey, map[string]any{"user": input.User})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Dify stop response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Dify stop returned %d: %s", resp.StatusCode, truncate(string(responseBody), 500))
	}
	return nil
}

func (c *Client) Run(ctx context.Context, input RunRequest) (RunResult, error) {
	path, body := runPayload(input, "blocking")
	resp, err := c.doJSON(ctx, http.MethodPost, input.BaseURL, path, input.APIKey, body)
	if err != nil {
		return RunResult{}, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return RunResult{}, fmt.Errorf("read Dify response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RunResult{}, fmt.Errorf("Dify returned %d: %s", resp.StatusCode, truncate(string(responseBody), 500))
	}
	if input.AppType == "workflow" {
		var response struct {
			TaskID        string `json:"task_id"`
			WorkflowRunID string `json:"workflow_run_id"`
			Data          struct {
				Outputs     map[string]any `json:"outputs"`
				TotalTokens any            `json:"total_tokens"`
			} `json:"data"`
		}
		if err := json.Unmarshal(responseBody, &response); err != nil {
			return RunResult{}, err
		}
		return RunResult{TaskID: response.TaskID, WorkflowRunID: response.WorkflowRunID, Outputs: response.Data.Outputs, Usage: map[string]any{"total_tokens": response.Data.TotalTokens}}, nil
	}
	var response struct {
		Answer         string `json:"answer"`
		ConversationID string `json:"conversation_id"`
		MessageID      string `json:"message_id"`
		TaskID         string `json:"task_id"`
		Metadata       struct {
			Usage map[string]any `json:"usage"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return RunResult{}, err
	}
	return RunResult{Answer: response.Answer, ConversationID: response.ConversationID, MessageID: response.MessageID, TaskID: response.TaskID, Usage: response.Metadata.Usage}, nil
}

func runPayload(input RunRequest, responseMode string) (string, map[string]any) {
	path := "/chat-messages"
	body := map[string]any{
		"inputs":        input.Inputs,
		"query":         input.Query,
		"response_mode": responseMode,
		"user":          input.User,
	}
	if input.ConversationID != "" {
		body["conversation_id"] = input.ConversationID
	}
	if input.AppType == "workflow" {
		path = "/workflows/run"
		body = map[string]any{"inputs": input.Inputs, "response_mode": responseMode, "user": input.User}
	}
	return path, body
}

func (c *Client) doJSON(ctx context.Context, method, baseURL, path, apiKey string, body any) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode Dify request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call Dify: %w", err)
	}
	return resp, nil
}

func scanSSE(reader io.Reader, callback func(eventName string, data []byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	var eventName string
	dataLines := make([]string, 0, 1)
	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := []byte(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		name := eventName
		eventName = ""
		return callback(name, data)
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Dify stream: %w", err)
	}
	return flush()
}

func mergeStreamResult(result *RunResult, event StreamEvent) {
	if event.TaskID != "" {
		result.TaskID = event.TaskID
	}
	if event.MessageID != "" {
		result.MessageID = event.MessageID
	}
	if event.ConversationID != "" {
		result.ConversationID = event.ConversationID
	}
	if event.WorkflowRunID != "" {
		result.WorkflowRunID = event.WorkflowRunID
	}
	switch event.Event {
	case "agent_message", "message":
		result.Answer += event.Answer
	case "message_replace":
		result.Answer = event.Answer
	case "message_end":
		if usage, ok := event.Metadata["usage"].(map[string]any); ok {
			result.Usage = usage
		}
	case "text_chunk":
		if text, ok := event.Data["text"].(string); ok {
			result.Answer += text
		}
	case "workflow_finished":
		if outputs, ok := event.Data["outputs"].(map[string]any); ok {
			result.Outputs = outputs
		}
		result.Usage = workflowUsage(event.Data)
	}
}

func workflowUsage(data map[string]any) map[string]any {
	usage := make(map[string]any)
	for _, key := range []string{"total_tokens", "total_steps", "elapsed_time"} {
		if value, ok := data[key]; ok {
			usage[key] = value
		}
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
