package billing

import "testing"

// TestCalculateCostUsesExactDecimalRates 验证十进制定价不会产生浮点误差。
func TestCalculateCostUsesExactDecimalRates(t *testing.T) {
	pricing := []byte(`{"input_per_1000":0.002,"output_per_1000":0.006,"minimum":0.001}`)
	cost, err := calculateCost(pricing, 1500, 500)
	if err != nil {
		t.Fatal(err)
	}
	if got := cost.FloatString(8); got != "0.00600000" {
		t.Fatalf("unexpected cost: %s", got)
	}
}

// TestCalculateCostAppliesMinimum 验证低用量请求会应用最低消费。
func TestCalculateCostAppliesMinimum(t *testing.T) {
	pricing := []byte(`{"input_per_1000":0.002,"output_per_1000":0.006,"minimum":0.001}`)
	cost, err := calculateCost(pricing, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := cost.FloatString(8); got != "0.00100000" {
		t.Fatalf("unexpected minimum cost: %s", got)
	}
}

// TestUsageTokenCountsSupportsDifyFields 验证 Dify Token 字段别名可以被统一识别。
func TestUsageTokenCountsSupportsDifyFields(t *testing.T) {
	input, output, total := UsageTokenCounts(map[string]any{
		"prompt_tokens":     float64(12),
		"completion_tokens": "8",
		"total_tokens":      float64(20),
	})
	if input != 12 || output != 8 || total != 20 {
		t.Fatalf("unexpected token counts: input=%d output=%d total=%d", input, output, total)
	}
}
