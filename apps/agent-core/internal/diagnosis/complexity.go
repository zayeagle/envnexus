package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
)

// ComplexityLevel represents the assessed complexity of a diagnostic problem.
type ComplexityLevel string

const (
	ComplexitySimple   ComplexityLevel = "simple"
	ComplexityModerate ComplexityLevel = "moderate"
	ComplexityComplex  ComplexityLevel = "complex"
	ComplexityCritical ComplexityLevel = "critical"
)

// MaxIterationsByComplexity returns the maximum reasoning iterations for a given complexity.
func MaxIterationsByComplexity(c ComplexityLevel) int {
	switch c {
	case ComplexitySimple:
		return 1
	case ComplexityModerate:
		return 2
	case ComplexityComplex:
		return 3
	case ComplexityCritical:
		return 4
	default:
		return 1
	}
}

// ToolBudgetByComplexity returns the maximum number of tools to use per evidence layer.
func ToolBudgetByComplexity(c ComplexityLevel) int {
	switch c {
	case ComplexitySimple:
		return 8
	case ComplexityModerate:
		return 12
	case ComplexityComplex:
		return 18
	case ComplexityCritical:
		return 25
	default:
		return 8
	}
}

type complexityResponse struct {
	Complexity string `json:"complexity"`
	Reason     string `json:"reason"`
}

// assessComplexity uses the LLM to evaluate problem complexity based on the intent
// and the initial diagnosis plan. Falls back to heuristic if LLM is unavailable.
func (e *Engine) assessComplexity(ctx context.Context, input string, plan *DiagnosisPlan) ComplexityLevel {
	if e.llmRouter == nil {
		return heuristicComplexity(input, plan)
	}

	prompt := fmt.Sprintf(`You are a diagnostic complexity assessor. Given a problem description and initial classification, assess the complexity level.

Problem: %s
Problem type: %s
Scope: %s

Complexity levels:
- "simple": Single-domain issue, likely one root cause, standard tools sufficient (e.g. "check disk space", "ping a host")
- "moderate": Cross-domain or multi-factor issue, may need layered evidence (e.g. "app is slow", "service intermittently fails")
- "complex": Multi-system issue with unclear root cause, needs iterative investigation (e.g. "random network drops across multiple services")
- "critical": Production-impacting issue across multiple systems with cascading failures

Return ONLY a JSON object: {"complexity": "simple|moderate|complex|critical", "reason": "brief explanation"}`, input, plan.ProblemType, plan.Scope)

	resp, err := e.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a structured diagnostic classifier. Output ONLY valid JSON."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   128,
		Temperature: 0.1,
	})
	if err != nil {
		slog.Warn("[diagnosis] Complexity assessment LLM failed, using heuristic", "error", err)
		return heuristicComplexity(input, plan)
	}

	content := extractJSON(resp.Content)
	var result complexityResponse
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		slog.Warn("[diagnosis] Failed to parse complexity response", "error", err)
		return heuristicComplexity(input, plan)
	}

	level := parseComplexityLevel(result.Complexity)
	slog.Info("[diagnosis] Complexity assessed", "level", level, "reason", result.Reason)
	return level
}

func parseComplexityLevel(s string) ComplexityLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "simple":
		return ComplexitySimple
	case "moderate":
		return ComplexityModerate
	case "complex":
		return ComplexityComplex
	case "critical":
		return ComplexityCritical
	default:
		return ComplexitySimple
	}
}

func heuristicComplexity(input string, plan *DiagnosisPlan) ComplexityLevel {
	lower := strings.ToLower(input)

	criticalKeywords := []string{"production", "生产", "cascading", "级联", "outage", "宕机", "all services", "全部服务"}
	for _, kw := range criticalKeywords {
		if strings.Contains(lower, kw) {
			return ComplexityCritical
		}
	}

	complexKeywords := []string{"intermittent", "间歇", "random", "随机", "multiple", "多个", "across", "跨"}
	for _, kw := range complexKeywords {
		if strings.Contains(lower, kw) {
			return ComplexityComplex
		}
	}

	if plan.Scope == "cluster" || plan.Scope == "network" {
		return ComplexityModerate
	}

	if len(plan.ToolNames) > 6 {
		return ComplexityModerate
	}

	return ComplexitySimple
}
