package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

// EventHandler receives execution progress events.
type EventHandler func(eventType string, data map[string]interface{})

// StepConfirmHandler is called for L2 steps that need execution-time confirmation.
// Returns true if the user confirms execution.
type StepConfirmHandler func(step *RemediationStep) bool

// StepApprovalHandler is called for L3 steps that need per-step approval.
// Returns true if the user approves.
type StepApprovalHandler func(step *RemediationStep) bool

// Executor runs a remediation plan following DAG topological order.
type Executor struct {
	registry    *tools.Registry
	snapshots   *SnapshotManager
	onEvent     EventHandler
	onConfirm   StepConfirmHandler
	onApproval  StepApprovalHandler
}

type ExecutorOption func(*Executor)

func WithExecutorEventHandler(h EventHandler) ExecutorOption {
	return func(e *Executor) { e.onEvent = h }
}

func WithStepConfirmHandler(h StepConfirmHandler) ExecutorOption {
	return func(e *Executor) { e.onConfirm = h }
}

func WithStepApprovalHandler(h StepApprovalHandler) ExecutorOption {
	return func(e *Executor) { e.onApproval = h }
}

func NewExecutor(registry *tools.Registry, snapshots *SnapshotManager, opts ...ExecutorOption) *Executor {
	e := &Executor{
		registry:  registry,
		snapshots: snapshots,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Executor) emit(eventType string, data map[string]interface{}) {
	if e.onEvent != nil {
		e.onEvent(eventType, data)
	}
}

// Execute runs all steps in the plan according to DAG order.
// It handles precondition checks, snapshots, approval gates, execution, verification, and rollback.
func (e *Executor) Execute(ctx context.Context, plan *RemediationPlan) error {
	dag, err := BuildDAG(plan.Steps)
	if err != nil {
		return fmt.Errorf("build DAG: %w", err)
	}

	order, err := dag.ExecutionOrder()
	if err != nil {
		return fmt.Errorf("execution order: %w", err)
	}

	plan.Status = PlanStatusExecuting
	var completedSteps []int

	for _, stepID := range order {
		if ctx.Err() != nil {
			plan.Status = PlanStatusFailed
			return ctx.Err()
		}

		step, _ := dag.GetStep(stepID)

		e.emit(EventStepStart, map[string]interface{}{
			"plan_id":     plan.PlanID,
			"step_id":     step.StepID,
			"tool_name":   step.ToolName,
			"description": step.Description,
			"risk_level":  step.RiskLevel,
		})

		if err := e.executeStep(ctx, plan, step); err != nil {
			slog.Error("[remediation] Step failed, initiating rollback",
				"plan_id", plan.PlanID, "step_id", step.StepID, "error", err)

			e.emit(EventStepResult, map[string]interface{}{
				"plan_id":   plan.PlanID,
				"step_id":   step.StepID,
				"status":    "failed",
				"error":     err.Error(),
			})

			step.Status = StepStatusFailed
			e.rollback(ctx, plan, dag, completedSteps)
			plan.Status = PlanStatusFailed

			e.emit(EventPlanComplete, map[string]interface{}{
				"plan_id": plan.PlanID,
				"status":  string(plan.Status),
				"error":   err.Error(),
			})
			return fmt.Errorf("step %d (%s) failed: %w", step.StepID, step.ToolName, err)
		}

		step.Status = StepStatusSucceeded
		completedSteps = append(completedSteps, stepID)

		e.emit(EventStepResult, map[string]interface{}{
			"plan_id": plan.PlanID,
			"step_id": step.StepID,
			"status":  "succeeded",
		})
	}

	plan.Status = PlanStatusCompleted
	e.emit(EventPlanComplete, map[string]interface{}{
		"plan_id": plan.PlanID,
		"status":  string(plan.Status),
	})

	if e.snapshots != nil {
		_ = e.snapshots.CleanupPlan(plan.PlanID)
	}

	return nil
}

func (e *Executor) executeStep(ctx context.Context, plan *RemediationPlan, step *RemediationStep) error {
	tool, ok := e.registry.Get(step.ToolName)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", step.ToolName)
	}

	if step.Precondition != nil {
		if err := e.runCheck(ctx, step.Precondition, "precondition"); err != nil {
			step.Status = StepStatusSkipped
			return fmt.Errorf("precondition failed: %w", err)
		}
	}

	if !tool.IsReadOnly() && e.snapshots != nil {
		state := e.capturePreState(ctx, step)
		if _, err := e.snapshots.Capture(plan.PlanID, step, state); err != nil {
			slog.Warn("[remediation] Snapshot capture failed, continuing", "error", err)
		}
	}

	if step.RiskLevel == RiskL2 && e.onConfirm != nil {
		step.Status = StepStatusAwaitingConfirm
		e.emit(EventStepConfirm, map[string]interface{}{
			"plan_id":     plan.PlanID,
			"step_id":     step.StepID,
			"description": step.Description,
			"risk_level":  step.RiskLevel,
		})
		if !e.onConfirm(step) {
			step.Status = StepStatusSkipped
			slog.Info("[remediation] L2 step skipped by user", "step_id", step.StepID)
			return nil
		}
	}

	if step.RiskLevel == RiskL3 && e.onApproval != nil {
		step.Status = StepStatusAwaitingApproval
		e.emit(EventStepApproval, map[string]interface{}{
			"plan_id":     plan.PlanID,
			"step_id":     step.StepID,
			"description": step.Description,
			"risk_level":  step.RiskLevel,
		})
		if !e.onApproval(step) {
			step.Status = StepStatusSkipped
			slog.Info("[remediation] L3 step denied by user", "step_id", step.StepID)
			return nil
		}
	}

	step.Status = StepStatusRunning
	timeout := step.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := tool.Execute(execCtx, step.Params)
	if err != nil {
		return fmt.Errorf("tool execution: %w", err)
	}
	if result.Status == "failed" {
		return fmt.Errorf("tool returned failure: %s", result.Error)
	}

	if step.Verify != nil {
		if err := e.runCheck(ctx, step.Verify, "verification"); err != nil {
			return fmt.Errorf("post-execution verification failed: %w", err)
		}
	}

	return nil
}

func (e *Executor) capturePreState(ctx context.Context, step *RemediationStep) map[string]interface{} {
	state := map[string]interface{}{
		"tool_name":  step.ToolName,
		"params":     step.Params,
		"captured_at": time.Now().UTC().Format(time.RFC3339),
	}

	if step.Verify != nil {
		if verifyTool, ok := e.registry.Get(step.Verify.ToolName); ok {
			result, err := verifyTool.Execute(ctx, step.Verify.Params)
			if err == nil {
				state["pre_state"] = result.Output
			}
		}
	}

	return state
}

func (e *Executor) runCheck(ctx context.Context, check *ToolCheck, label string) error {
	tool, ok := e.registry.Get(check.ToolName)
	if !ok {
		return fmt.Errorf("%s tool %q not found", label, check.ToolName)
	}

	result, err := tool.Execute(ctx, check.Params)
	if err != nil {
		return fmt.Errorf("%s tool execution failed: %w", label, err)
	}
	if result.Status == "failed" {
		return fmt.Errorf("%s check failed: %s", label, result.Error)
	}
	return nil
}

func (e *Executor) rollback(ctx context.Context, plan *RemediationPlan, dag *DAG, completedSteps []int) {
	e.emit(EventRollbackStart, map[string]interface{}{
		"plan_id":         plan.PlanID,
		"steps_to_revert": len(completedSteps),
	})

	for i := len(completedSteps) - 1; i >= 0; i-- {
		stepID := completedSteps[i]
		step, ok := dag.GetStep(stepID)
		if !ok || step.Rollback == nil {
			continue
		}

		tool, ok := e.registry.Get(step.ToolName)
		if !ok || tool.IsReadOnly() {
			continue
		}

		slog.Info("[remediation] Rolling back step", "plan_id", plan.PlanID, "step_id", stepID)

		if step.Rollback.ToolName != "" {
			if rbTool, rbOk := e.registry.Get(step.Rollback.ToolName); rbOk {
				rbCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				_, rbErr := rbTool.Execute(rbCtx, step.Rollback.Params)
				cancel()
				if rbErr != nil {
					slog.Error("[remediation] Rollback step failed", "step_id", stepID, "error", rbErr)
				} else {
					step.Status = StepStatusRolledBack
				}
			}
		}
	}

	plan.Status = PlanStatusRolledBack
}
