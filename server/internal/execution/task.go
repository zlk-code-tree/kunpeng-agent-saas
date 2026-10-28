package execution

import "encoding/json"

const TaskTypeRun = "ai:run"

type TaskPayload struct {
	RunID    string `json:"runId"`
	TenantID string `json:"tenantId"`
}

func EncodeTask(payload TaskPayload) ([]byte, error) {
	return json.Marshal(payload)
}
