package execution

import "encoding/json"

const TaskTypeRun = "ai:run"

type TaskPayload struct {
	RunID    string `json:"runId"`
	TenantID string `json:"tenantId"`
}

// EncodeTask 将运行任务编码成 Asynq 可传输的 JSON 负载。
func EncodeTask(payload TaskPayload) ([]byte, error) {
	return json.Marshal(payload)
}
