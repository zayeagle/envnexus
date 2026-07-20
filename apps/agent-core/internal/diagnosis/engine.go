package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type DiagnosisPlan struct {
	ProblemType string                            `json:"problem_type"`
	Scope       string                            `json:"scope"`
	RiskBias    string                            `json:"risk_bias"`
	ToolNames   []string                          `json:"tool_names"`
	ToolParams  map[string]map[string]interface{} `json:"tool_params,omitempty"`
	Complexity  ComplexityLevel                   `json:"complexity,omitempty"`
}

type DiagnosisResult struct {
	ProblemType        string           `json:"problem_type"`
	Confidence         float64          `json:"confidence"`
	Findings           []Finding        `json:"findings"`
	RecommendedActions []ActionDraft    `json:"recommended_actions"`
	ApprovalRequired   bool             `json:"approval_required"`
	NeedsRemediation   bool             `json:"needs_remediation"`
	NextStep           string           `json:"next_step"`
	Evidence           map[string]interface{} `json:"evidence"`
	DurationMs         int64            `json:"duration_ms"`
}

type Finding struct {
	Source  string `json:"source"`
	Summary string `json:"summary"`
	Level   string `json:"level"`
}

type ActionDraft struct {
	ToolName    string                 `json:"tool_name"`
	Description string                 `json:"description"`
	RiskLevel   string                 `json:"risk_level"`
	Params      map[string]interface{} `json:"params"`
}

type ProgressFn func(step string, detail string)

type Engine struct {
	registry  *tools.Registry
	llmRouter *router.Router
}

func NewEngine(registry *tools.Registry, llmRouter *router.Router) *Engine {
	return &Engine{
		registry:  registry,
		llmRouter: llmRouter,
	}
}

func (e *Engine) Plan(ctx context.Context, sessionID, input string) (*DiagnosisPlan, error) {
	return e.PlanWithProgress(ctx, sessionID, input, nil)
}

func (e *Engine) PlanWithProgress(ctx context.Context, sessionID, input string, onProgress ProgressFn) (*DiagnosisPlan, error) {
	slog.Info("[diagnosis] Planning", "session_id", sessionID, "input", input)

	notify(onProgress, "intent_parse", "Analyzing problem type...")
	plan, err := e.stepIntentParse(ctx, input)
	if err != nil {
		slog.Warn("[diagnosis] IntentParse failed, using heuristic", "error", err)
		notify(onProgress, "intent_parse_fallback", "Using local heuristic analysis")
		plan = e.heuristicPlan(input)
	}

	notify(onProgress, "complexity_assess", "Assessing problem complexity...")
	plan.Complexity = e.assessComplexity(ctx, input, plan)
	notify(onProgress, "complexity_done", fmt.Sprintf("Complexity: %s", plan.Complexity))

	plan.ToolNames = e.stepEvidencePlan(plan)
	notify(onProgress, "plan_ready", fmt.Sprintf("Problem type: %s, complexity: %s, tools: %d", plan.ProblemType, plan.Complexity, len(plan.ToolNames)))
	slog.Info("[diagnosis] Plan", "problem_type", plan.ProblemType, "complexity", plan.Complexity, "tools", plan.ToolNames)
	return plan, nil
}

func (e *Engine) Execute(ctx context.Context, sessionID string, plan *DiagnosisPlan) (*DiagnosisResult, error) {
	return e.ExecuteWithProgress(ctx, sessionID, plan, nil)
}

func (e *Engine) ExecuteWithProgress(ctx context.Context, sessionID string, plan *DiagnosisPlan, onProgress ProgressFn) (*DiagnosisResult, error) {
	start := time.Now()
	slog.Info("[diagnosis] Executing plan", "session_id", sessionID, "problem_type", plan.ProblemType, "complexity", plan.Complexity)

	var evidence map[string]interface{}

	if plan.Complexity == ComplexitySimple || plan.Complexity == "" {
		notify(onProgress, "evidence_collect", fmt.Sprintf("Collecting evidence from %d tools...", len(plan.ToolNames)))
		evidence = e.stepEvidenceCollect(ctx, plan)
		notify(onProgress, "evidence_done", fmt.Sprintf("Collected %d evidence items", len(evidence)))
	} else {
		notify(onProgress, "evidence_collect_layered", "Starting layered evidence collection...")
		evidence = e.stepLayeredEvidenceCollect(ctx, plan, onProgress)
		notify(onProgress, "evidence_done", fmt.Sprintf("Collected %d evidence items (layered)", len(evidence)))
	}

	var reasoning *DiagnosisResult
	var err error

	if plan.Complexity == ComplexitySimple || plan.Complexity == "" {
		notify(onProgress, "reasoning", "Generating diagnosis...")
		reasoning, err = e.stepReasoning(ctx, plan, evidence)
		if err != nil {
			slog.Warn("[diagnosis] Reasoning via LLM failed, using local analysis", "error", err)
			notify(onProgress, "reasoning_fallback", "Using local analysis engine")
			reasoning = e.localReasoning(plan, evidence)
		}
	} else {
		notify(onProgress, "reasoning_iterative", "Starting iterative reasoning...")
		reasoning, err = e.stepIterativeReasoning(ctx, plan, evidence, onProgress)
		if err != nil {
			slog.Warn("[diagnosis] Iterative reasoning failed, falling back to single-shot", "error", err)
			notify(onProgress, "reasoning_fallback", "Falling back to single-shot reasoning")
			reasoning, err = e.stepReasoning(ctx, plan, evidence)
			if err != nil {
				notify(onProgress, "reasoning_fallback", "Using local analysis engine")
				reasoning = e.localReasoning(plan, evidence)
			}
		}
	}

	reasoning.Evidence = evidence
	reasoning.DurationMs = time.Since(start).Milliseconds()

	if len(reasoning.RecommendedActions) > 0 {
		e.stepActionDraft(reasoning)
		e.evaluateNeedsRemediation(reasoning)
	}

	notify(onProgress, "complete", fmt.Sprintf("Done in %dms", reasoning.DurationMs))
	slog.Info("[diagnosis] Complete",
		"problem_type", reasoning.ProblemType,
		"confidence", reasoning.Confidence,
		"findings", len(reasoning.Findings),
		"actions", len(reasoning.RecommendedActions),
		"needs_remediation", reasoning.NeedsRemediation,
		"duration_ms", reasoning.DurationMs,
	)

	return reasoning, nil
}

func (e *Engine) RunDiagnosis(ctx context.Context, sessionID, intent string) (*DiagnosisResult, error) {
	return e.RunDiagnosisWithProgress(ctx, sessionID, intent, nil)
}

func (e *Engine) RunDiagnosisWithProgress(ctx context.Context, sessionID, intent string, onProgress ProgressFn) (*DiagnosisResult, error) {
	plan, err := e.PlanWithProgress(ctx, sessionID, intent, onProgress)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	return e.ExecuteWithProgress(ctx, sessionID, plan, onProgress)
}

func notify(fn ProgressFn, step, detail string) {
	if fn != nil {
		fn(step, detail)
	}
}

// extractJSON finds the first valid JSON object in a string that may contain
// mixed natural language and JSON. This handles DeepSeek R1 models that return
// reasoning text with embedded JSON.
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

// Step 1: IntentParse — use LLM to classify the problem
func (e *Engine) stepIntentParse(ctx context.Context, input string) (*DiagnosisPlan, error) {
	if e.llmRouter == nil {
		return nil, fmt.Errorf("no LLM router configured")
	}

	prompt := fmt.Sprintf(`You are an IT diagnostic assistant. Analyze the user's problem description and return a JSON object with:
- "problem_type": one of "network", "dns", "service", "performance", "disk", "auth", "install", "docker", "kubernetes", "database", "general"
- "scope": one of "local", "network", "system", "cluster"
- "risk_bias": one of "conservative", "moderate", "aggressive"

User's problem: %s

You MUST respond with ONLY a JSON object. No explanation, no markdown, no thinking process. Just the raw JSON object starting with { and ending with }.`, input)

	resp, err := e.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a structured diagnostic classifier. You MUST output ONLY a valid JSON object. No explanation, no markdown fences, no thinking. Just raw JSON."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.1,
	})
	if err != nil {
		return nil, err
	}

	var plan DiagnosisPlan
	content := extractJSON(resp.Content)
	slog.Debug("[diagnosis] IntentParse raw response", "content_len", len(resp.Content), "extracted_json", content[:min(len(content), 200)])

	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w (content: %s)", err, content[:min(len(content), 300)])
	}
	return &plan, nil
}

// Step 2: EvidencePlan — select read-only tools based on problem type
func (e *Engine) stepEvidencePlan(plan *DiagnosisPlan) []string {
	toolMapping := map[string][]string{
		"network":     {"read_network_config", "read_proxy_config", "read_system_info", "ping_host", "dns_lookup", "read_route_table", "read_env_vars"},
		"dns":         {"read_network_config", "read_system_info", "dns_lookup", "read_env_vars"},
		"service":     {"read_system_info", "read_process_list", "read_env_vars", "http_check", "read_event_log", "check_runtime_deps"},
		"performance": {"read_system_info", "read_process_list", "read_disk_usage"},
		"disk":        {"read_system_info", "read_disk_usage", "read_dir_list"},
		"auth":        {"read_system_info", "read_env_vars", "check_tls_cert"},
		"install":     {"read_system_info", "read_disk_usage", "read_installed_apps", "check_runtime_deps", "read_event_log", "read_env_vars", "read_file_info"},
		"docker":      {"read_system_info", "docker_inspect", "docker_compose_check", "read_process_list", "read_disk_usage", "read_env_vars"},
		"kubernetes":  {"read_system_info", "kubectl_diagnose", "docker_inspect", "read_process_list", "read_env_vars", "read_event_log"},
		"database":    {"read_system_info", "mysql_check", "postgres_check", "redis_check", "mongo_check", "read_process_list", "read_env_vars", "read_disk_usage"},
		"general":     {"read_system_info", "read_network_config", "read_proxy_config", "read_env_vars"},
	}

	candidates := toolMapping[plan.ProblemType]
	if len(candidates) == 0 {
		candidates = toolMapping["general"]
	}

	var available []string
	for _, name := range candidates {
		if t, ok := e.registry.Get(name); ok && t.IsReadOnly() {
			available = append(available, name)
		}
	}
	return available
}

// Step 3: EvidenceCollect — execute read-only tools in parallel
func (e *Engine) stepEvidenceCollect(ctx context.Context, plan *DiagnosisPlan) map[string]interface{} {
	evidence := make(map[string]interface{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, name := range plan.ToolNames {
		t, ok := e.registry.Get(name)
		if !ok {
			continue
		}
		var params map[string]interface{}
		if plan.ToolParams != nil {
			params = plan.ToolParams[name]
		}
		wg.Add(1)
		go func(tool tools.Tool, toolName string, toolParams map[string]interface{}) {
			defer wg.Done()
			result, err := tool.Execute(ctx, toolParams)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				evidence[toolName] = map[string]interface{}{"error": err.Error()}
				slog.Warn("[diagnosis] Tool failed during evidence collection", "tool", toolName, "error", err)
			} else {
				evidence[toolName] = result.Output
				slog.Info("[diagnosis] Tool collected", "tool", toolName, "summary", result.Summary)
			}
		}(t, name, params)
	}
	wg.Wait()
	return evidence
}

// stepLayeredEvidenceCollect performs multi-layer evidence collection for moderate+ complexity.
// Layer 1: Run the planned tools (same as simple path).
// Layer 2: Ask the LLM what additional tools to run based on Layer 1 results.
func (e *Engine) stepLayeredEvidenceCollect(ctx context.Context, plan *DiagnosisPlan, onProgress ProgressFn) map[string]interface{} {
	budget := ToolBudgetByComplexity(plan.Complexity)

	notify(onProgress, "evidence_layer1", fmt.Sprintf("Layer 1: collecting from %d tools...", len(plan.ToolNames)))
	evidence := e.stepEvidenceCollect(ctx, plan)
	used := len(plan.ToolNames)

	if e.llmRouter == nil || used >= budget {
		return evidence
	}

	notify(onProgress, "evidence_layer2", "Layer 2: LLM deciding additional tools...")
	additionalTools := e.askLLMForAdditionalTools(ctx, plan, evidence, budget-used)

	if len(additionalTools) == 0 {
		slog.Info("[diagnosis] Layer 2: LLM suggested no additional tools")
		return evidence
	}

	notify(onProgress, "evidence_layer2_collect", fmt.Sprintf("Layer 2: collecting from %d additional tools...", len(additionalTools)))

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, name := range additionalTools {
		t, ok := e.registry.Get(name)
		if !ok || !t.IsReadOnly() {
			continue
		}
		alreadyHave := false
		for _, existing := range plan.ToolNames {
			if existing == name {
				alreadyHave = true
				break
			}
		}
		if alreadyHave {
			continue
		}

		wg.Add(1)
		go func(tool tools.Tool, toolName string) {
			defer wg.Done()
			result, err := tool.Execute(ctx, nil)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				evidence[toolName] = map[string]interface{}{"error": err.Error()}
			} else {
				evidence[toolName] = result.Output
				slog.Info("[diagnosis] Layer 2 tool collected", "tool", toolName)
			}
		}(t, name)
	}
	wg.Wait()

	return evidence
}

// askLLMForAdditionalTools asks the LLM to suggest more tools based on Layer 1 evidence.
func (e *Engine) askLLMForAdditionalTools(ctx context.Context, plan *DiagnosisPlan, evidence map[string]interface{}, remaining int) []string {
	evidenceSummary := make(map[string]string)
	for k, v := range evidence {
		b, _ := json.Marshal(v)
		s := string(b)
		if len(s) > 300 {
			s = s[:300] + "..."
		}
		evidenceSummary[k] = s
	}
	summaryJSON, _ := json.MarshalIndent(evidenceSummary, "", "  ")

	var toolList strings.Builder
	for _, t := range e.registry.List() {
		if !t.IsReadOnly() {
			continue
		}
		alreadyUsed := false
		for _, used := range plan.ToolNames {
			if used == t.Name() {
				alreadyUsed = true
				break
			}
		}
		if !alreadyUsed {
			toolList.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
		}
	}

	if toolList.Len() == 0 {
		return nil
	}

	prompt := fmt.Sprintf(`Based on the initial evidence collected for a "%s" problem, suggest additional read-only diagnostic tools to run for deeper investigation.

Initial evidence summary:
%s

Available additional tools (NOT yet used):
%s

Budget: suggest at most %d additional tools.

Return ONLY a JSON array of tool names, e.g. ["tool_a", "tool_b"]. If no additional tools are needed, return [].`, plan.ProblemType, string(summaryJSON), toolList.String(), remaining)

	resp, err := e.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a diagnostic tool selector. Output ONLY a valid JSON array of tool name strings."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.1,
	})
	if err != nil {
		slog.Warn("[diagnosis] Layer 2 LLM request failed", "error", err)
		return nil
	}

	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var toolNames []string
	if err := json.Unmarshal([]byte(content), &toolNames); err != nil {
		slog.Warn("[diagnosis] Failed to parse Layer 2 tool suggestions", "error", err, "content", content)
		return nil
	}

	slog.Info("[diagnosis] Layer 2 suggested tools", "tools", toolNames)
	return toolNames
}

// Step 4: Reasoning — use LLM to analyze evidence and generate diagnosis
func (e *Engine) stepReasoning(ctx context.Context, plan *DiagnosisPlan, evidence map[string]interface{}) (*DiagnosisResult, error) {
	if e.llmRouter == nil {
		return nil, fmt.Errorf("no LLM router configured")
	}

	evidenceJSON, _ := json.MarshalIndent(evidence, "", "  ")

	var toolList strings.Builder
	for _, t := range e.registry.List() {
		toolList.WriteString(fmt.Sprintf("- %s: %s (read_only=%v, risk=%s)\n", t.Name(), t.Description(), t.IsReadOnly(), t.RiskLevel()))
	}

	prompt := fmt.Sprintf(`You are an IT diagnostic engine. Given the problem type and collected evidence, produce a diagnosis.

Problem type: %s
Scope: %s

Evidence collected:
%s

Available tools (ONLY use these tool_name values in recommended_actions):
%s

Return a JSON object with:
- "problem_type": string
- "confidence": float between 0.0 and 1.0
- "findings": array of {"source": string, "summary": string, "level": "info"|"warning"|"error"}
- "recommended_actions": array of {"tool_name": string, "description": string, "risk_level": "L0"|"L1"|"L2"|"L3", "params": {}}. IMPORTANT: tool_name MUST be one of the available tools listed above. Do NOT invent tool names.
- "approval_required": boolean (true if any action is L1 or above)
- "next_step": string describing what to do next

You MUST respond with ONLY a JSON object. No explanation, no markdown, no thinking process. Just the raw JSON object starting with { and ending with }.`, plan.ProblemType, plan.Scope, string(evidenceJSON), toolList.String())

	resp, err := e.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a structured IT diagnostic engine. You MUST output ONLY a valid JSON object. No explanation, no markdown fences, no thinking. Just raw JSON."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   2048,
		Temperature: 0.2,
	})
	if err != nil {
		return nil, err
	}

	var result DiagnosisResult
	content := extractJSON(resp.Content)
	slog.Debug("[diagnosis] Reasoning raw response", "content_len", len(resp.Content), "extracted_json", content[:min(len(content), 200)])

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse reasoning response: %w (content: %s)", err, content[:min(len(content), 300)])
	}
	return &result, nil
}

// stepIterativeReasoning performs multi-round reasoning for moderate+ complexity.
// If the initial reasoning confidence is below a threshold, it requests additional evidence
// and re-reasons up to N iterations (determined by complexity level).
func (e *Engine) stepIterativeReasoning(ctx context.Context, plan *DiagnosisPlan, evidence map[string]interface{}, onProgress ProgressFn) (*DiagnosisResult, error) {
	maxIter := MaxIterationsByComplexity(plan.Complexity)
	confidenceThreshold := 0.75

	result, err := e.stepReasoning(ctx, plan, evidence)
	if err != nil {
		return nil, err
	}

	for i := 1; i < maxIter; i++ {
		if result.Confidence >= confidenceThreshold {
			slog.Info("[diagnosis] Iterative reasoning converged", "iteration", i, "confidence", result.Confidence)
			break
		}

		notify(onProgress, "reasoning_iterate", fmt.Sprintf("Iteration %d/%d — confidence %.0f%%, requesting supplementary evidence...", i+1, maxIter, result.Confidence*100))

		supplementary := e.requestSupplementaryEvidence(ctx, plan, result, evidence)
		if len(supplementary) == 0 {
			slog.Info("[diagnosis] No supplementary evidence available, stopping iteration", "iteration", i)
			break
		}

		for k, v := range supplementary {
			evidence[k] = v
		}

		notify(onProgress, "reasoning_iterate", fmt.Sprintf("Iteration %d/%d — re-reasoning with %d total evidence items...", i+1, maxIter, len(evidence)))

		newResult, err := e.stepReasoning(ctx, plan, evidence)
		if err != nil {
			slog.Warn("[diagnosis] Iterative reasoning round failed, keeping previous result", "iteration", i, "error", err)
			break
		}

		if newResult.Confidence > result.Confidence {
			result = newResult
		} else {
			slog.Info("[diagnosis] Iterative reasoning did not improve confidence, stopping", "iteration", i)
			break
		}
	}

	return result, nil
}

// requestSupplementaryEvidence asks the LLM what additional data would help improve confidence.
func (e *Engine) requestSupplementaryEvidence(ctx context.Context, plan *DiagnosisPlan, result *DiagnosisResult, currentEvidence map[string]interface{}) map[string]interface{} {
	if e.llmRouter == nil {
		return nil
	}

	findingsJSON, _ := json.Marshal(result.Findings)

	var availableTools strings.Builder
	for _, t := range e.registry.List() {
		if !t.IsReadOnly() {
			continue
		}
		_, alreadyHave := currentEvidence[t.Name()]
		if !alreadyHave {
			availableTools.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
		}
	}

	if availableTools.Len() == 0 {
		return nil
	}

	prompt := fmt.Sprintf(`Current diagnosis confidence is %.0f%%. The findings so far:
%s

Available unused diagnostic tools:
%s

Which tools should we run to improve diagnostic confidence? Return ONLY a JSON array of tool names. If none would help, return [].`, result.Confidence*100, string(findingsJSON), availableTools.String())

	resp, err := e.llmRouter.Complete(ctx, &router.CompletionRequest{
		Messages: []router.Message{
			{Role: "system", Content: "You are a diagnostic tool selector. Output ONLY a valid JSON array of tool name strings."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.1,
	})
	if err != nil {
		return nil
	}

	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var toolNames []string
	if err := json.Unmarshal([]byte(content), &toolNames); err != nil {
		return nil
	}

	supplementary := make(map[string]interface{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, name := range toolNames {
		t, ok := e.registry.Get(name)
		if !ok || !t.IsReadOnly() {
			continue
		}
		wg.Add(1)
		go func(tool tools.Tool, toolName string) {
			defer wg.Done()
			result, err := tool.Execute(ctx, nil)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				supplementary[toolName] = map[string]interface{}{"error": err.Error()}
			} else {
				supplementary[toolName] = result.Output
			}
		}(t, name)
	}
	wg.Wait()

	return supplementary
}

// evaluateNeedsRemediation sets NeedsRemediation=true if any recommended action
// involves a write operation (non-L0 risk level).
func (e *Engine) evaluateNeedsRemediation(result *DiagnosisResult) {
	for _, action := range result.RecommendedActions {
		if action.RiskLevel != "L0" {
			result.NeedsRemediation = true
			return
		}
		t, ok := e.registry.Get(action.ToolName)
		if ok && !t.IsReadOnly() {
			result.NeedsRemediation = true
			return
		}
	}
}

// Step 5: ActionDraft — validate and enrich recommended actions
func (e *Engine) stepActionDraft(result *DiagnosisResult) {
	var validated []ActionDraft
	for _, action := range result.RecommendedActions {
		t, ok := e.registry.Get(action.ToolName)
		if !ok {
			slog.Warn("[diagnosis] ActionDraft: tool not found, skipping", "tool", action.ToolName)
			continue
		}
		action.RiskLevel = t.RiskLevel()
		if action.RiskLevel != "L0" {
			result.ApprovalRequired = true
		}
		validated = append(validated, action)
	}
	result.RecommendedActions = validated
}

var ipOrHostRe = regexp.MustCompile(`(?:(?:\d{1,3}\.){3}\d{1,3})|(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z]{2,})+)`)

func (e *Engine) heuristicPlan(input string) *DiagnosisPlan {
	lower := strings.ToLower(input)
	plan := &DiagnosisPlan{
		ProblemType: "general",
		Scope:       "local",
		RiskBias:    "conservative",
	}
	switch {
	case strings.Contains(lower, "dns") || strings.Contains(lower, "resolve") || strings.Contains(lower, "域名"):
		plan.ProblemType = "dns"
		plan.Scope = "network"
	case strings.Contains(lower, "proxy") || strings.Contains(lower, "代理") ||
		strings.Contains(lower, "network") || strings.Contains(lower, "网络") ||
		strings.Contains(lower, "connect") || strings.Contains(lower, "连接") ||
		strings.Contains(lower, "ping") || strings.Contains(lower, "延迟") ||
		strings.Contains(lower, "timeout") || strings.Contains(lower, "超时") ||
		strings.Contains(lower, "vpn") || strings.Contains(lower, "防火墙") || strings.Contains(lower, "firewall"):
		plan.ProblemType = "network"
		plan.Scope = "network"
	case strings.Contains(lower, "service") || strings.Contains(lower, "服务") || strings.Contains(lower, "restart") || strings.Contains(lower, "重启"):
		plan.ProblemType = "service"
	case strings.Contains(lower, "slow") || strings.Contains(lower, "慢") || strings.Contains(lower, "performance") ||
		strings.Contains(lower, "cpu") || strings.Contains(lower, "内存") || strings.Contains(lower, "memory") || strings.Contains(lower, "卡"):
		plan.ProblemType = "performance"
	case strings.Contains(lower, "disk") || strings.Contains(lower, "磁盘") || strings.Contains(lower, "space") ||
		strings.Contains(lower, "存储") || strings.Contains(lower, "容量"):
		plan.ProblemType = "disk"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "认证") || strings.Contains(lower, "登录") ||
		strings.Contains(lower, "password") || strings.Contains(lower, "密码") || strings.Contains(lower, "权限"):
		plan.ProblemType = "auth"
	case strings.Contains(lower, "docker") || strings.Contains(lower, "容器") || strings.Contains(lower, "container") ||
		strings.Contains(lower, "compose") || strings.Contains(lower, "镜像") || strings.Contains(lower, "image"):
		plan.ProblemType = "docker"
		plan.Scope = "local"
	case strings.Contains(lower, "k8s") || strings.Contains(lower, "kubernetes") || strings.Contains(lower, "kubectl") ||
		strings.Contains(lower, "pod") || strings.Contains(lower, "deployment") || strings.Contains(lower, "集群") ||
		strings.Contains(lower, "node") || strings.Contains(lower, "namespace") || strings.Contains(lower, "helm"):
		plan.ProblemType = "kubernetes"
		plan.Scope = "cluster"
	case strings.Contains(lower, "mysql") || strings.Contains(lower, "postgres") || strings.Contains(lower, "redis") ||
		strings.Contains(lower, "mongo") || strings.Contains(lower, "数据库") || strings.Contains(lower, "database") ||
		strings.Contains(lower, "db") || strings.Contains(lower, "sql") || strings.Contains(lower, "缓存") ||
		strings.Contains(lower, "mariadb") || strings.Contains(lower, "连接池") || strings.Contains(lower, "connection pool"):
		plan.ProblemType = "database"
		plan.Scope = "local"
	case strings.Contains(lower, "install") || strings.Contains(lower, "安装") || strings.Contains(lower, "setup") ||
		strings.Contains(lower, "卸载") || strings.Contains(lower, "uninstall") || strings.Contains(lower, "依赖") ||
		strings.Contains(lower, "dependency") || strings.Contains(lower, "runtime") || strings.Contains(lower, "运行时") ||
		strings.Contains(lower, "dll") || strings.Contains(lower, "缺少") || strings.Contains(lower, "missing") ||
		strings.Contains(lower, "版本") || strings.Contains(lower, "version") || strings.Contains(lower, "兼容") ||
		strings.Contains(lower, "compatible"):
		plan.ProblemType = "install"
		plan.Scope = "local"
	}

	if host := ipOrHostRe.FindString(input); host != "" {
		plan.ToolParams = map[string]map[string]interface{}{
			"ping_host": {"host": host},
		}
	}

	return plan
}

func (e *Engine) localReasoning(plan *DiagnosisPlan, evidence map[string]interface{}) *DiagnosisResult {
	findings := make([]Finding, 0)

	for source, data := range evidence {
		if dataMap, ok := data.(map[string]interface{}); ok {
			if errMsg, exists := dataMap["error"]; exists {
				findings = append(findings, Finding{Source: source, Summary: fmt.Sprintf("Collection failed: %v", errMsg), Level: "error"})
				continue
			}
		}

		switch source {
		case "read_proxy_config":
			if dataMap, ok := data.(map[string]interface{}); ok {
				findings = append(findings, e.analyzeProxyEvidence(dataMap)...)
			}
		case "read_network_config":
			findings = append(findings, e.analyzeNetworkEvidence(data)...)
		case "read_system_info":
			if dataMap, ok := data.(map[string]interface{}); ok {
				findings = append(findings, e.analyzeSystemEvidence(dataMap)...)
			}
		case "ping_host":
			if dataMap, ok := data.(map[string]interface{}); ok {
				findings = append(findings, e.analyzePingEvidence(dataMap)...)
			}
		default:
			raw, _ := json.MarshalIndent(data, "", "  ")
			findings = append(findings, Finding{Source: source, Summary: string(raw), Level: "info"})
		}
	}

	if len(findings) == 0 {
		findings = append(findings, Finding{Source: "diagnosis", Summary: "No evidence could be collected", Level: "warning"})
	}

	var actions []ActionDraft
	switch plan.ProblemType {
	case "dns":
		actions = append(actions, ActionDraft{
			ToolName:    "dns_flush_cache",
			Description: "Flush DNS cache to resolve stale entries",
			RiskLevel:   "L2",
			Params:      map[string]interface{}{},
		})
	case "service":
		actions = append(actions, ActionDraft{
			ToolName:    "service_restart",
			Description: "Restart the affected service",
			RiskLevel:   "L2",
			Params:      map[string]interface{}{},
		})
	}

	return &DiagnosisResult{
		ProblemType:        plan.ProblemType,
		Confidence:         0.6,
		Findings:           findings,
		RecommendedActions: actions,
		ApprovalRequired:   len(actions) > 0,
		NextStep:           "Review findings and approve recommended actions if appropriate",
	}
}

func (e *Engine) analyzeProxyEvidence(data map[string]interface{}) []Finding {
	var findings []Finding
	hasProxy, _ := data["has_proxy"].(bool)

	if hasProxy {
		findings = append(findings, Finding{Source: "read_proxy_config", Summary: "Proxy is configured on this system", Level: "info"})
		switch envProxies := data["env_proxies"].(type) {
		case map[string]string:
			for k, v := range envProxies {
				findings = append(findings, Finding{Source: "read_proxy_config", Summary: fmt.Sprintf("Environment variable %s = %s", k, v), Level: "info"})
			}
		case map[string]interface{}:
			for k, v := range envProxies {
				findings = append(findings, Finding{Source: "read_proxy_config", Summary: fmt.Sprintf("Environment variable %s = %v", k, v), Level: "info"})
			}
		}
		if goProxy, ok := data["go_http_proxy"].(string); ok && goProxy != "" {
			findings = append(findings, Finding{Source: "read_proxy_config", Summary: fmt.Sprintf("Go HTTP transport proxy: %s", goProxy), Level: "info"})
		}
		if sysProxy, ok := data["system_proxy"].(string); ok && sysProxy != "" {
			findings = append(findings, Finding{Source: "read_proxy_config", Summary: fmt.Sprintf("System proxy: %s", sysProxy), Level: "info"})
		}
	} else {
		findings = append(findings, Finding{Source: "read_proxy_config", Summary: "No proxy detected — no HTTP_PROXY/HTTPS_PROXY environment variables set, no system proxy configured", Level: "info"})
	}
	return findings
}

func (e *Engine) analyzeNetworkEvidence(data interface{}) []Finding {
	var findings []Finding

	var interfaces []interface{}
	switch v := data.(type) {
	case []interface{}:
		interfaces = v
	case []map[string]interface{}:
		for _, m := range v {
			interfaces = append(interfaces, m)
		}
	default:
		raw, _ := json.Marshal(data)
		findings = append(findings, Finding{Source: "read_network_config", Summary: string(raw), Level: "info"})
		return findings
	}

	activeCount := 0
	for _, iface := range interfaces {
		ifMap, ok := iface.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := ifMap["name"].(string)
		flags, _ := ifMap["flags"].(string)

		var ipStrs []string
		switch v := ifMap["ip_addresses"].(type) {
		case []string:
			ipStrs = v
		case []interface{}:
			for _, ip := range v {
				ipStrs = append(ipStrs, fmt.Sprintf("%v", ip))
			}
		}

		if len(ipStrs) == 0 {
			continue
		}

		if strings.Contains(flags, "up") {
			activeCount++
			findings = append(findings, Finding{
				Source:  "read_network_config",
				Summary: fmt.Sprintf("Interface %s (UP): %s", name, strings.Join(ipStrs, ", ")),
				Level:   "info",
			})
		}
	}

	if activeCount == 0 {
		findings = append(findings, Finding{Source: "read_network_config", Summary: "No active network interfaces found", Level: "warning"})
	} else {
		findings = append(findings, Finding{
			Source:  "read_network_config",
			Summary: fmt.Sprintf("Found %d active network interface(s)", activeCount),
			Level:   "info",
		})
	}
	return findings
}

func (e *Engine) analyzeSystemEvidence(data map[string]interface{}) []Finding {
	osName, _ := data["os"].(string)
	arch, _ := data["architecture"].(string)
	hostname, _ := data["hostname"].(string)
	numCPU := 0
	switch n := data["num_cpu"].(type) {
	case int:
		numCPU = n
	case float64:
		numCPU = int(n)
	}

	return []Finding{
		{
			Source:  "read_system_info",
			Summary: fmt.Sprintf("System: %s/%s, Hostname: %s, CPUs: %d", osName, arch, hostname, numCPU),
			Level:   "info",
		},
	}
}

func (e *Engine) analyzePingEvidence(data map[string]interface{}) []Finding {
	host, _ := data["host"].(string)
	port, _ := data["port"].(string)
	reachable, _ := data["reachable"].(bool)

	var latency int64
	switch v := data["latency_ms"].(type) {
	case float64:
		latency = int64(v)
	case int64:
		latency = v
	}

	if reachable {
		return []Finding{{
			Source:  "ping_host",
			Summary: fmt.Sprintf("Host %s:%s is reachable (latency: %dms)", host, port, latency),
			Level:   "info",
		}}
	}

	errMsg, _ := data["error"].(string)
	return []Finding{{
		Source:  "ping_host",
		Summary: fmt.Sprintf("Host %s:%s is NOT reachable (%s)", host, port, errMsg),
		Level:   "warning",
	}}
}

func (e *Engine) analyzeSliceEvidence(source string, data []interface{}) []Finding {
	raw, _ := json.MarshalIndent(data, "", "  ")
	return []Finding{{Source: source, Summary: string(raw), Level: "info"}}
}
