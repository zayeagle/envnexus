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

type OllamaProvider struct {
	name    string
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaProvider(cfg router.ProviderConfig) *OllamaProvider {
	name := cfg.Name
	if name == "" {
		name = "ollama"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &OllamaProvider{
		name:    name,
		baseURL: baseURL,
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *OllamaProvider) Name() string { return p.name }

func (p *OllamaProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []openaiToolDef `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaResponse struct {
	Message struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	Model              string `json:"model"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	EvalCount          int    `json:"eval_count"`
	TotalDuration      int64  `json:"total_duration"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalDuration       int64  `json:"eval_duration"`
	Error              string `json:"error,omitempty"`
}

func convertMessagesToOllama(msgs []router.Message) []ollamaMessage {
	result := make([]ollamaMessage, 0, len(msgs))
	for _, m := range msgs {
		om := ollamaMessage{
			Role:       m.Role,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		if m.Content != "" || m.Role != "assistant" || len(m.ToolCalls) == 0 {
			om.Content = m.Content
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

func (p *OllamaProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "llama3.2"
	}

	body := ollamaRequest{
		Model:    model,
		Messages: convertMessagesToOllama(req.Messages),
		Tools:    convertToolsToOpenAI(req.Tools),
		Stream:   false,
	}
	if req.Temperature > 0 || req.MaxTokens > 0 {
		body.Options = &ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

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

	var oResp ollamaResponse
	if err := json.Unmarshal(respBody, &oResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if oResp.Error != "" {
		return nil, fmt.Errorf("ollama error: %s", oResp.Error)
	}

	toolCalls := convertToolCallsFromOpenAI(oResp.Message.ToolCalls)

	return &router.CompletionResponse{
		Content:      oResp.Message.Content,
		ToolCalls:    toolCalls,
		Model:        oResp.Model,
		PromptTokens: oResp.PromptEvalCount,
		CompTokens:   oResp.EvalCount,
	}, nil
}
