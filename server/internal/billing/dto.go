package billing

type AdjustmentInput struct {
	Amount         string `json:"amount" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required,min=8,max=128"`
	Reason         string `json:"reason" binding:"required,min=2,max=255"`
}
