package diagnosis

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

// ── Mock infrastructure ──────────────────────────────────────────────

type diagMockTool struct {
	name     string
	readOnly bool
	risk     string
	output   interface{}
}

func (m *diagMockTool) Name() string                   { return m.name }
func (m *diagMockTool) Description() string            { return "mock " + m.name }
func (m *diagMockTool) IsReadOnly() bool               { return m.readOnly }
func (m *diagMockTool) RiskLevel() string              { return m.risk }
func (m *diagMockTool) Parameters() *tools.ParamSchema { return tools.NoParams() }
func (m *diagMockTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	return &tools.ToolResult{ToolName: m.name, Status: "ok", Summary: "done", Output: m.output}, nil
}

type diagMockProvider struct {
	responses []*router.CompletionResponse
	callIdx   int
}

func (m *diagMockProvider) Name() string      { return "mock" }
func (m *diagMockProvider) IsAvailable() bool { return true }
func (m *diagMockProvider) Complete(ctx context.Context, req *router.CompletionRequest) (*router.CompletionResponse, error) {
	if m.callIdx >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func newDiagRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(&diagMockTool{name: "read_system_info", readOnly: true, risk: "L0", output: map[string]interface{}{"os": "linux", "hostname": "test"}})
	r.Register(&diagMockTool{name: "read_network_config", readOnly: true, risk: "L0", output: []interface{}{}})
	r.Register(&diagMockTool{name: "read_proxy_config", readOnly: true, risk: "L0", output: map[string]interface{}{"has_proxy": false}})
	r.Register(&diagMockTool{name: "read_env_vars", readOnly: true, risk: "L0", output: map[string]interface{}{}})
	r.Register(&diagMockTool{name: "ping_host", readOnly: true, risk: "L0", output: map[string]interface{}{"reachable": true, "latency_ms": 10.0}})
	r.Register(&diagMockTool{name: "dns_lookup", readOnly: true, risk: "L0", output: map[string]interface{}{"resolved": true}})
	r.Register(&diagMockTool{name: "dns_flush_cache", readOnly: false, risk: "L2", output: "flushed"})
	r.Register(&diagMockTool{name: "service_restart", readOnly: false, risk: "L2", output: "restarted"})
	return r
}

// ── Regression: Simple complexity uses fast path ─────────────────────

func TestDiagnosis_SimpleFastPath(t *testing.T) {
	registry := newDiagRegistry()

	intentResp, _ := json.Marshal(map[string]interface{}{
		"problem_type": "network",
		"scope":        "local",
		"risk_bias":    "conservative",
	})
	complexityResp, _ := json.Marshal(map[string]interface{}{
		"complexity": "simple",
		"reason":     "basic network check",
	})
	reasoningResp, _ := json.Marshal(DiagnosisResult{
		ProblemType: "network",
		Confidence:  0.85,
		Findings:    []Finding{{Source: "test", Summary: "all good", Level: "info"}},
		NextStep:    "done",
	})

	provider := &diagMockProvider{
		responses: []*router.CompletionResponse{
			{Content: string(intentResp)},
			{Content: string(complexityResp)},
			{Content: string(reasoningResp)},
		},
	}

	llmRouter := router.NewRouter(0)
	llmRouter.RegisterProvider(provider)

	engine := NewEngine(registry, llmRouter)

	var steps []string
	result, err := engine.RunDiagnosisWithProgress(context.Background(), "test-session", "check network", func(step, detail string) {
		steps = append(steps, step)
	})
	if err != nil {
		t.Fatalf("RunDiagnosis failed: %v", err)
	}

	if result.ProblemType != "network" {
		t.Errorf("expected problem_type network, got %s", result.ProblemType)
	}

	hasLayered := false
	hasIterative := false
	for _, s := range steps {
		if s == "evidence_collect_layered" {
			hasLayered = true
		}
		if s == "reasoning_iterative" {
			hasIterative = true
		}
	}
	if hasLayered {
		t.Error("simple complexity should NOT use layered evidence collection")
	}
	if hasIterative {
		t.Error("simple complexity should NOT use iterative reasoning")
	}
}

// ── Regression: Moderate+ uses layered evidence + iterative reasoning ─

func TestDiagnosis_ModerateUsesLayeredAndIterative(t *testing.T) {
	registry := newDiagRegistry()

	intentResp, _ := json.Marshal(map[string]interface{}{
		"problem_type": "network",
		"scope":        "network",
		"risk_bias":    "moderate",
	})
	complexityResp, _ := json.Marshal(map[string]interface{}{
		"complexity": "moderate",
		"reason":     "cross-domain issue",
	})
	layer2Resp, _ := json.Marshal([]string{"dns_lookup"})
	reasoningResp, _ := json.Marshal(DiagnosisResult{
		ProblemType: "network",
		Confidence:  0.9,
		Findings:    []Finding{{Source: "test", Summary: "resolved", Level: "info"}},
		NextStep:    "done",
	})

	provider := &diagMockProvider{
		responses: []*router.CompletionResponse{
			{Content: string(intentResp)},
			{Content: string(complexityResp)},
			{Content: string(layer2Resp)},
			{Content: string(reasoningResp)},
		},
	}

	llmRouter := router.NewRouter(0)
	llmRouter.RegisterProvider(provider)

	engine := NewEngine(registry, llmRouter)

	var steps []string
	result, err := engine.RunDiagnosisWithProgress(context.Background(), "test-session", "network issues across services", func(step, detail string) {
		steps = append(steps, step)
	})
	if err != nil {
		t.Fatalf("RunDiagnosis failed: %v", err)
	}

	if result.Confidence < 0.5 {
		t.Errorf("expected reasonable confidence, got %f", result.Confidence)
	}

	hasLayered := false
	for _, s := range steps {
		if s == "evidence_collect_layered" || s == "evidence_layer1" || s == "evidence_layer2" {
			hasLayered = true
		}
	}
	if !hasLayered {
		t.Error("moderate complexity should use layered evidence collection")
	}
}

// ── Regression: Heuristic plan still works without LLM ───────────────

func TestDiagnosis_HeuristicFallback(t *testing.T) {
	registry := newDiagRegistry()
	engine := NewEngine(registry, nil)

	result, err := engine.RunDiagnosisWithProgress(context.Background(), "test-session", "DNS 解析失败", nil)
	if err != nil {
		t.Fatalf("RunDiagnosis failed: %v", err)
	}

	if result.ProblemType != "dns" {
		t.Errorf("expected problem_type dns, got %s", result.ProblemType)
	}

	if result.DurationMs < 0 {
		t.Error("expected non-negative duration")
	}
}

// ── Complexity assessment ────────────────────────────────────────────

func TestComplexity_Heuristic(t *testing.T) {
	tests := []struct {
		input    string
		plan     *DiagnosisPlan
		expected ComplexityLevel
	}{
		{"check disk space", &DiagnosisPlan{ProblemType: "disk", Scope: "local"}, ComplexitySimple},
		{"network issues", &DiagnosisPlan{ProblemType: "network", Scope: "network"}, ComplexityModerate},
		{"intermittent failures across multiple services", &DiagnosisPlan{ProblemType: "service", Scope: "local"}, ComplexityComplex},
		{"production outage all services down", &DiagnosisPlan{ProblemType: "service", Scope: "cluster"}, ComplexityCritical},
	}

	for _, tt := range tests {
		got := heuristicComplexity(tt.input, tt.plan)
		if got != tt.expected {
			t.Errorf("heuristicComplexity(%q) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestMaxIterationsByComplexity(t *testing.T) {
	if MaxIterationsByComplexity(ComplexitySimple) != 1 {
		t.Error("simple should have 1 iteration")
	}
	if MaxIterationsByComplexity(ComplexityModerate) != 2 {
		t.Error("moderate should have 2 iterations")
	}
	if MaxIterationsByComplexity(ComplexityComplex) != 3 {
		t.Error("complex should have 3 iterations")
	}
	if MaxIterationsByComplexity(ComplexityCritical) != 4 {
		t.Error("critical should have 4 iterations")
	}
}

func TestToolBudgetByComplexity(t *testing.T) {
	if ToolBudgetByComplexity(ComplexitySimple) < 5 {
		t.Error("simple budget too low")
	}
	if ToolBudgetByComplexity(ComplexityCritical) <= ToolBudgetByComplexity(ComplexitySimple) {
		t.Error("critical budget should be higher than simple")
	}
}

// ── NeedsRemediation evaluation ──────────────────────────────────────

func TestEvaluateNeedsRemediation(t *testing.T) {
	registry := newDiagRegistry()
	engine := NewEngine(registry, nil)

	readOnlyResult := &DiagnosisResult{
		RecommendedActions: []ActionDraft{
			{ToolName: "read_system_info", RiskLevel: "L0"},
		},
	}
	engine.evaluateNeedsRemediation(readOnlyResult)
	if readOnlyResult.NeedsRemediation {
		t.Error("L0-only actions should not need remediation")
	}

	writeResult := &DiagnosisResult{
		RecommendedActions: []ActionDraft{
			{ToolName: "read_system_info", RiskLevel: "L0"},
			{ToolName: "dns_flush_cache", RiskLevel: "L2"},
		},
	}
	engine.evaluateNeedsRemediation(writeResult)
	if !writeResult.NeedsRemediation {
		t.Error("actions with L2 should need remediation")
	}
}

// ── Regression: existing DiagnosisResult fields preserved ────────────

func TestDiagnosisResult_FieldsPreserved(t *testing.T) {
	result := DiagnosisResult{
		ProblemType:      "network",
		Confidence:       0.85,
		Findings:         []Finding{{Source: "test", Summary: "ok", Level: "info"}},
		ApprovalRequired: true,
		NeedsRemediation: true,
		NextStep:         "review",
		DurationMs:       100,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded DiagnosisResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ProblemType != result.ProblemType {
		t.Error("ProblemType mismatch")
	}
	if decoded.NeedsRemediation != result.NeedsRemediation {
		t.Error("NeedsRemediation field not preserved in JSON round-trip")
	}
	if decoded.ApprovalRequired != result.ApprovalRequired {
		t.Error("ApprovalRequired field not preserved")
	}
}
