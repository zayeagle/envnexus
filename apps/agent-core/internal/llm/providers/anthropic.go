package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
)

type AnthropicProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewAnthropicProvider(cfg router.ProviderConfig) *AnthropicProvider {
	name := cfg.Name
	if name == "" {
		name = "anthropic"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	return &AnthropicProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *AnthropicProvider) Name() string { return p.name }

func (p *AnthropicProvider) IsAvailable() bool {
	return p.apiKey != "" && p.baseURL != ""
}

type anthropicToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicToolDef `json:"tools,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

// anthropicMessage uses json.RawMessage for content so the API receives either a
// JSON string or an array of content blocks (text, tool_use, tool_result).
type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []anthropicResponseContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type anthropicResponseContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func routerToolsToAnthropic(defs []router.ToolDefinition) []anthropicToolDef {
	if len(defs) == 0 {
		return nil
	}
	out := make([]anthropicToolDef, 0, len(defs))
	for _, t := range defs {
		schema := t.Function.Parameters
		if schema == nil {
			schema = map[string]interface{}{}
		}
		out = append(out, anthropicToolDef{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: schema,
		})
	}
	return out
}

func toolArgumentsToAnthropicInput(args string) json.RawMessage {
	if args == "" {
		return json.RawMessage(`{}`)
	}
	var v json.RawMessage
	if json.Unmarshal([]byte(args), &v) == nil {
		return v
	}
	return json.RawMessage(`{}`)
}

func routerMessagesToAnthropic(msgs []router.Message) ([]anthropicMessage, string, error) {
	var systemPrompt string
	messages := make([]anthropicMessage, 0, len(msgs))

	for _, m := range msgs {
		if m.Role == "system" {
			systemPrompt = m.Content
			continue
		}

		if m.Role == "tool" {
			blocks := []anthropicToolResultBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}}
			raw, err := json.Marshal(blocks)
			if err != nil {
				return nil, "", fmt.Errorf("marshal tool_result blocks: %w", err)
			}
			messages = append(messages, anthropicMessage{Role: "user", Content: raw})
			continue
		}

		role := m.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}

		if role == "assistant" && len(m.ToolCalls) > 0 {
			var parts []interface{}
			if m.Content != "" {
				parts = append(parts, anthropicTextBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, anthropicToolUseBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: toolArgumentsToAnthropicInput(tc.Function.Arguments),
				})
			}
			raw, err := json.Marshal(parts)
			if err != nil {
				return nil, "", fmt.Errorf("marshal assistant tool blocks: %w", err)
			}
			messages = append(messages, anthropicMessage{Role: "assistant", Content: raw})
			continue
		}

		raw, err := json.Marshal(m.Content)
		if err != nil {
			return nil, "", fmt.Errorf("marshal message content: %w", err)
		}
		messages = append(messages, anthropicMessage{Role: role, Content: json.RawMessage(raw)})
	}

	return messages, systemPrompt, nil
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	temp := req.Temperature
	if temp == 0 {
		temp = 0.3
	}

	messages, systemPrompt, err := routerMessagesToAnthropic(req.Messages)
	if err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no user/assistant messages provided")
	}

	body := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      systemPrompt,
		Messages:    messages,
		Tools:       routerToolsToAnthropic(req.Tools),
		Temperature: temp,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var aResp anthropicResponse
	if err := json.Unmarshal(respBody, &aResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if aResp.Error != nil {
		return nil, fmt.Errorf("api error [%s]: %s", aResp.Error.Type, aResp.Error.Message)
	}

	var content string
	var toolCalls []router.ToolCall
	for _, block := range aResp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, router.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: router.FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	if content == "" && len(toolCalls) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	return &router.CompletionResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		Model:        aResp.Model,
		PromptTokens: aResp.Usage.InputTokens,
		CompTokens:   aResp.Usage.OutputTokens,
	}, nil
}
