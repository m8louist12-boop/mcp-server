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

	// Register tools with annotations
	server.RegisterTool(toolChat(), handleChat)
	server.RegisterTool(toolHealth(), handleHealth)
	server.RegisterTool(toolProviders(), handleProviders)
	server.RegisterTool(toolStats(), handleStats)

	// Register prompts
	server.RegisterPrompt(promptVerify(), handlePromptVerify)
	server.RegisterPrompt(promptCompareProviders(), handlePromptCompareProviders)
	server.RegisterPrompt(promptReliabilityAudit(), handlePromptReliabilityAudit)

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
		Description: "Send a chat message to an LLM with automatic output verification. Routes through the best available provider, validates the response across 6 dimensions (structure, schema, latency, cost, identity, integrity), and auto-heals on failure by retrying or failing over to another provider. Returns the response text plus a validation report showing which dimensions passed or failed.",
		InputSchema: mcp.InputSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"messages": {
					Type:        "array",
					Description: "Conversation messages in OpenAI format: [{role: 'user', content: '...'}, ...]. Each message must have 'role' (system/user/assistant) and 'content' (string).",
				},
				"model": {
					Type:        "string",
					Description: "Model name or 'auto' for automatic provider selection. Examples: 'gpt-4o-mini', 'claude-3-haiku-20240307', 'deepseek-chat'. Default: 'auto'.",
				},
				"provider": {
					Type:        "string",
					Description: "Force a specific provider: 'openai', 'anthropic', 'deepseek', 'moonshot', 'zhipu', 'qwen', 'siliconflow', 'groq', 'together'. If omitted, auto-selects by priority and health.",
				},
				"temperature": {
					Type:        "number",
					Description: "Sampling temperature (0.0-2.0). Lower values for more deterministic output. Default: provider-specific.",
				},
				"max_tokens": {
					Type:        "integer",
					Description: "Maximum tokens in response. Limits output length to control cost and latency.",
				},
				"system_prompt": {
					Type:        "string",
					Description: "System prompt to prepend to the conversation. Useful for setting context, role, or output format requirements.",
				},
			},
			Required: []string{"messages"},
		},
		Annotations: &mcp.ToolAnnotations{
			Title:           "Verified Chat",
			Description:     "LLM chat with automatic 6-dimension output verification and self-healing failover",
			ReadOnlyHint:    mcp.BoolPtr(true),
			DestructiveHint: mcp.BoolPtr(false),
			IdempotentHint:  mcp.BoolPtr(false),
			OpenWorldHint:   mcp.BoolPtr(true),
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
		Description: "Check health and availability of all configured LLM providers. Returns a list of active providers with their default models and session statistics. Call this first to verify your configuration before using the chat tool.",
		InputSchema: mcp.InputSchema{
			Type:       "object",
			Properties: map[string]mcp.Property{},
		},
		Annotations: &mcp.ToolAnnotations{
			Title:           "Provider Health Check",
			Description:     "Returns status of all configured providers",
			ReadOnlyHint:    mcp.BoolPtr(true),
			DestructiveHint: mcp.BoolPtr(false),
			IdempotentHint:  mcp.BoolPtr(true),
			OpenWorldHint:   mcp.BoolPtr(false),
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
		Description: "List all supported LLM providers with their configuration details, default models, base URLs, and current status. Use this to see which providers are available, what model each uses by default, and whether custom base URLs are configured for proxy or mirror setups.",
		InputSchema: mcp.InputSchema{
			Type:       "object",
			Properties: map[string]mcp.Property{},
		},
		Annotations: &mcp.ToolAnnotations{
			Title:           "Provider Configuration",
			Description:     "Lists all supported providers with configuration details",
			ReadOnlyHint:    mcp.BoolPtr(true),
			DestructiveHint: mcp.BoolPtr(false),
			IdempotentHint:  mcp.BoolPtr(true),
			OpenWorldHint:   mcp.BoolPtr(false),
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
		Description: "Show Correctover session statistics including total API calls, validation pass rate, failover count, active providers, and server version. Use this after a working session to review reliability metrics and see how many self-healing events occurred.",
		InputSchema: mcp.InputSchema{
			Type:       "object",
			Properties: map[string]mcp.Property{},
		},
		Annotations: &mcp.ToolAnnotations{
			Title:           "Session Statistics",
			Description:     "Returns session reliability metrics and call statistics",
			ReadOnlyHint:    mcp.BoolPtr(true),
			DestructiveHint: mcp.BoolPtr(false),
			IdempotentHint:  mcp.BoolPtr(true),
			OpenWorldHint:   mcp.BoolPtr(false),
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

// ==================== Prompts ====================

func promptVerify() mcp.Prompt {
	return mcp.Prompt{
		Name:        "verify-output",
		Description: "Verify a specific piece of AI-generated content for correctness, completeness, and reliability across 6 dimensions. Use this when you want to check if a response is trustworthy.",
		Arguments: []mcp.PromptArgument{
			{
				Name:        "content",
				Description: "The AI-generated content to verify for correctness and completeness",
				Required:    true,
			},
			{
				Name:        "expected_format",
				Description: "Expected output format (e.g., 'JSON with fields: name, email, role' or 'markdown list with exactly 5 items')",
				Required:    false,
			},
		},
	}
}

func handlePromptVerify(args map[string]any) (*mcp.PromptGetResult, error) {
	content, _ := args["content"].(string)
	format, _ := args["expected_format"].(string)

	systemMsg := "You are a correctness verifier. Analyze the following AI-generated content and check for:\n" +
		"1. Factual accuracy — are claims verifiable?\n" +
		"2. Completeness — are all requested elements present?\n" +
		"3. Structural integrity — is the output well-formed?\n"

	if format != "" {
		systemMsg += fmt.Sprintf("4. Format compliance — does it match the expected format: %s?\n", format)
	}

	systemMsg += "\nProvide a clear pass/fail assessment with specific issues found."

	return &mcp.PromptGetResult{
		Description: "Verify AI output correctness and completeness",
		Messages: []mcp.PromptMessage{
			{Role: "system", Content: mcp.TextContent(systemMsg)},
			{Role: "user", Content: mcp.TextContent(fmt.Sprintf("Please verify this AI-generated content:\n\n%s", content))},
		},
	}, nil
}

func promptCompareProviders() mcp.Prompt {
	return mcp.Prompt{
		Name:        "compare-providers",
		Description: "Compare responses from multiple LLM providers on the same prompt to identify quality differences, hallucinations, or inconsistencies. Useful for provider selection and reliability testing.",
		Arguments: []mcp.PromptArgument{
			{
				Name:        "prompt",
				Description: "The prompt to send to all providers for comparison",
				Required:    true,
			},
			{
				Name:        "providers",
				Description: "Comma-separated list of providers to compare (e.g., 'openai,anthropic,deepseek'). Leave empty for all available.",
				Required:    false,
			},
		},
	}
}

func handlePromptCompareProviders(args map[string]any) (*mcp.PromptGetResult, error) {
	prompt, _ := args["prompt"].(string)
	providers, _ := args["providers"].(string)

	providerList := "all configured providers"
	if providers != "" {
		providerList = fmt.Sprintf("these specific providers: %s", providers)
	}

	systemMsg := "You are an LLM response comparison engine. For each provider response, evaluate:\n" +
		"1. Accuracy — factual correctness of claims\n" +
		"2. Completeness — does it address all parts of the prompt?\n" +
		"3. Hallucination risk — any fabricated data, citations, or references?\n" +
		"4. Consistency — do providers agree or contradict each other?\n" +
		"\nProvide a comparison table and recommend the most reliable response."

	return &mcp.PromptGetResult{
		Description: "Compare LLM provider responses for quality and reliability",
		Messages: []mcp.PromptMessage{
			{Role: "system", Content: mcp.TextContent(systemMsg)},
			{Role: "user", Content: mcp.TextContent(
				fmt.Sprintf("Send this prompt to %s and compare the responses:\n\nPrompt: %s\n\nAfter collecting responses, provide a detailed comparison showing which provider gave the most reliable answer.", providerList, prompt),
			)},
		},
	}, nil
}

func promptReliabilityAudit() mcp.Prompt {
	return mcp.Prompt{
		Name:        "reliability-audit",
		Description: "Run a comprehensive reliability audit on your LLM provider configuration. Tests connectivity, validates API keys, checks response quality, and identifies potential failure points in your setup.",
		Arguments: []mcp.PromptArgument{
			{
				Name:        "focus",
				Description: "Area to focus the audit on: 'connectivity' (can providers be reached?), 'quality' (output correctness?), 'latency' (response times?), or 'comprehensive' (all of the above). Default: 'comprehensive'.",
				Required:    false,
			},
		},
	}
}

func handlePromptReliabilityAudit(args map[string]any) (*mcp.PromptGetResult, error) {
	focus, _ := args["focus"].(string)
	if focus == "" {
		focus = "comprehensive"
	}

	systemMsg := "You are an LLM infrastructure reliability auditor. Perform a systematic audit:\n\n" +
		"## Audit Steps:\n" +
		"1. Run the 'health' tool to check all provider statuses\n" +
		"2. Run the 'providers' tool to review configuration details\n" +
		"3. Send a test prompt via 'chat' tool to each active provider\n" +
		"4. Validate each response using the 6-dimension validation report\n" +
		"5. Run the 'stats' tool to review session metrics\n\n" +
		"## Output Format:\n" +
		"Provide a structured audit report with:\n" +
		"- Provider status summary (active/inactive/misconfigured)\n" +
		"- Response quality scores per provider\n" +
		"- Latency comparison\n" +
		"- Risk assessment (single point of failure, no redundancy, etc.)\n" +
		"- Actionable recommendations\n"

	if focus != "comprehensive" {
		systemMsg += fmt.Sprintf("\nFocus specifically on: %s\n", focus)
	}

	return &mcp.PromptGetResult{
		Description: "Comprehensive LLM provider reliability audit",
		Messages: []mcp.PromptMessage{
			{Role: "system", Content: mcp.TextContent(systemMsg)},
			{Role: "user", Content: mcp.TextContent(fmt.Sprintf("Run a %s reliability audit on my LLM provider setup. Check all configured providers, test their responses, and give me a structured report with recommendations.", focus))},
		},
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
