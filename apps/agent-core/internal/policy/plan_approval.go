package policy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/remediation"
)

// PlanApprovalRequest represents a pending plan-level approval.
type PlanApprovalRequest struct {
	ID        string                    `json:"id"`
	PlanID    string                    `json:"plan_id"`
	Summary   string                    `json:"summary"`
	RiskLevel string                    `json:"risk_level"`
	Steps     []remediation.RemediationStep `json:"steps"`
	Status    ApprovalStatus            `json:"status"`
	CreatedAt time.Time                 `json:"created_at"`
	ExpiresAt time.Time                 `json:"expires_at"`
	ResultCh  chan bool                 `json:"-"`
}

// CheckPlan evaluates whether a remediation plan can proceed based on its risk level.
// L0-only plans are auto-approved. L1+ plans require user approval.
// This method does NOT modify existing Check/CheckWithSession behavior.
func (e *Engine) CheckPlan(ctx context.Context, plan *remediation.RemediationPlan) (bool, error) {
	riskLevel := plan.OverallRiskLevel()

	if riskLevel == remediation.RiskL0 {
		slog.Info("[policy] Plan auto-approved (L0 only)", "plan_id", plan.PlanID)
		return true, nil
	}

	reqID := generateID()
	req := &PlanApprovalRequest{
		ID:        reqID,
		PlanID:    plan.PlanID,
		Summary:   plan.Summary,
		RiskLevel: riskLevel,
		Steps:     plan.Steps,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(10 * time.Minute),
		ResultCh:  make(chan bool, 1),
	}

	e.mu.Lock()
	e.pendingApprovals[reqID] = &ApprovalRequest{
		ID:        reqID,
		ToolName:  "__plan__",
		RiskLevel: riskLevel,
		Params: map[string]interface{}{
			"plan_id":    plan.PlanID,
			"summary":    plan.Summary,
			"steps":      len(plan.Steps),
			"risk_level": riskLevel,
		},
		Status:    StatusPending,
		CreatedAt: req.CreatedAt,
		ExpiresAt: req.ExpiresAt,
		ResultCh:  req.ResultCh,
	}
	e.mu.Unlock()

	slog.Info("[policy] Plan requires approval",
		"plan_id", plan.PlanID,
		"risk_level", riskLevel,
		"request_id", reqID,
		"steps", len(plan.Steps),
	)

	select {
	case <-ctx.Done():
		e.removeRequest(reqID)
		return false, ctx.Err()
	case <-time.After(time.Until(req.ExpiresAt)):
		e.removeRequest(reqID)
		return false, fmt.Errorf("plan approval request expired")
	case approved := <-req.ResultCh:
		e.removeRequest(reqID)
		if !approved {
			return false, fmt.Errorf("plan rejected by user")
		}
		slog.Info("[policy] Plan approved", "plan_id", plan.PlanID, "request_id", reqID)
		return true, nil
	}
}

// NeedsPlanApproval returns the approval level needed for a plan without blocking.
func (e *Engine) NeedsPlanApproval(plan *remediation.RemediationPlan) string {
	return plan.OverallRiskLevel()
}
