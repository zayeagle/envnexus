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

type GeminiProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewGeminiProvider(cfg router.ProviderConfig) *GeminiProvider {
	name := cfg.Name
	if name == "" {
		name = "gemini"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	return &GeminiProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *GeminiProvider) Name() string { return p.name }

func (p *GeminiProvider) IsAvailable() bool {
	return p.apiKey != "" && p.baseURL != ""
}

type geminiToolDeclaration struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent           `json:"contents"`
	SystemInstruction *geminiContent            `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig   `json:"generationConfig,omitempty"`
	Tools             []geminiToolDeclaration   `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                 `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall    `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string `json:"name"`
	Response struct {
		Content interface{} `json:"content"`
	} `json:"response"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiResponsePart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
	Error        *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

type geminiResponsePart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
}

func convertToolsToGemini(tools []router.ToolDefinition) []geminiToolDeclaration {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]geminiFunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, geminiFunctionDeclaration{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}
	return []geminiToolDeclaration{{FunctionDeclarations: decls}}
}

func geminiArgsFromRouter(arguments string) json.RawMessage {
	if arguments == "" {
		return json.RawMessage("{}")
	}
	return json.RawMessage(arguments)
}

func convertMessagesToGemini(msgs []router.Message) ([]geminiContent, error) {
	var contents []geminiContent
	for _, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "assistant":
			var parts []geminiPart
			if m.Content != "" {
				parts = append(parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				fn := tc.Function
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: fn.Name,
						Args: geminiArgsFromRouter(fn.Arguments),
					},
				})
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, geminiContent{Role: "model", Parts: parts})
		case "tool":
			if m.Name == "" {
				return nil, fmt.Errorf("tool message missing function name")
			}
			var fr geminiFunctionResponse
			fr.Name = m.Name
			fr.Response.Content = m.Content
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{FunctionResponse: &fr}},
			})
		default:
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: m.Content}},
			})
		}
	}
	return contents, nil
}

func (p *GeminiProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "gemini-2.0-flash"
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	temp := req.Temperature
	if temp == 0 {
		temp = 0.3
	}

	var systemInstruction *geminiContent
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
		}
	}

	contents, err := convertMessagesToGemini(req.Messages)
	if err != nil {
		return nil, err
	}

	if len(contents) == 0 {
		return nil, fmt.Errorf("no user messages provided")
	}

	body := geminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig: &geminiGenerationConfig{
			Temperature:     temp,
			MaxOutputTokens: maxTokens,
		},
		Tools: convertToolsToGemini(req.Tools),
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
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

	var gResp geminiResponse
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if gResp.Error != nil {
		return nil, fmt.Errorf("api error [%s]: %s", gResp.Error.Status, gResp.Error.Message)
	}

	if len(gResp.Candidates) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	parts := gResp.Candidates[0].Content.Parts
	if len(parts) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	var content string
	var toolCalls []router.ToolCall
	callIdx := 0
	for _, part := range parts {
		if part.Text != "" {
			content += part.Text
		}
		if part.FunctionCall != nil {
			args := string(part.FunctionCall.Args)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, router.ToolCall{
				ID:   fmt.Sprintf("gemini-call-%d", callIdx),
				Type: "function",
				Function: router.FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: args,
				},
			})
			callIdx++
		}
	}

	if content == "" && len(toolCalls) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	return &router.CompletionResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		Model:        model,
		PromptTokens: gResp.UsageMetadata.PromptTokenCount,
		CompTokens:   gResp.UsageMetadata.CandidatesTokenCount,
	}, nil
}
