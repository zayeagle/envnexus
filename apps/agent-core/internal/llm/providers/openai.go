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

type OpenAIProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAIProvider(cfg router.ProviderConfig) *OpenAIProvider {
	name := cfg.Name
	if name == "" {
		name = "openai"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &OpenAIProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *OpenAIProvider) Name() string { return p.name }

func (p *OpenAIProvider) IsAvailable() bool {
	return p.apiKey != "" && p.baseURL != ""
}

type openaiRequest struct {
	Model       string             `json:"model"`
	Messages    []openaiMessage    `json:"messages"`
	Tools       []openaiToolDef    `json:"tools,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type openaiMessage struct {
	Role       string              `json:"role"`
	Content    *string             `json:"content"`
	ToolCalls  []openaiToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Name       string              `json:"name,omitempty"`
}

type openaiToolDef struct {
	Type     string              `json:"type"`
	Function openaiToolFunction  `json:"function"`
}

type openaiToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

type openaiToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function openaiToolCallFunc  `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content          string           `json:"content"`
			ReasoningContent string           `json:"reasoning_content"`
			ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func convertMessagesToOpenAI(msgs []router.Message) []openaiMessage {
	result := make([]openaiMessage, 0, len(msgs))
	for _, m := range msgs {
		om := openaiMessage{
			Role:       m.Role,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		if m.Content != "" || (m.Role != "assistant" || len(m.ToolCalls) == 0) {
			s := m.Content
			om.Content = &s
		}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, openaiToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: openaiToolCallFunc{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		result = append(result, om)
	}
	return result
}

func convertToolsToOpenAI(tools []router.ToolDefinition) []openaiToolDef {
	if len(tools) == 0 {
		return nil
	}
	result := make([]openaiToolDef, len(tools))
	for i, t := range tools {
		result[i] = openaiToolDef{
			Type: t.Type,
			Function: openaiToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		}
	}
	return result
}

func convertToolCallsFromOpenAI(calls []openaiToolCall) []router.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]router.ToolCall, len(calls))
	for i, c := range calls {
		result[i] = router.ToolCall{
			ID:   c.ID,
			Type: c.Type,
			Function: router.FunctionCall{
				Name:      c.Function.Name,
				Arguments: c.Function.Arguments,
			},
		}
	}
	return result
}

func (p *OpenAIProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	temp := req.Temperature
	if temp == 0 {
		temp = 0.3
	}

	body := openaiRequest{
		Model:       model,
		Messages:    convertMessagesToOpenAI(req.Messages),
		Tools:       convertToolsToOpenAI(req.Tools),
		MaxTokens:   maxTokens,
		Temperature: temp,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	var oResp openaiResponse
	if err := json.Unmarshal(respBody, &oResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if oResp.Error != nil {
		return nil, fmt.Errorf("api error: %s", oResp.Error.Message)
	}

	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	msg := oResp.Choices[0].Message
	toolCalls := convertToolCallsFromOpenAI(msg.ToolCalls)

	content := msg.Content
	if content == "" && msg.ReasoningContent != "" {
		content = msg.ReasoningContent
	}
	if content == "" && len(toolCalls) == 0 {
		return nil, fmt.Errorf("empty content in response (raw: %s)", string(respBody[:min(len(respBody), 500)]))
	}

	return &router.CompletionResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		Model:        oResp.Model,
		PromptTokens: oResp.Usage.PromptTokens,
		CompTokens:   oResp.Usage.CompletionTokens,
	}, nil
}
