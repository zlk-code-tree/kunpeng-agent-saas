// Package usage 提供不同模型供应商用量字段的技术性归一化能力。
package usage

import (
	"encoding/json"
	"strconv"
)

// TokenCounts 兼容 Dify 和常见模型供应商的字段别名，统一提取 Token 数量。
func TokenCounts(values map[string]any) (input, output, total int64) {
	input = firstInt(values, "prompt_tokens", "input_tokens")
	output = firstInt(values, "completion_tokens", "output_tokens")
	total = firstInt(values, "total_tokens")
	if total == 0 {
		total = input + output
	}
	return
}

// firstInt 按字段优先级读取第一个可转换为整数的值。
func firstInt(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case float64:
			return int64(value)
		case int:
			return int64(value)
		case int64:
			return value
		case json.Number:
			parsed, _ := value.Int64()
			return parsed
		case string:
			parsed, _ := strconv.ParseInt(value, 10, 64)
			return parsed
		}
	}
	return 0
}
