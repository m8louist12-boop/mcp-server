package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/Correctover/mcp-server/mcp"
	"github.com/Correctover/mcp-server/provider"
	"github.com/Correctover/mcp-server/validator"
)

var (
	provManager = provider.NewManager()
	valid       = validator.New()
	// Stats
	totalCalls    int64
	totalPass     int64
	totalFailover int64
)

func main() {
	server := mcp.NewServer()
	server.SetLogWriter(os.Stderr)

	// Register tools
	server.RegisterTool(toolChat(), handleChat)
	server.RegisterTool(toolHealth(), handleHealth)
	server.RegisterTool(toolProviders(), handleProviders)
	server.RegisterTool(toolStats(), handleStats)

	log.SetOutput(os.Stderr)
	log.SetPrefix("[correctover] ")

	available := provManager.AvailableProviders()
	if len(available) == 0 {
		log.Println("WARNING: No providers configured. Set at least one API key:")
		log.Println("  OPENAI_API_KEY, ANTHROPIC_API_KEY, DEEPSEEK_API_KEY,")
		log.Println("  MOONSHOT_API_KEY, ZHIPU_API_KEY, DASHSCOPE_API_KEY, etc.")
	} else {
		log.Printf("Loaded %d providers: %s", len(available), strings.Join(available, ", "))
	}

	log.Println("Starting MCP server on stdio...")
	if err := server.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// ==================== Tool: chat ====================

func toolChat() mcp.Tool {
	return mcp.Tool{
		Name:        "chat",
		Description: "Send a chat message to an LLM with automatic output verification. Routes through the best available provider, validates the response across 6 dimensions (structure, schema, latency, cost, identity, integrity), and auto-heals on failure by retrying or failing over to another provider.",
		InputSchema: mcp.InputSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"messages": {
					Type:        "array",
					Description: "Conversation messages in OpenAI format: [{role: 'user', content: '...'}, ...]",
				},
				"model": {
					Type:        "string",
					Description: "Model name or 'auto' for automatic provider selection. Examples: gpt-4o-mini, claude-3-haiku, deepseek-chat",
				},
				"provider": {
					Type:        "string",
					Description: "Force a specific provider (openai, anthropic, deepseek, moonshot, zhipu, qwen, siliconflow, groq, together). If omitted, auto-selects by priority.",
				},
				"temperature": {
					Type:        "number",
					Description: "Sampling temperature (0.0-2.0)",
				},
				"max_tokens": {
					Type:        "integer",
					Description: "Maximum tokens in response",
				},
				"system_prompt": {
					Type:        "string",
					Description: "System prompt to prepend to messages",
				},
			},
			Required: []string{"messages"},
		},
	}
}

func handleChat(args map[string]any) (*mcp.ToolCallResult, error) {
	// Parse messages
	messagesRaw, ok := args["messages"]
	if !ok {
		return nil, fmt.Errorf("messages is required")
	}

	messagesJSON, err := json.Marshal(messagesRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid messages format: %w", err)
	}

	var messages []provider.Message
	if err := json.Unmarshal(messagesJSON, &messages); err != nil {
		return nil, fmt.Errorf("invalid messages: %w", err)
	}

	// Prepend system prompt if provided
	if sysPrompt, ok := args["system_prompt"].(string); ok && sysPrompt != "" {
		messages = append([]provider.Message{{Role: "system", Content: sysPrompt}}, messages...)
	}

	// Build request
	model, _ := args["model"].(string)
	if model == "" {
		model = "auto"
	}

	chatReq := &provider.ChatRequest{
		Model:    model,
		Messages: messages,
	}

	if temp, ok := args["temperature"].(float64); ok {
		chatReq.Temperature = &temp
	}
	if maxTok, ok := args["max_tokens"].(float64); ok {
		maxTokInt := int(maxTok)
		chatReq.MaxTokens = &maxTokInt
	}

	// Get provider list
	var providerOrder []string
	if forcedProvider, ok := args["provider"].(string); ok && forcedProvider != "" {
		providerOrder = []string{forcedProvider}
	} else {
		providerOrder = getProvidersByPriority()
	}

	if len(providerOrder) == 0 {
		return nil, fmt.Errorf("no providers available. Set at least one API key (OPENAI_API_KEY, DEEPSEEK_API_KEY, etc.)")
	}

	// Execute with validation and failover
	var lastResp *provider.ChatResponse
	var lastValidation *validator.ValidationResult
	var lastLatency int64
	var lastProvider string
	failoverCount := 0

	for i, provName := range providerOrder {
		totalCalls++

		resp, latency, callErr := provManager.Call(provName, chatReq)
		if callErr != nil {
			log.Printf("Provider %s failed: %v", provName, callErr)
			if i < len(providerOrder)-1 {
				failoverCount++
				totalFailover++
				continue
			}
			return nil, fmt.Errorf("all providers failed. Last error from %s: %w", provName, callErr)
		}

		// Validate output
		validation := valid.Validate(resp, latency)
		lastResp = resp
		lastValidation = validation
		lastLatency = latency
		lastProvider = provName

		if validation.Passed {
			totalPass++
			break // Success!
		}

		// Validation failed, try failover
		log.Printf("Provider %s output validation failed (score: %d/6): %s",
			provName, validation.Score, strings.Join(validation.Reasons, "; "))

		if i < len(providerOrder)-1 {
			failoverCount++
			totalFailover++
		}
	}

	if lastResp == nil {
		return nil, fmt.Errorf("no response received from any provider")
	}

	// Attach metadata
	lastResp.CorrectoverMeta = &provider.ResponseMeta{
		Provider:          lastProvider,
		LatencyMs:         lastLatency,
		ValidationPassed:  lastValidation.Passed,
		ValidationDetails: lastValidation.Details,
		FailoverCount:     failoverCount,
	}

	// Build response
	var result strings.Builder
	result.WriteString(lastResp.Choices[0].Message.Content)
	result.WriteString("\n\n")
	result.WriteString(validator.FormatValidationReport(lastResp, lastValidation, lastLatency, lastProvider))

	if failoverCount > 0 {
		result.WriteString(fmt.Sprintf("\n⚡ Auto-failover: %d provider(s) tried before success\n", failoverCount+1))
	}

	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.TextContent(result.String())},
	}, nil
}

// ==================== Tool: health ====================

func toolHealth() mcp.Tool {
	return mcp.Tool{
		Name:        "health",
		Description: "Check health and availability of all configured LLM providers. Shows which providers are active and ready for routing.",
		InputSchema: mcp.InputSchema{
			Type:       "object",
			Properties: map[string]mcp.Property{},
		},
	}
}

func handleHealth(args map[string]any) (*mcp.ToolCallResult, error) {
	available := provManager.AvailableProviders()
	if len(available) == 0 {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.TextContent(
				"❌ No providers configured.\n\nSet at least one API key as environment variable:\n" +
					"  OPENAI_API_KEY      → OpenAI (GPT-4o-mini)\n" +
					"  ANTHROPIC_API_KEY   → Anthropic (Claude 3 Haiku)\n" +
					"  DEEPSEEK_API_KEY    → DeepSeek (deepseek-chat)\n" +
					"  MOONSHOT_API_KEY    → Moonshot (moonshot-v1-8k)\n" +
					"  ZHIPU_API_KEY       → Zhipu AI (glm-4-flash)\n" +
					"  DASHSCOPE_API_KEY   → Alibaba Qwen (qwen-turbo)\n" +
					"  SILICONFLOW_API_KEY → SiliconFlow\n" +
					"  GROQ_API_KEY        → Groq (Llama 3)\n" +
					"  TOGETHER_API_KEY    → Together AI\n",
			)},
			IsError: false,
		}, nil
	}

	var b strings.Builder
	b.WriteString("✅ Correctover MCP Server — Provider Health\n")
	b.WriteString("═══════════════════════════════════════\n\n")

	for _, name := range available {
		p, _ := provManager.Get(name)
		b.WriteString(fmt.Sprintf("  ✅ %-15s  model: %s\n", name, p.Model))
	}

	b.WriteString(fmt.Sprintf("\n📊 %d provider(s) active | %d total calls | %d validations passed\n",
		len(available), totalCalls, totalPass))

	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.TextContent(b.String())},
	}, nil
}

// ==================== Tool: providers ====================

func toolProviders() mcp.Tool {
	return mcp.Tool{
		Name:        "providers",
		Description: "List all supported LLM providers with their configuration details and current status.",
		InputSchema: mcp.InputSchema{
			Type:       "object",
			Properties: map[string]mcp.Property{},
		},
	}
}

func handleProviders(args map[string]any) (*mcp.ToolCallResult, error) {
	type provInfo struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		Status  string `json:"status"`
		BaseURL string `json:"base_url"`
	}

	available := provManager.AvailableProviders()
	infos := make([]provInfo, 0, len(available))
	for _, name := range available {
		p, _ := provManager.Get(name)
		infos = append(infos, provInfo{
			Name:    name,
			Model:   p.Model,
			Status:  "active",
			BaseURL: p.BaseURL,
		})
	}

	data, _ := json.MarshalIndent(infos, "", "  ")
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.TextContent(string(data))},
	}, nil
}

// ==================== Tool: stats ====================

func toolStats() mcp.Tool {
	return mcp.Tool{
		Name:        "stats",
		Description: "Show Correctover session statistics: total calls, validation pass rate, failover count.",
		InputSchema: mcp.InputSchema{
			Type:       "object",
			Properties: map[string]mcp.Property{},
		},
	}
}

func handleStats(args map[string]any) (*mcp.ToolCallResult, error) {
	passRate := "0%"
	if totalCalls > 0 {
		passRate = fmt.Sprintf("%.1f%%", float64(totalPass)/float64(totalCalls)*100)
	}

	var b strings.Builder
	b.WriteString("📊 Correctover Session Statistics\n")
	b.WriteString("═══════════════════════════════════\n")
	b.WriteString(fmt.Sprintf("  Total Calls:      %d\n", totalCalls))
	b.WriteString(fmt.Sprintf("  Validation Passed: %d (%s)\n", totalPass, passRate))
	b.WriteString(fmt.Sprintf("  Failovers:        %d\n", totalFailover))
	b.WriteString(fmt.Sprintf("  Providers Active: %d\n", len(provManager.AvailableProviders())))
	b.WriteString(fmt.Sprintf("  Server Version:   %s\n", mcp.ServerVersion))

	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.TextContent(b.String())},
	}, nil
}

// ==================== Helpers ====================

func getProvidersByPriority() []string {
	available := provManager.AvailableProviders()
	// Sort by name for deterministic priority (can be enhanced with real priority scores)
	sort.Slice(available, func(i, j int) bool {
		pi, _ := provManager.Get(available[i])
		pj, _ := provManager.Get(available[j])
		if pi.Priority != pj.Priority {
			return pi.Priority < pj.Priority
		}
		return available[i] < available[j]
	})
	return available
}

func init() {
	log.SetFlags(log.Ltime)
}
