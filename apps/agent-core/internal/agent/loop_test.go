package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

// mockProvider implements router.Provider for testing.
type mockProvider struct {
	responses []*router.CompletionResponse
	callIdx   int
}

func (m *mockProvider) Name() string        { return "mock" }
func (m *mockProvider) IsAvailable() bool   { return true }
func (m *mockProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	if m.callIdx >= len(m.responses) {
		return &router.CompletionResponse{Content: "done"}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

type simpleTool struct {
	name     string
	readOnly bool
}

func (t *simpleTool) Name() string                { return t.name }
func (t *simpleTool) Description() string          { return "test tool" }
func (t *simpleTool) IsReadOnly() bool             { return t.readOnly }
func (t *simpleTool) RiskLevel() string            { return "L0" }
func (t *simpleTool) Parameters() *tools.ParamSchema { return tools.NoParams() }
func (t *simpleTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	return &tools.ToolResult{ToolName: t.name, Status: "ok", Summary: "done", Output: "result"}, nil
}

// TestLoop_WithoutRemediationPlanner verifies that the existing chat behavior
// is unchanged when no remediation planner is configured.
func TestLoop_WithoutRemediationPlanner(t *testing.T) {
	provider := &mockProvider{
		responses: []*router.CompletionResponse{
			{Content: "Hello! How can I help you?"},
		},
	}

	llmRouter := router.NewRouter(0)
	llmRouter.RegisterProvider(provider)

	registry := tools.NewRegistry()
	registry.Register(&simpleTool{name: "read_system_info", readOnly: true})

	var events []EventType
	loop := NewLoop(llmRouter, registry,
		WithEventHandler(func(evt Event) {
			events = append(events, evt.Type)
		}),
	)

	result, err := loop.Run(context.Background(), []router.Message{
		{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result != "Hello! How can I help you?" {
		t.Errorf("unexpected result: %s", result)
	}

	hasThinking := false
	hasMessage := false
	for _, e := range events {
		if e == EventThinking {
			hasThinking = true
		}
		if e == EventMessage {
			hasMessage = true
		}
	}
	if !hasThinking {
		t.Error("expected thinking event")
	}
	if !hasMessage {
		t.Error("expected message event")
	}
}

// TestLoop_ToolExecution verifies tool calls work without remediation planner.
func TestLoop_ToolExecution(t *testing.T) {
	argsJSON, _ := json.Marshal(map[string]interface{}{})

	provider := &mockProvider{
		responses: []*router.CompletionResponse{
			{
				Content: "",
				ToolCalls: []router.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: router.FunctionCall{
							Name:      "read_system_info",
							Arguments: string(argsJSON),
						},
					},
				},
			},
			{Content: "System info collected. Everything looks good."},
		},
	}

	llmRouter := router.NewRouter(0)
	llmRouter.RegisterProvider(provider)

	registry := tools.NewRegistry()
	registry.Register(&simpleTool{name: "read_system_info", readOnly: true})

	var events []EventType
	loop := NewLoop(llmRouter, registry,
		WithEventHandler(func(evt Event) {
			events = append(events, evt.Type)
		}),
	)

	result, err := loop.Run(context.Background(), []router.Message{
		{Role: "user", Content: "check system"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}

	hasToolStart := false
	hasToolResult := false
	for _, e := range events {
		if e == EventToolStart {
			hasToolStart = true
		}
		if e == EventToolResult {
			hasToolResult = true
		}
	}
	if !hasToolStart {
		t.Error("expected tool_start event")
	}
	if !hasToolResult {
		t.Error("expected tool_result event")
	}
}

// TestLoop_ExistingSSEEventTypes verifies that existing event types are preserved.
func TestLoop_ExistingSSEEventTypes(t *testing.T) {
	expectedTypes := []EventType{
		EventThinking,
		EventToolStart,
		EventToolResult,
		EventApprovalRequired,
		EventMessage,
		EventError,
	}

	for _, et := range expectedTypes {
		if string(et) == "" {
			t.Errorf("event type constant is empty")
		}
	}

	if EventThinking != "thinking" {
		t.Errorf("EventThinking changed: %s", EventThinking)
	}
	if EventToolStart != "tool_start" {
		t.Errorf("EventToolStart changed: %s", EventToolStart)
	}
	if EventToolResult != "tool_result" {
		t.Errorf("EventToolResult changed: %s", EventToolResult)
	}
	if EventApprovalRequired != "approval_required" {
		t.Errorf("EventApprovalRequired changed: %s", EventApprovalRequired)
	}
	if EventMessage != "message" {
		t.Errorf("EventMessage changed: %s", EventMessage)
	}
	if EventError != "error" {
		t.Errorf("EventError changed: %s", EventError)
	}
}
