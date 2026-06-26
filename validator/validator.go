package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Correctover/mcp-server/provider"
)

// ---- 6维合约验证 ----
// Structure: 输出结构完整性
// Schema:    输出格式合规性
// Latency:   延迟合理性
// Cost:      成本合理性
// Identity:  身份/角色一致性
// Integrity: 数据完整性

type ValidationResult struct {
	Passed    bool              `json:"passed"`
	Details   map[string]bool   `json:"details"`    // dimension -> pass/fail
	Score     int               `json:"score"`      // 0-6
	Reasons   []string          `json:"reasons,omitempty"`
}

type Validator struct{}

func New() *Validator {
	return &Validator{}
}

func (v *Validator) Validate(resp *provider.ChatResponse, latencyMs int64) *ValidationResult {
	result := &ValidationResult{
		Details: make(map[string]bool),
		Score:   0,
	}

	// 1. Structure — 输出结构完整性
	structure := v.checkStructure(resp)
	result.Details["structure"] = structure
	if structure {
		result.Score++
	} else {
		result.Reasons = append(result.Reasons, "structure: empty or malformed response")
	}

	// 2. Schema — 输出格式合规性
	schema := v.checkSchema(resp)
	result.Details["schema"] = schema
	if schema {
		result.Score++
	} else {
		result.Reasons = append(result.Reasons, "schema: output format non-compliant")
	}

	// 3. Latency — 延迟合理性 (< 60s = pass)
	latency := v.checkLatency(latencyMs)
	result.Details["latency"] = latency
	if latency {
		result.Score++
	} else {
		result.Reasons = append(result.Reasons, fmt.Sprintf("latency: %dms exceeds threshold", latencyMs))
	}

	// 4. Cost — 成本合理性 (token count > 0 and reasonable)
	cost := v.checkCost(resp)
	result.Details["cost"] = cost
	if cost {
		result.Score++
	} else {
		result.Reasons = append(result.Reasons, "cost: abnormal token usage")
	}

	// 5. Identity — 身份/角色一致性
	identity := v.checkIdentity(resp)
	result.Details["identity"] = identity
	if identity {
		result.Score++
	} else {
		result.Reasons = append(result.Reasons, "identity: role consistency check failed")
	}

	// 6. Integrity — 数据完整性
	integrity := v.checkIntegrity(resp)
	result.Details["integrity"] = integrity
	if integrity {
		result.Score++
	} else {
		result.Reasons = append(result.Reasons, "integrity: data truncation detected")
	}

	result.Passed = result.Score >= 4 // 至少4/6维度通过
	return result
}

// checkStructure: 确保响应有有效的 choice 和 content
func (v *Validator) checkStructure(resp *provider.ChatResponse) bool {
	if len(resp.Choices) == 0 {
		return false
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return false
	}
	return true
}

// checkSchema: 检查 finish_reason 合理性
func (v *Validator) checkSchema(resp *provider.ChatResponse) bool {
	if len(resp.Choices) == 0 {
		return false
	}
	reason := resp.Choices[0].FinishReason
	// "stop" or "end_turn" are normal completions
	validReasons := map[string]bool{
		"stop":     true,
		"end_turn": true,
		"length":   true, // truncated but still valid schema
	}
	return validReasons[reason]
}

// checkLatency: 延迟在合理范围内
func (v *Validator) checkLatency(latencyMs int64) bool {
	return latencyMs > 0 && latencyMs < 60000 // 60秒上限
}

// checkCost: token 使用量合理
func (v *Validator) checkCost(resp *provider.ChatResponse) bool {
	// usage 可能不存在（某些provider不返回），此时不扣分
	if resp.Usage.TotalTokens == 0 {
		return true // 无usage信息时默认通过
	}
	// 总token不应为负，且 completion 不应超过合理上限
	if resp.Usage.TotalTokens < 0 || resp.Usage.CompletionTokens < 0 {
		return false
	}
	// 单次响应超过100K tokens 视为异常
	if resp.Usage.CompletionTokens > 100000 {
		return false
	}
	return true
}

// checkIdentity: 检查 assistant 角色一致性
func (v *Validator) checkIdentity(resp *provider.ChatResponse) bool {
	if len(resp.Choices) == 0 {
		return false
	}
	role := resp.Choices[0].Message.Role
	return role == "assistant"
}

// checkIntegrity: 检测数据截断
func (v *Validator) checkIntegrity(resp *provider.ChatResponse) bool {
	if len(resp.Choices) == 0 {
		return false
	}
	content := resp.Choices[0].Message.Content
	finishReason := resp.Choices[0].FinishReason

	// 如果是 length 截断，检查是否有明显的 JSON/代码截断
	if finishReason == "length" {
		// 检查是否是未闭合的 JSON
		trimmed := strings.TrimSpace(content)
		if strings.HasPrefix(trimmed, "{") && !isValidJSON(trimmed) {
			return false
		}
		if strings.HasPrefix(trimmed, "[") && !isValidJSON(trimmed) {
			return false
		}
	}

	// 检查内容是否包含明显的截断标记
	if strings.HasSuffix(content, "...") || strings.HasSuffix(content, "…") {
		return false
	}

	return true
}

func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

// FormatValidationReport 生成人类可读的验证报告
func FormatValidationReport(resp *provider.ChatResponse, result *ValidationResult, latencyMs int64, providerName string) string {
	var b strings.Builder
	b.WriteString("╔══════════════════════════════════════╗\n")
	b.WriteString("║   Correctover Validation Report     ║\n")
	b.WriteString("╠══════════════════════════════════════╣\n")
	b.WriteString(fmt.Sprintf("║ Provider: %-27s║\n", providerName))
	b.WriteString(fmt.Sprintf("║ Latency:  %-27s║\n", fmt.Sprintf("%dms", latencyMs)))
	b.WriteString(fmt.Sprintf("║ Model:    %-27s║\n", resp.Model))
	b.WriteString(fmt.Sprintf("║ Score:    %-27s║\n", fmt.Sprintf("%d/6", result.Score)))
	b.WriteString(fmt.Sprintf("║ Passed:   %-27s║\n", fmt.Sprintf("%v", result.Passed)))
	b.WriteString("╠══════════════════════════════════════╣\n")

	dims := []string{"structure", "schema", "latency", "cost", "identity", "integrity"}
	for _, d := range dims {
		status := "✅"
		if !result.Details[d] {
			status = "❌"
		}
		b.WriteString(fmt.Sprintf("║ %s %-8s %-21s║\n", status, d, statusLabel(result.Details[d])))
	}

	b.WriteString("╠══════════════════════════════════════╣\n")
	if len(result.Reasons) > 0 {
		for _, r := range result.Reasons {
			b.WriteString(fmt.Sprintf("║ ⚠  %-30s║\n", truncate(r, 30)))
		}
	} else {
		b.WriteString("║ ✓ All dimensions passed            ║\n")
	}
	b.WriteString("╚══════════════════════════════════════╝\n")

	return b.String()
}

func statusLabel(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
