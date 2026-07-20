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

type OpenAIVisionProvider struct {
	*OpenAIProvider
}

func NewOpenAIVisionProvider(cfg router.ProviderConfig) *OpenAIVisionProvider {
	return &OpenAIVisionProvider{OpenAIProvider: NewOpenAIProvider(cfg)}
}

func (p *OpenAIVisionProvider) SupportsVision() bool { return true }

func (p *OpenAIVisionProvider) CompleteMultimodal(ctx context.Context, req *router.MultimodalRequest) (*router.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	type mmContent struct {
		Type     string                 `json:"type"`
		Text     string                 `json:"text,omitempty"`
		ImageURL *router.ImageURLPart   `json:"image_url,omitempty"`
	}

	type mmMessage struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"`
	}

	messages := make([]mmMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if len(m.ContentParts) == 0 {
			messages = append(messages, mmMessage{Role: m.Role, Content: nil})
			continue
		}
		if len(m.ContentParts) == 1 && m.ContentParts[0].Type == "text" {
			messages = append(messages, mmMessage{Role: m.Role, Content: m.ContentParts[0].Text})
			continue
		}
		parts := make([]mmContent, 0, len(m.ContentParts))
		for _, cp := range m.ContentParts {
			parts = append(parts, mmContent{
				Type:     cp.Type,
				Text:     cp.Text,
				ImageURL: cp.ImageURL,
			})
		}
		messages = append(messages, mmMessage{Role: m.Role, Content: parts})
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vision request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vision API status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
	}

	return &router.CompletionResponse{
		Content:      content,
		Model:        model,
		PromptTokens: result.Usage.PromptTokens,
		CompTokens:   result.Usage.CompletionTokens,
	}, nil
}
