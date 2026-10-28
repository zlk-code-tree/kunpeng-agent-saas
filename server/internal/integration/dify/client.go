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

// NewClient 创建带统一请求超时的 Dify API 客户端。
func NewClient(timeout time.Duration) *Client {
	return &Client{httpClient: &http.Client{Timeout: timeout}}
}

// RunStreaming 以流式模式调用 Dify 聊天、Agent、Chatflow 或 Workflow 应用。
// 即使回调要求提前停止，也会返回停止前已经累计的任务标识、内容和用量。
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
	// 先合并事件再调用回调。即使回调因用户取消而终止处理，调用方仍能获得已观察到的
	// task id、部分输出和用量，用于停止上游任务及完成结算。
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

// Stop 请求取消上游 Dify 任务；空 taskID 按无需处理返回成功。
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

// Run 以阻塞模式调用 Dify，并将不同应用类型的响应归一为 RunResult。
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

// runPayload 根据 Dify 应用类型选择接口地址并构造对应请求体。
func runPayload(input RunRequest, responseMode string) (string, map[string]any) {
	// 会话应用和工作流使用不同端点及参数结构，在客户端内部统一映射，避免业务层重复判断。
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

// doJSON 构造带 Dify API Key 的 JSON 请求并返回原始 HTTP 响应。
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

// scanSSE 按 SSE 规范解析事件名和多行 data，并把完整事件交给回调处理。
func scanSSE(reader io.Reader, callback func(eventName string, data []byte) error) error {
	// 空行表示事件结束，冒号开头为心跳注释；工作流输出可能显著超过 64 KiB，
	// 因此把 Scanner 最大缓冲区扩大到 16 MiB。
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

// mergeStreamResult 将不同类型的流事件累积到统一运行结果中。
func mergeStreamResult(result *RunResult, event StreamEvent) {
	// Dify 可能在不同事件中返回标识，只覆盖非空值；普通消息追加增量，
	// message_replace 则按协议要求整体替换现有答案。
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

// workflowUsage 从工作流结束事件中提取计费和性能相关用量字段。
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

// truncate 限制上游错误正文长度，避免异常响应污染日志和 API 错误。
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
