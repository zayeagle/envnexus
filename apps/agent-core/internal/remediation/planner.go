package remediation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/diagnosis"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type Planner struct {
	registry  *tools.Registry
	llmRouter *router.Router
}

func NewPlanner(registry *tools.Registry, llmRouter *router.Router) *Planner {
	return &Planner{
		registry:  registry,
		llmRouter: llmRouter,
	}
}

// llmPlanResponse is the expected JSON structure from the LLM.
type llmPlanResponse struct {
	Summary string        `json:"summary"`
	Steps   []llmPlanStep `json:"steps"`
}

type llmPlanStep struct {
	StepID      int                    `json:"step_id"`
	Description string                 `json:"description"`
	ToolName    string                 `json:"tool_name"`
	Params      map[string]interface{} `json:"params"`
	DependsOn   []int                  `json:"depends_on"`
	Rollback    *llmRollback           `json:"rollback,omitempty"`
	Verify      *llmVerify             `json:"verify,omitempty"`
}

type llmRollback struct {
	ToolName    string                 `json:"tool_name"`
	Params      map[string]interface{} `json:"params"`
	Description string                 `json:"description"`
}

type llmVerify struct {
	ToolName string                 `json:"tool_name"`
	Params   map[string]interface{} `json:"params"`
	Expect   string                 `json:"expect"`
}

// GeneratePlan creates a remediation plan from a diagnosis result using LLM + engine validation.
func (p *Planner) GeneratePlan(ctx context.Context, diagResult *diagnosis.DiagnosisResult) (*RemediationPlan, error) {
	if p.llmRouter == nil {
		return nil, fmt.Errorf("no LLM router configured")
	}

	llmPlan, err := p.callLLM(ctx, diagResult)
	if err != nil {
		return nil, fmt.Errorf("LLM plan generation: %w", err)
	}

	plan, err := p.validate(llmPlan)
	if err != nil {
		return nil, fmt.Errorf("plan validation: %w", err)
	}

	return plan, nil
}

func (p *Planner) callLLM(ctx context.Context, diagResult *diagnosis.DiagnosisResult) (*llmPlanResponse, error) {
	var toolList strings.Builder
	for _, t := range p.registry.List() {
		toolList.WriteString(fmt.Sprintf("- %s: %s (read_only=%v, risk=%s)\n", t.Name(), t.Description(), t.IsReadOnly(), t.RiskLevel()))
	}

	diagJSON, _ := json.MarshalIndent(diagResult, "", "  ")

	prompt := fmt.Sprintf(`You are a remediation plan generator. Given a diagnosis result, generate a structured remediation plan.

Diagnosis result:
%s

Available tools (ONLY use these tool_name values):
%s

Generate a JSON remediation plan with:
- "summary": brief description of the fix
- "steps": array of ordered steps, each with:
  - "step_id": integer starting from 1
  - "description": what this step does
  - "tool_name": MUST be one of the available tools listed above
  - "params": parameters for the tool
  - "depends_on": array of step_ids that must complete first (empty for first steps)
  - "rollback": optional {"tool_name", "params", "description"} for undoing this step
  - "verify": optional {"tool_name", "params", "expect"} for verifying success

Rules:
1. Prefer read-only verification steps before write operations.
2. Include rollback for any write operation when possible.
3. Include verification after write operations.
4. Order steps logically with proper dependencies.
5. Keep plans concise — typically 2-6 steps.

You MUST respond with ONLY a JSON object. No explanation, no markdown.`, string(diagJSON), toolList.String())

	resp, err := p.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a structured remediation plan generator. Output ONLY valid JSON. No explanation, no markdown fences."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   2048,
		Temperature: 0.2,
	})
	if err != nil {
		return nil, err
	}

	content := extractJSON(resp.Content)
	var plan llmPlanResponse
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("parse LLM plan response: %w (content: %s)", err, content[:min(len(content), 300)])
	}
	return &plan, nil
}

// validate converts the LLM response into a validated RemediationPlan.
// It enforces: tool existence, registry risk levels, DAG acyclicity, rollback injection.
func (p *Planner) validate(llmPlan *llmPlanResponse) (*RemediationPlan, error) {
	steps := make([]RemediationStep, 0, len(llmPlan.Steps))

	for _, ls := range llmPlan.Steps {
		tool, ok := p.registry.Get(ls.ToolName)
		if !ok {
			slog.Warn("[remediation] LLM referenced non-existent tool, skipping step", "tool", ls.ToolName, "step_id", ls.StepID)
			continue
		}

		riskLevel := tool.RiskLevel()

		step := RemediationStep{
			StepID:      ls.StepID,
			Description: ls.Description,
			ToolName:    ls.ToolName,
			Params:      ls.Params,
			RiskLevel:   riskLevel,
			DependsOn:   ls.DependsOn,
			Status:      StepStatusPending,
		}

		if ls.Rollback != nil {
			if _, rbOk := p.registry.Get(ls.Rollback.ToolName); rbOk {
				step.Rollback = &RollbackAction{
					ToolName:    ls.Rollback.ToolName,
					Params:      ls.Rollback.Params,
					Description: ls.Rollback.Description,
				}
			}
		}

		if ls.Verify != nil {
			if _, vOk := p.registry.Get(ls.Verify.ToolName); vOk {
				step.Verify = &ToolCheck{
					ToolName: ls.Verify.ToolName,
					Params:   ls.Verify.Params,
					Expect:   ls.Verify.Expect,
				}
			}
		}

		if step.Rollback == nil && !tool.IsReadOnly() {
			p.injectDefaultRollback(&step)
		}

		steps = append(steps, step)
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("no valid steps after validation")
	}

	_, err := BuildDAG(steps)
	if err != nil {
		return nil, fmt.Errorf("DAG validation: %w", err)
	}

	plan := &RemediationPlan{
		PlanID:  generatePlanID(),
		Summary: llmPlan.Summary,
		Steps:   steps,
		Status:  PlanStatusGenerated,
	}
	plan.RiskLevel = plan.OverallRiskLevel()

	slog.Info("[remediation] Plan generated",
		"plan_id", plan.PlanID,
		"steps", len(plan.Steps),
		"risk_level", plan.RiskLevel,
	)

	return plan, nil
}

func (p *Planner) injectDefaultRollback(step *RemediationStep) {
	// For common write tools, inject sensible rollback hints.
	// The actual rollback data (e.g. original config value) is captured by the snapshot manager at runtime.
	switch {
	case strings.Contains(step.ToolName, "restart"):
		step.Rollback = &RollbackAction{
			Description: "Service was restarted; manual check may be needed if restart caused issues",
		}
	case strings.Contains(step.ToolName, "config_modify"):
		step.Rollback = &RollbackAction{
			Description: "Configuration was modified; snapshot will restore original value",
		}
	}
}

func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	if len(s) > 0 && s[0] == '{' {
		return s
	}

	start := strings.Index(s, "{")
	if start == -1 {
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

func generatePlanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "plan-" + hex.EncodeToString(b)
}
