package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/libs/shared/pkg/agentprompt"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

// nlGenHTTPTimeout is the client timeout for upstream LLM HTTP calls.
// Slow models or congested APIs often exceed ~1m; cap under 5 minutes per product expectation.
const nlGenHTTPTimeout = 4*time.Minute + 45*time.Second

// nlGenMaxTokens leaves room for JSON metadata plus a multi-line shell/PowerShell command.
const nlGenMaxTokens = 1024

type NLGenerator struct {
	modelRepo  repository.ModelProfileRepository
	httpClient *http.Client
}

func NewNLGenerator(modelRepo repository.ModelProfileRepository) *NLGenerator {
	return &NLGenerator{
		modelRepo:  modelRepo,
		httpClient: &http.Client{Timeout: nlGenHTTPTimeout},
	}
}

type GenerateCommandResult struct {
	Command   string `json:"command"`
	RiskLevel string `json:"risk_level,omitempty"`
	Title     string `json:"title,omitempty"`
}

func (g *NLGenerator) Generate(ctx context.Context, tenantID, prompt string, target agentprompt.Snapshot) (*GenerateCommandResult, error) {
	start := time.Now()
	model, err := g.pickModel(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("no available model: %w", err)
	}
	slog.Info("[nl-gen] picked model", "model", model.ModelName, "elapsed_ms", time.Since(start).Milliseconds())

	llmStart := time.Now()
	system := agentprompt.BuildNLCommandSystemPrompt(target)
	respText, err := g.callLLM(ctx, model, prompt, system)
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			return nil, err
		}
		return nil, fmt.Errorf("LLM call failed (took %dms): %w", time.Since(llmStart).Milliseconds(), err)
	}
	slog.Info("[nl-gen] LLM responded", "elapsed_ms", time.Since(llmStart).Milliseconds(), "total_ms", time.Since(start).Milliseconds())

	var result GenerateCommandResult
	cleaned := normalizeLLMTextForJSON(respText)
	if cleaned == "" {
		cleaned = strings.TrimSpace(respText)
	}

	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		if alt := firstJSONObject(respText); alt != "" && alt != cleaned {
			cleaned = normalizeLLMTextForJSON(alt)
			err = json.Unmarshal([]byte(cleaned), &result)
		}
		if err != nil {
			if cmdObj := jsonObjectContainingCommandKey(respText); cmdObj != "" {
				cleaned = normalizeLLMTextForJSON(cmdObj)
				err = json.Unmarshal([]byte(cleaned), &result)
			}
		}
		if err != nil {
			if responseLooksLikeExplanationNotJSON(respText) {
				slog.Warn("[nl-gen] model returned explanation instead of JSON", "raw_len", len(respText))
				return nil, domain.NewAppError("llm_format_error",
					"模型返回了说明文字而非 JSON 命令，请改用非推理模型、调高遵循指令，或重试生成",
					http.StatusUnprocessableEntity)
			}
			slog.Warn("[nl-gen] Failed to parse LLM JSON, using raw text as command", "raw", respText)
			result.Command = strings.TrimSpace(respText)
			result.RiskLevel = EvaluateRisk("shell", result.Command)
		}
	}
	// Some OpenAI-compatible providers may return a different JSON shape (e.g. {"cmd": "..."}).
	// If we parsed JSON but the command is still empty, try a generic map-based extraction.
	if strings.TrimSpace(result.Command) == "" && strings.HasPrefix(cleaned, "{") {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(cleaned), &m); err == nil {
			for _, k := range []string{"command", "cmd", "shell", "bash", "powershell", "executable", "script", "line", "powershell_command"} {
				if v, ok := m[k]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						result.Command = strings.TrimSpace(s)
						break
					}
				}
			}
		}
	}
	// Model sometimes returns valid JSON but puts chain-of-thought into "command" instead of a shell line.
	if strings.TrimSpace(result.Command) != "" && responseLooksLikeExplanationNotJSON(result.Command) {
		slog.Warn("[nl-gen] command field looks like prose not a shell line", "len", len(result.Command))
		if altObj := jsonObjectContainingCommandKey(respText); altObj != "" {
			var alt GenerateCommandResult
			cleanAlt := normalizeLLMTextForJSON(altObj)
			if json.Unmarshal([]byte(cleanAlt), &alt) == nil && strings.TrimSpace(alt.Command) != "" &&
				!responseLooksLikeExplanationNotJSON(alt.Command) {
				result = alt
			}
		}
		if strings.TrimSpace(result.Command) != "" && responseLooksLikeExplanationNotJSON(result.Command) {
			return nil, domain.NewAppError("llm_format_error",
				"模型把说明文字写进了 command 字段；请改用 deepseek-chat 等非推理模型并打开 JSON 输出，或重试生成",
				http.StatusUnprocessableEntity)
		}
	}
	// If the provider returned an empty content despite a 200 OK, surface a clear error.
	if strings.TrimSpace(result.Command) == "" {
		slog.Warn("[nl-gen] Empty command from LLM", "raw", respText)
		msg := "模型未返回可解析的命令内容"
		if preview := truncateForDisplay(respText, 800); preview != "" {
			msg += ": " + preview
		}
		return nil, domain.NewAppError("llm_empty_command", msg, http.StatusUnprocessableEntity)
	}

	if result.RiskLevel == "" {
		result.RiskLevel = EvaluateRisk("shell", result.Command)
	}

	slog.Info("[nl-gen] result", "command_len", len(result.Command), "risk", result.RiskLevel, "title", result.Title, "total_ms", time.Since(start).Milliseconds())
	return &result, nil
}

func (g *NLGenerator) pickModel(ctx context.Context, tenantID string) (*domain.ModelProfile, error) {
	profiles, err := g.modelRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if p.Status == "active" && p.BaseURL != "" && p.APIKey != "" {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no active model profile with API key configured for tenant %s", tenantID)
}

// normalizeLLMTextForJSON strips common markdown fences and whitespace around a JSON payload.
func normalizeLLMTextForJSON(s string) string {
	cleaned := strings.TrimSpace(s)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	return strings.TrimSpace(cleaned)
}

// jsonObjectContainingCommandKey finds a JSON object in s that includes a "command" field (handles prose then JSON from chatty models).
func jsonObjectContainingCommandKey(s string) string {
	const needle = `"command"`
	idx := strings.LastIndex(s, needle)
	if idx < 0 {
		return ""
	}
	for j := idx; j >= 0; j-- {
		if s[j] != '{' {
			continue
		}
		obj := firstJSONObject(s[j:])
		if obj != "" && strings.Contains(obj, needle) {
			return obj
		}
	}
	return ""
}

// responseLooksLikeExplanationNotJSON detects chain-of-thought / help text mistakenly used as the only assistant content.
func responseLooksLikeExplanationNotJSON(s string) bool {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "{") {
		return false
	}
	if len(t) < 80 {
		return false
	}
	lower := strings.ToLower(t)
	markers := []string{
		"first,", "the user request", "user's request", "i need to", "translates to:",
		"as an operations assistant", "required json", "let me ", "i'll ", "i will ",
		"hard rules", "operations assistant",
		"用户请求", "用户的要求", "我需要", "首先",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// firstJSONObject returns the substring of the first top-level JSON object in s, if any.
func firstJSONObject(s string) string {
	i := strings.Index(s, "{")
	if i < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for j := i; j < len(s); j++ {
		ch := s[j]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i : j+1]
			}
		}
	}
	return ""
}

func truncateForDisplay(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// extractOpenAICompatError reads provider error shapes such as {"error":{"message":"...","type":"..."}}.
func extractOpenAICompatError(raw []byte) (string, bool) {
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", false
	}
	if envelope.Error != nil && strings.TrimSpace(envelope.Error.Message) != "" {
		msg := strings.TrimSpace(envelope.Error.Message)
		if t := strings.TrimSpace(envelope.Error.Type); t != "" {
			msg = t + ": " + msg
		}
		return msg, true
	}
	if strings.TrimSpace(envelope.Message) != "" {
		return strings.TrimSpace(envelope.Message), true
	}
	return "", false
}

func decodeAssistantContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Array of heterogeneous parts (OpenAI / Anthropic / DeepSeek / gateways).
	var rawParts []json.RawMessage
	if err := json.Unmarshal(raw, &rawParts); err == nil && len(rawParts) > 0 {
		var b strings.Builder
		for _, p := range rawParts {
			// Avoid infinite recursion on empty
			if len(p) > 0 && string(p) != "null" {
				b.WriteString(decodeAssistantContent(p))
			}
		}
		return b.String()
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		typ, _ := p["type"].(string)
		// text (OpenAI, many gateways), output_text (responses API), reasoning (optional strip handled by caller priority)
		switch typ {
		case "text", "output_text", "input_text", "":
			if txt, ok := p["text"].(string); ok {
				b.WriteString(txt)
			} else if c, ok := p["content"].(string); ok {
				b.WriteString(c)
			}
		case "reasoning":
			// NL command generation must never merge chain-of-thought into the payload.
		default:
			// Unknown block: still try common text fields (provider-specific types).
			if txt, ok := p["text"].(string); ok {
				b.WriteString(txt)
			} else if c, ok := p["content"].(string); ok {
				b.WriteString(c)
			}
		}
	}
	return b.String()
}

// stringifyContentField turns provider "content" / "reasoning_content" values into plain text.
func stringifyContentField(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []interface{}:
		var b strings.Builder
		for _, item := range x {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			part, _ := json.Marshal(m)
			b.WriteString(decodeAssistantContent(part))
		}
		return b.String()
	case map[string]interface{}:
		// Rare: single object instead of array
		part, _ := json.Marshal(x)
		return decodeAssistantContent(part)
	default:
		return ""
	}
}

func textFromChoiceMap(choice map[string]interface{}) string {
	if choice == nil {
		return ""
	}
	if msg, ok := choice["message"].(map[string]interface{}); ok {
		if s := stringifyContentField(msg["content"]); strings.TrimSpace(s) != "" {
			return s
		}
	}
	if d, ok := choice["delta"].(map[string]interface{}); ok {
		if s := stringifyContentField(d["content"]); strings.TrimSpace(s) != "" {
			return s
		}
	}
	if t, ok := choice["text"].(string); ok {
		return t
	}
	return ""
}

// extractAssistantTextFromRawResponse walks unknown OpenAI-compatible JSON (DeepSeek, OpenRouter, proxies) for assistant text.
func extractAssistantTextFromRawResponse(raw []byte) string {
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	choices, ok := root["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	for _, ch := range choices {
		cm, ok := ch.(map[string]interface{})
		if !ok {
			continue
		}
		if s := textFromChoiceMap(cm); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (g *NLGenerator) postChatCompletions(ctx context.Context, baseURL, apiKey string, body map[string]interface{}) ([]byte, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	return raw, resp.StatusCode, nil
}

func (g *NLGenerator) callLLM(ctx context.Context, model *domain.ModelProfile, userMessage, systemPrompt string) (string, error) {
	baseURL := strings.TrimSuffix(model.BaseURL, "/")

	userContent := strings.TrimSpace(userMessage)
	if userContent != "" {
		userContent += "\n\nOutput ONLY one JSON object (keys: command, risk_level, title) as defined in the system message."
	}
	body := map[string]interface{}{
		"model": model.ModelName,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userContent},
		},
		"temperature":     0,
		"max_tokens":      nlGenMaxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}

	raw, status, err := g.postChatCompletions(ctx, baseURL, model.APIKey, body)
	if err != nil {
		return "", err
	}
	if status == http.StatusBadRequest {
		if _, has := body["response_format"]; has {
			delete(body, "response_format")
			slog.Info("[nl-gen] retrying chat without response_format (provider may not support json_object)")
			raw, status, err = g.postChatCompletions(ctx, baseURL, model.APIKey, body)
			if err != nil {
				return "", err
			}
		}
	}

	if status >= 400 {
		if msg, ok := extractOpenAICompatError(raw); ok {
			return "", domain.NewAppError("llm_provider_error",
				fmt.Sprintf("%s (HTTP %d)", msg, status), http.StatusBadGateway)
		}
		return "", domain.NewAppError("llm_provider_error",
			fmt.Sprintf("LLM API HTTP %d: %s", status, truncateForDisplay(string(raw), 1200)),
			http.StatusBadGateway)
	}

	var chatResp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			// OpenAI Chat Completions (+ DeepSeek reasoning fields on message)
			Message *struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
			} `json:"message,omitempty"`
			// Some providers may respond with legacy "text" on each choice.
			Text *string `json:"text,omitempty"`
			// Streaming-like delta shape (occasionally returned even without stream=true).
			Delta *struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
			} `json:"delta,omitempty"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		if fallback := strings.TrimSpace(extractAssistantTextFromRawResponse(raw)); fallback != "" {
			slog.Info("[nl-gen] used generic JSON fallback for chat response")
			return fallback, nil
		}
		return "", fmt.Errorf("parse LLM response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		if msg, ok := extractOpenAICompatError(raw); ok {
			return "", domain.NewAppError("llm_provider_error", msg, http.StatusBadGateway)
		}
		if fallback := strings.TrimSpace(extractAssistantTextFromRawResponse(raw)); fallback != "" {
			return fallback, nil
		}
		return "", domain.NewAppError("llm_provider_error",
			"模型未返回任何候选回复 (choices 为空)", http.StatusBadGateway)
	}

	for i := range chatResp.Choices {
		c0 := chatResp.Choices[i]
		if c0.Message != nil && len(c0.Message.Content) > 0 {
			if t := decodeAssistantContent(c0.Message.Content); strings.TrimSpace(t) != "" {
				return t, nil
			}
		}
		if c0.Delta != nil && len(c0.Delta.Content) > 0 {
			if t := decodeAssistantContent(c0.Delta.Content); strings.TrimSpace(t) != "" {
				return t, nil
			}
		}
		if c0.Text != nil && strings.TrimSpace(*c0.Text) != "" {
			return *c0.Text, nil
		}
	}

	// Structured decode missed text (unusual types); try map walk last.
	if fallback := strings.TrimSpace(extractAssistantTextFromRawResponse(raw)); fallback != "" {
		slog.Info("[nl-gen] used generic JSON fallback after empty structured fields", "choice_count", len(chatResp.Choices))
		return fallback, nil
	}

	c0 := chatResp.Choices[0]
	if msg, ok := extractOpenAICompatError(raw); ok {
		slog.Warn("[nl-gen] provider error in body without assistant text", "raw_response_len", len(raw))
		return "", domain.NewAppError("llm_provider_error", msg, http.StatusBadGateway)
	}
	slog.Warn("[nl-gen] LLM choice had no usable text", "raw_response_len", len(raw))
	fr := strings.TrimSpace(c0.FinishReason)
	if fr != "" {
		return "", domain.NewAppError("llm_provider_error",
			"模型未返回可解析文本 (finish_reason="+fr+")", http.StatusBadGateway)
	}
	return "", domain.NewAppError("llm_provider_error", "模型未返回可解析文本", http.StatusBadGateway)
}
