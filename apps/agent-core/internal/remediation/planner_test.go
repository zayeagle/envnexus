package remediation

import (
	"context"
	"testing"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type mockTool struct {
	name     string
	readOnly bool
	risk     string
}

func (m *mockTool) Name() string                                                        { return m.name }
func (m *mockTool) Description() string                                                 { return "mock " + m.name }
func (m *mockTool) IsReadOnly() bool                                                    { return m.readOnly }
func (m *mockTool) RiskLevel() string                                                   { return m.risk }
func (m *mockTool) Parameters() *tools.ParamSchema                                      { return tools.NoParams() }
func (m *mockTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	return &tools.ToolResult{ToolName: m.name, Status: "ok", Summary: "done"}, nil
}

func newMockRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(&mockTool{name: "read_system_info", readOnly: true, risk: "L0"})
	r.Register(&mockTool{name: "dns_flush_cache", readOnly: false, risk: "L2"})
	r.Register(&mockTool{name: "service_restart", readOnly: false, risk: "L2"})
	r.Register(&mockTool{name: "ping_host", readOnly: true, risk: "L0"})
	return r
}

func TestPlanner_Validate_FiltersUnknownTools(t *testing.T) {
	p := NewPlanner(newMockRegistry(), nil)

	llmPlan := &llmPlanResponse{
		Summary: "Fix DNS",
		Steps: []llmPlanStep{
			{StepID: 1, ToolName: "read_system_info", Description: "Check system"},
			{StepID: 2, ToolName: "nonexistent_tool", Description: "This should be filtered"},
			{StepID: 3, ToolName: "dns_flush_cache", Description: "Flush DNS", DependsOn: []int{1}},
		},
	}

	plan, err := p.validate(llmPlan)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	if len(plan.Steps) != 2 {
		t.Errorf("expected 2 valid steps, got %d", len(plan.Steps))
	}
}

func TestPlanner_Validate_EnforcesRegistryRiskLevel(t *testing.T) {
	p := NewPlanner(newMockRegistry(), nil)

	llmPlan := &llmPlanResponse{
		Summary: "Test risk override",
		Steps: []llmPlanStep{
			{StepID: 1, ToolName: "dns_flush_cache", Description: "Flush DNS"},
		},
	}

	plan, err := p.validate(llmPlan)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	if plan.Steps[0].RiskLevel != "L2" {
		t.Errorf("expected risk level L2 from registry, got %s", plan.Steps[0].RiskLevel)
	}
}

func TestPlanner_Validate_DetectsCycle(t *testing.T) {
	p := NewPlanner(newMockRegistry(), nil)

	llmPlan := &llmPlanResponse{
		Summary: "Cycle test",
		Steps: []llmPlanStep{
			{StepID: 1, ToolName: "read_system_info", Description: "A", DependsOn: []int{2}},
			{StepID: 2, ToolName: "ping_host", Description: "B", DependsOn: []int{1}},
		},
	}

	_, err := p.validate(llmPlan)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestPlanner_Validate_EmptyAfterFilter(t *testing.T) {
	p := NewPlanner(newMockRegistry(), nil)

	llmPlan := &llmPlanResponse{
		Summary: "All invalid",
		Steps: []llmPlanStep{
			{StepID: 1, ToolName: "fake_tool_1", Description: "Nope"},
			{StepID: 2, ToolName: "fake_tool_2", Description: "Also nope"},
		},
	}

	_, err := p.validate(llmPlan)
	if err == nil {
		t.Fatal("expected error for empty plan after validation")
	}
}

func TestPlanner_Validate_InjectsDefaultRollback(t *testing.T) {
	p := NewPlanner(newMockRegistry(), nil)

	llmPlan := &llmPlanResponse{
		Summary: "Restart service",
		Steps: []llmPlanStep{
			{StepID: 1, ToolName: "service_restart", Description: "Restart svc"},
		},
	}

	plan, err := p.validate(llmPlan)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	if plan.Steps[0].Rollback == nil {
		t.Error("expected default rollback to be injected for service_restart")
	}
}
