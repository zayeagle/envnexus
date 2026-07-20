package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
)

type DeepSeekProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewDeepSeekProvider(cfg router.ProviderConfig) *DeepSeekProvider {
	name := cfg.Name
	if name == "" {
		name = "deepseek"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	return &DeepSeekProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *DeepSeekProvider) Name() string { return p.name }

func (p *DeepSeekProvider) IsAvailable() bool {
	return p.apiKey != "" && p.baseURL != ""
}

func (p *DeepSeekProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "deepseek-chat"
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
	reasoning := msg.ReasoningContent
	if content == "" && reasoning != "" {
		slog.Debug("[deepseek] content empty but reasoning_content present, using reasoning_content",
			"reasoning_len", len(reasoning))
		content = reasoning
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
