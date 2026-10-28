package dify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunStreamingChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat-messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization: %s", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["response_mode"] != "streaming" {
			t.Fatalf("expected streaming mode, got %v", body["response_mode"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"event\":\"message\",\"task_id\":\"task-1\",\"message_id\":\"message-1\",\"conversation_id\":\"conversation-1\",\"answer\":\"你\"}\n\n")
		fmt.Fprint(w, "data: {\"event\":\"message\",\"task_id\":\"task-1\",\"answer\":\"好\"}\n\n")
		fmt.Fprint(w, "data: {\"event\":\"message_end\",\"task_id\":\"task-1\",\"metadata\":{\"usage\":{\"total_tokens\":12}}}\n\n")
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	var eventNames []string
	result, err := client.RunStreaming(context.Background(), RunRequest{
		BaseURL: server.URL + "/v1", APIKey: "test-key", AppType: "chat", User: "user-1", Query: "hello",
	}, func(event StreamEvent) error {
		eventNames = append(eventNames, event.Event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "你好" || result.TaskID != "task-1" || result.MessageID != "message-1" || result.ConversationID != "conversation-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Usage["total_tokens"] != float64(12) {
		t.Fatalf("unexpected usage: %#v", result.Usage)
	}
	if strings.Join(eventNames, ",") != "message,message,message_end" {
		t.Fatalf("unexpected events: %v", eventNames)
	}
}

func TestRunStreamingWorkflowAndStop(t *testing.T) {
	var stopped bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workflows/run":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: update\ndata: {\"event\":\"workflow_started\",\"task_id\":\"task-2\",\"workflow_run_id\":\"workflow-1\"}\n\n")
			fmt.Fprint(w, "data: {\"event\":\"text_chunk\",\"task_id\":\"task-2\",\"data\":{\"text\":\"完成\"}}\n\n")
			fmt.Fprint(w, "data: {\"event\":\"workflow_finished\",\"task_id\":\"task-2\",\"workflow_run_id\":\"workflow-1\",\"data\":{\"outputs\":{\"answer\":\"完成\"},\"total_tokens\":9,\"total_steps\":2}}\n\n")
		case "/v1/workflows/tasks/task-2/stop":
			stopped = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	request := RunRequest{BaseURL: server.URL + "/v1", APIKey: "test-key", AppType: "workflow", User: "user-1", Inputs: map[string]any{"name": "test"}}
	result, err := client.RunStreaming(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "完成" || result.WorkflowRunID != "workflow-1" || result.Outputs["answer"] != "完成" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if err := client.Stop(context.Background(), request, result.TaskID); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("expected workflow stop endpoint to be called")
	}
}

func TestRunStreamingReturnsPartialResultOnCallbackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"event\":\"message\",\"task_id\":\"task-3\",\"answer\":\"partial\"}\n\n")
	}))
	defer server.Close()

	wantErr := errors.New("stop stream")
	result, err := NewClient(5*time.Second).RunStreaming(context.Background(), RunRequest{
		BaseURL: server.URL, APIKey: "test-key", AppType: "chat", User: "user-1", Query: "hello",
	}, func(StreamEvent) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
	if result.Answer != "partial" || result.TaskID != "task-3" {
		t.Fatalf("expected partial result, got %#v", result)
	}
}
