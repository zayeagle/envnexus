package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/diagnosis"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/remediation"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
	"github.com/zayeagle/envnexus/libs/shared/pkg/agentprompt"
)

const (
	DefaultMaxIterations = 10
	DefaultMaxTokens     = 4096
	DefaultTemperature   = 0.3
)

type ApprovalRequest struct {
	ToolName    string                 `json:"tool_name"`
	Description string                 `json:"description"`
	RiskLevel   string                 `json:"risk_level"`
	Params      map[string]interface{} `json:"params"`
}

type ApprovalResponse struct {
	Approved bool `json:"approved"`
}

type EventType string

const (
	EventThinking         EventType = "thinking"
	EventToolStart        EventType = "tool_start"
	EventToolResult       EventType = "tool_result"
	EventApprovalRequired EventType = "approval_required"
	EventMessage          EventType = "message"
	EventError            EventType = "error"
)

type Event struct {
	Type    EventType   `json:"type"`
	Content interface{} `json:"content"`
}

type EventHandler func(event Event)

type ApprovalHandler func(req ApprovalRequest) ApprovalResponse

type Loop struct {
	llmRouter           *router.Router
	registry            *tools.Registry
	maxIterations       int
	onEvent             EventHandler
	onApproval          ApprovalHandler
	remediationPlanner  *remediation.Planner
}

type LoopOption func(*Loop)

func WithMaxIterations(n int) LoopOption {
	return func(l *Loop) { l.maxIterations = n }
}

func WithEventHandler(h EventHandler) LoopOption {
	return func(l *Loop) { l.onEvent = h }
}

func WithApprovalHandler(h ApprovalHandler) LoopOption {
	return func(l *Loop) { l.onApproval = h }
}

func WithRemediationPlanner(p *remediation.Planner) LoopOption {
	return func(l *Loop) { l.remediationPlanner = p }
}

func NewLoop(llmRouter *router.Router, registry *tools.Registry, opts ...LoopOption) *Loop {
	l := &Loop{
		llmRouter:     llmRouter,
		registry:      registry,
		maxIterations: DefaultMaxIterations,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *Loop) emit(evt Event) {
	if l.onEvent != nil {
		l.onEvent(evt)
	}
}

func (l *Loop) Run(ctx context.Context, messages []router.Message) (string, error) {
	toolDefs := l.buildToolDefinitions()
	systemPrompt := l.buildSystemPrompt()

	fullMessages := make([]router.Message, 0, len(messages)+1)
	fullMessages = append(fullMessages, router.Message{
		Role:    "system",
		Content: systemPrompt,
	})
	fullMessages = append(fullMessages, messages...)

	for i := 0; i < l.maxIterations; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		slog.Info("[agent] Loop iteration", "iteration", i+1, "messages", len(fullMessages))

		l.emit(Event{Type: EventThinking, Content: map[string]interface{}{
			"iteration": i + 1,
			"detail":    "Thinking...",
		}})

		resp, err := l.llmRouter.Complete(ctx, &router.CompletionRequest{
			Messages:    fullMessages,
			Tools:       toolDefs,
			MaxTokens:   DefaultMaxTokens,
			Temperature: DefaultTemperature,
		})
		if err != nil {
			return "", fmt.Errorf("llm completion failed at iteration %d: %w", i+1, err)
		}

		if len(resp.ToolCalls) == 0 {
			if plan := l.tryGenerateRemediationPlan(ctx, resp.Content); plan != nil {
				l.emit(Event{Type: EventMessage, Content: map[string]interface{}{
					"content": resp.Content,
				}})
				return resp.Content, nil
			}
			l.emit(Event{Type: EventMessage, Content: map[string]interface{}{
				"content": resp.Content,
			}})
			return resp.Content, nil
		}

		assistantMsg := router.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		fullMessages = append(fullMessages, assistantMsg)

		for _, tc := range resp.ToolCalls {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			toolResult := l.executeTool(ctx, tc)

			resultForLLM := truncateToolResultForLLM(toolResult)
			resultJSON, _ := json.Marshal(resultForLLM)
			fullMessages = append(fullMessages, router.Message{
				Role:       "tool",
				Content:    string(resultJSON),
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
	}

	l.emit(Event{Type: EventThinking, Content: map[string]interface{}{
		"detail":  fmt.Sprintf("Reached maximum of %d tool iterations. Generating summary...", l.maxIterations),
		"warning": "max_iterations_reached",
	}})

	fullMessages = append(fullMessages, router.Message{
		Role:    "user",
		Content: "You have reached the maximum number of tool call iterations. Stop calling tools. Summarize what you accomplished, what failed (include specific error messages), and what remains to be done.",
	})

	resp, err := l.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages:    fullMessages,
		MaxTokens:   DefaultMaxTokens,
		Temperature: DefaultTemperature,
	})
	if err != nil {
		return "", fmt.Errorf("final completion failed: %w", err)
	}

	l.emit(Event{Type: EventMessage, Content: map[string]interface{}{
		"content": resp.Content,
	}})
	return resp.Content, nil
}

func (l *Loop) executeTool(ctx context.Context, tc router.ToolCall) map[string]interface{} {
	toolName := tc.Function.Name

	l.emit(Event{Type: EventToolStart, Content: map[string]interface{}{
		"tool_name": toolName,
		"arguments": tc.Function.Arguments,
		"call_id":   tc.ID,
	}})

	tool, found := l.registry.Get(toolName)
	if !found {
		errMsg := fmt.Sprintf("tool '%s' not found", toolName)
		result := map[string]interface{}{
			"error": errMsg,
		}
		l.emit(Event{Type: EventToolResult, Content: map[string]interface{}{
			"tool_name": toolName,
			"call_id":   tc.ID,
			"status":    "failed",
			"error":     errMsg,
			"output":    result,
		}})
		return result
	}

	needsApproval := !tool.IsReadOnly()
	if checker, ok := tool.(tools.ApprovalChecker); ok {
		params := make(map[string]interface{})
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &params)
		}
		needsApproval = checker.NeedsApproval(params)
	}

	if needsApproval {
		approved := l.requestApproval(tool, tc)
		if !approved {
			result := map[string]interface{}{
				"status":  "rejected",
				"message": "User rejected this operation",
			}
			l.emit(Event{Type: EventToolResult, Content: map[string]interface{}{
				"tool_name": toolName,
				"call_id":   tc.ID,
				"status":    "rejected",
				"output":    result,
			}})
			return result
		}
	}

	params := make(map[string]interface{})
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
			slog.Warn("[agent] Failed to parse tool arguments", "tool", toolName, "error", err)
		}
	}

	start := time.Now()
	toolResult, err := tool.Execute(ctx, params)
	elapsed := time.Since(start)

	if err != nil {
		result := map[string]interface{}{
			"error":       err.Error(),
			"duration_ms": elapsed.Milliseconds(),
		}
		l.emit(Event{Type: EventToolResult, Content: map[string]interface{}{
			"tool_name":   toolName,
			"call_id":     tc.ID,
			"status":      "failed",
			"error":       err.Error(),
			"output":      result,
			"duration_ms": elapsed.Milliseconds(),
		}})
		return result
	}

	result := map[string]interface{}{
		"status":      toolResult.Status,
		"summary":     toolResult.Summary,
		"output":      toolResult.Output,
		"duration_ms": toolResult.DurationMs,
	}
	if toolResult.Error != "" {
		result["error"] = toolResult.Error
	}

	evtContent := map[string]interface{}{
		"tool_name":   toolName,
		"call_id":     tc.ID,
		"status":      toolResult.Status,
		"summary":     toolResult.Summary,
		"duration_ms": toolResult.DurationMs,
	}
	if toolResult.Error != "" {
		evtContent["error"] = toolResult.Error
	}
	if toolResult.Status == "failed" {
		evtContent["output"] = toolResult.Output
	}
	l.emit(Event{Type: EventToolResult, Content: evtContent})

	slog.Info("[agent] Tool executed", "tool", toolName, "status", toolResult.Status, "duration_ms", toolResult.DurationMs)
	return result
}

func (l *Loop) requestApproval(tool tools.Tool, tc router.ToolCall) bool {
	if l.onApproval == nil {
		slog.Warn("[agent] Write tool requested but no approval handler set, denying", "tool", tool.Name())
		return false
	}

	var params map[string]interface{}
	if tc.Function.Arguments != "" {
		json.Unmarshal([]byte(tc.Function.Arguments), &params)
	}

	req := ApprovalRequest{
		ToolName:    tool.Name(),
		Description: tool.Description(),
		RiskLevel:   tool.RiskLevel(),
		Params:      params,
	}

	resp := l.onApproval(req)
	return resp.Approved
}

func (l *Loop) buildToolDefinitions() []router.ToolDefinition {
	openaiTools := l.registry.ToOpenAITools()
	defs := make([]router.ToolDefinition, len(openaiTools))
	for i, t := range openaiTools {
		defs[i] = router.ToolDefinition{
			Type: t.Type,
			Function: router.FunctionDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		}
	}
	return defs
}

const maxToolOutputRunesForLLM = 4096

func truncateToolResultForLLM(result map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(result))
	for k, v := range result {
		out[k] = v
	}
	if output, ok := out["output"]; ok {
		switch o := output.(type) {
		case string:
			r := []rune(o)
			if len(r) > maxToolOutputRunesForLLM {
				out["output"] = string(r[:maxToolOutputRunesForLLM]) + "\n... (truncated for context window)"
			}
		case map[string]interface{}:
			if outStr, ok := o["output"].(string); ok {
				r := []rune(outStr)
				if len(r) > maxToolOutputRunesForLLM {
					o["output"] = string(r[:maxToolOutputRunesForLLM]) + "\n... (truncated for context window)"
				}
			}
		}
	}
	return out
}

func (l *Loop) buildSystemPrompt() string {
	return agentprompt.BuildAgentLoopSystemPrompt(collectEnvironmentSnapshot())
}

func collectEnvironmentSnapshot() agentprompt.Snapshot {
	hostname, _ := os.Hostname()
	s := agentprompt.Snapshot{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: hostname,
	}

	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("OS"); v != "" {
			s.OSVersion = v
		}
		if v := os.Getenv("PROCESSOR_ARCHITECTURE"); v != "" {
			s.Processor = v
		}
		if v := os.Getenv("USERPROFILE"); v != "" {
			s.UserProfile = v
		}
		if v := os.Getenv("SystemRoot"); v != "" {
			s.SystemRoot = v
		}
	default:
		if v := os.Getenv("SHELL"); v != "" {
			s.ExtraLines = append(s.ExtraLines, fmt.Sprintf("User default shell: %s", v))
		}
		if v := os.Getenv("HOME"); v != "" {
			s.UserProfile = v
		}
		if v := os.Getenv("LANG"); v != "" {
			s.Lang = v
		}
	}

	if v := os.Getenv("PATH"); v != "" {
		sep := ":"
		if runtime.GOOS == "windows" {
			sep = ";"
		}
		paths := strings.Split(v, sep)
		if len(paths) > 10 {
			paths = paths[:10]
		}
		s.PathHead = fmt.Sprintf("first %d entries: %s", len(paths), strings.Join(paths, sep))
	}

	if wd, err := os.Getwd(); err == nil {
		s.WorkDir = wd
	}

	return agentprompt.NormalizeSnapshot(s)
}

// tryGenerateRemediationPlan checks if the LLM response suggests a remediation action
// and generates a plan if the planner is configured. Returns nil if no plan is generated.
func (l *Loop) tryGenerateRemediationPlan(ctx context.Context, content string) *remediation.RemediationPlan {
	if l.remediationPlanner == nil {
		return nil
	}

	lower := strings.ToLower(content)
	markers := []string{
		"remediation plan",
		"fix plan",
		"repair plan",
		"修复计划",
		"修复方案",
		"建议修复",
		"recommended fix",
		"suggested remediation",
	}

	hasMarker := false
	for _, m := range markers {
		if strings.Contains(lower, m) {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return nil
	}

	diagResult := &diagnosis.DiagnosisResult{
		ProblemType: "general",
		Confidence:  0.7,
		Findings: []diagnosis.Finding{
			{Source: "chat_loop", Summary: content, Level: "info"},
		},
		RecommendedActions: []diagnosis.ActionDraft{},
	}

	plan, err := l.remediationPlanner.GeneratePlan(ctx, diagResult)
	if err != nil {
		slog.Warn("[agent] Failed to generate remediation plan from chat", "error", err)
		return nil
	}

	l.emit(Event{Type: "plan_generated", Content: map[string]interface{}{
		"plan_id":    plan.PlanID,
		"summary":    plan.Summary,
		"risk_level": plan.RiskLevel,
		"steps":      len(plan.Steps),
		"plan":       plan,
	}})

	slog.Info("[agent] Remediation plan generated from chat",
		"plan_id", plan.PlanID,
		"steps", len(plan.Steps),
	)

	return plan
}
