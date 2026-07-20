package remediation

import (
	"context"
	"testing"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type failingTool struct {
	mockTool
}

func (f *failingTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	return &tools.ToolResult{ToolName: f.name, Status: "failed", Error: "simulated failure"}, nil
}

func TestExecutor_NormalFlow(t *testing.T) {
	registry := newMockRegistry()
	executor := NewExecutor(registry, nil)

	plan := &RemediationPlan{
		PlanID: "test-plan-1",
		Steps: []RemediationStep{
			{StepID: 1, ToolName: "read_system_info", RiskLevel: RiskL0, Status: StepStatusPending},
			{StepID: 2, ToolName: "ping_host", RiskLevel: RiskL0, DependsOn: []int{1}, Status: StepStatusPending},
		},
		Status: PlanStatusApproved,
	}

	err := executor.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if plan.Status != PlanStatusCompleted {
		t.Errorf("expected status completed, got %s", plan.Status)
	}

	for _, step := range plan.Steps {
		if step.Status != StepStatusSucceeded {
			t.Errorf("step %d: expected succeeded, got %s", step.StepID, step.Status)
		}
	}
}

func TestExecutor_RollbackOnFailure(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "read_system_info", readOnly: true, risk: "L0"})
	registry.Register(&failingTool{mockTool: mockTool{name: "dns_flush_cache", readOnly: false, risk: "L2"}})
	registry.Register(&mockTool{name: "rollback_tool", readOnly: false, risk: "L1"})

	var rollbackEvents []string
	executor := NewExecutor(registry, nil,
		WithExecutorEventHandler(func(eventType string, data map[string]interface{}) {
			rollbackEvents = append(rollbackEvents, eventType)
		}),
	)

	plan := &RemediationPlan{
		PlanID: "test-plan-rollback",
		Steps: []RemediationStep{
			{StepID: 1, ToolName: "read_system_info", RiskLevel: RiskL0, Status: StepStatusPending},
			{
				StepID: 2, ToolName: "dns_flush_cache", RiskLevel: RiskL2, DependsOn: []int{1}, Status: StepStatusPending,
				Rollback: &RollbackAction{ToolName: "rollback_tool", Description: "undo flush"},
			},
		},
		Status: PlanStatusApproved,
	}

	err := executor.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error from failing step")
	}

	if plan.Status != PlanStatusFailed {
		t.Errorf("expected status failed, got %s", plan.Status)
	}

	hasRollback := false
	for _, evt := range rollbackEvents {
		if evt == EventRollbackStart {
			hasRollback = true
		}
	}
	if !hasRollback {
		t.Error("expected rollback_start event")
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	registry := newMockRegistry()
	executor := NewExecutor(registry, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := &RemediationPlan{
		PlanID: "test-plan-cancel",
		Steps: []RemediationStep{
			{StepID: 1, ToolName: "read_system_info", RiskLevel: RiskL0, Status: StepStatusPending},
		},
		Status: PlanStatusApproved,
	}

	err := executor.Execute(ctx, plan)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestExecutor_EventEmission(t *testing.T) {
	registry := newMockRegistry()

	var events []string
	executor := NewExecutor(registry, nil,
		WithExecutorEventHandler(func(eventType string, data map[string]interface{}) {
			events = append(events, eventType)
		}),
	)

	plan := &RemediationPlan{
		PlanID:    "test-plan-events",
		CreatedAt: time.Now(),
		Steps: []RemediationStep{
			{StepID: 1, ToolName: "read_system_info", RiskLevel: RiskL0, Status: StepStatusPending},
		},
		Status: PlanStatusApproved,
	}

	err := executor.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := []string{EventStepStart, EventStepResult, EventPlanComplete}
	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(events), events)
	}
	for i, e := range expected {
		if events[i] != e {
			t.Errorf("event %d: expected %s, got %s", i, e, events[i])
		}
	}
}
