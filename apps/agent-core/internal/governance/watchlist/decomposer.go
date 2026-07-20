package watchlist

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type Decomposer struct {
	llmRouter *router.Router
	registry  *tools.Registry
}

func NewDecomposer(llmRouter *router.Router, registry *tools.Registry) *Decomposer {
	return &Decomposer{
		llmRouter: llmRouter,
		registry:  registry,
	}
}

type DecomposeResult struct {
	Items      []WatchItem `json:"items"`
	Suggested  []WatchItem `json:"suggested,omitempty"`
	RawLLMJSON string      `json:"-"`
}

type llmWatchItem struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	ToolName    string                 `json:"tool_name"`
	ToolParams  map[string]interface{} `json:"tool_params,omitempty"`
	Condition   WatchCondition         `json:"condition"`
	IntervalSec int                    `json:"interval_sec"`
}

type llmDecomposeResponse struct {
	Items     []llmWatchItem `json:"items"`
	Suggested []llmWatchItem `json:"suggested,omitempty"`
}

func (d *Decomposer) Decompose(ctx context.Context, naturalLanguage string) (*DecomposeResult, error) {
	if d.llmRouter == nil {
		return nil, fmt.Errorf("no LLM router configured")
	}

	availableTools := d.buildToolList()

	prompt := fmt.Sprintf(`The user wants to set up monitoring watch items based on this request:
"%s"

Available tools (you MUST only use tools from this list):
%s

Output a JSON object with two arrays:
1. "items": watch items that directly match the user's request
2. "suggested": additional watch items the AI recommends (related but not explicitly requested)

Each item must have:
- "name": short descriptive name
- "description": what this monitors
- "tool_name": must be one of the available tools listed above
- "tool_params": parameters to pass to the tool (object, can be empty)
- "condition": {"type": "threshold|exists|reachable|contains|custom", "json_path": "path.to.value", "operator": "lt|gt|eq|ne|contains|not_contains", "threshold": <value>, "pattern": "<string for contains>"}
- "interval_sec": check interval in seconds (minimum 60)

Condition types:
- "threshold": numeric comparison (use json_path + operator + threshold)
- "exists": check if a value exists at json_path (operator "eq" with threshold true/false)
- "reachable": check if target is reachable (uses tool output status field)
- "contains": string pattern match (use json_path + operator + pattern)
- "custom": flexible comparison (use json_path + operator + threshold)

Output ONLY valid JSON. No explanation, no markdown fences.`, naturalLanguage, availableTools)

	resp, err := d.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a monitoring configuration assistant. You decompose natural language monitoring requests into structured watch items. Output ONLY valid JSON."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   2048,
		Temperature: 0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM decompose failed: %w", err)
	}

	rawJSON := extractJSON(resp.Content)

	var llmResp llmDecomposeResponse
	if err := json.Unmarshal([]byte(rawJSON), &llmResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w (raw: %s)", err, truncate(rawJSON, 200))
	}

	result := &DecomposeResult{RawLLMJSON: rawJSON}

	for _, li := range llmResp.Items {
		item, err := d.validateAndConvert(li, SourceUser)
		if err != nil {
			slog.Warn("[Decomposer] skipping invalid item", "name", li.Name, "error", err)
			continue
		}
		result.Items = append(result.Items, *item)
	}

	for _, li := range llmResp.Suggested {
		item, err := d.validateAndConvert(li, SourceLLMSuggested)
		if err != nil {
			slog.Warn("[Decomposer] skipping invalid suggested item", "name", li.Name, "error", err)
			continue
		}
		result.Suggested = append(result.Suggested, *item)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("LLM produced no valid watch items for request: %s", truncate(naturalLanguage, 100))
	}

	return result, nil
}

func (d *Decomposer) validateAndConvert(li llmWatchItem, source WatchItemSource) (*WatchItem, error) {
	if li.ToolName == "" {
		return nil, fmt.Errorf("missing tool_name")
	}
	if _, ok := d.registry.Get(li.ToolName); !ok {
		return nil, fmt.Errorf("tool %q not found in registry", li.ToolName)
	}
	if li.Name == "" {
		return nil, fmt.Errorf("missing name")
	}

	interval := time.Duration(li.IntervalSec) * time.Second
	if interval < 60*time.Second {
		interval = 5 * time.Minute
	}

	return &WatchItem{
		ID:          generateID(),
		Name:        li.Name,
		Description: li.Description,
		Source:      source,
		ToolName:    li.ToolName,
		ToolParams:  li.ToolParams,
		Condition:   li.Condition,
		Interval:    interval,
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (d *Decomposer) buildToolList() string {
	var sb strings.Builder
	for _, t := range d.registry.List() {
		fmt.Fprintf(&sb, "- %s (read_only=%v, risk=%s): %s\n",
			t.Name(), t.IsReadOnly(), t.RiskLevel(), t.Description())
	}
	return sb.String()
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func generateID() string {
	return fmt.Sprintf("wi_%d", time.Now().UnixNano())
}
