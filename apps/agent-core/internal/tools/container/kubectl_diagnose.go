package container

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type KubectlDiagnoseTool struct{}

func NewKubectlDiagnoseTool() *KubectlDiagnoseTool { return &KubectlDiagnoseTool{} }

func (t *KubectlDiagnoseTool) Name() string { return "kubectl_diagnose" }
func (t *KubectlDiagnoseTool) Description() string {
	return "Diagnoses Kubernetes cluster issues. Params: action (cluster-info|get-nodes|get-pods|describe-pod|logs|get-events|get-services), namespace, pod (for describe-pod/logs)."
}
func (t *KubectlDiagnoseTool) IsReadOnly() bool  { return true }
func (t *KubectlDiagnoseTool) RiskLevel() string { return "L0" }

func (t *KubectlDiagnoseTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"action": {
				Type:        "string",
				Description: "Kubernetes diagnostic action, default cluster-info",
				Enum:        []string{"cluster-info", "get-nodes", "get-pods", "describe-pod", "logs", "get-events", "get-services"},
			},
			"namespace": {
				Type:        "string",
				Description: "Kubernetes namespace, default 'default'",
			},
			"pod": {
				Type:        "string",
				Description: "Pod name, required for describe-pod and logs actions",
			},
		},
	}
}

func (t *KubectlDiagnoseTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return &tools.ToolResult{
			ToolName: t.Name(), Status: "succeeded",
			Summary: "kubectl is not installed or not in PATH",
			Output:  map[string]interface{}{"installed": false, "error": "kubectl not found"},
		}, nil
	}

	action, _ := params["action"].(string)
	if action == "" {
		action = "cluster-info"
	}
	namespace, _ := params["namespace"].(string)
	if namespace == "" {
		namespace = "default"
	}
	pod, _ := params["pod"].(string)

	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch action {
	case "cluster-info":
		cmd = exec.CommandContext(cmdCtx, "kubectl", "cluster-info")
	case "get-nodes":
		cmd = exec.CommandContext(cmdCtx, "kubectl", "get", "nodes", "-o", "json")
	case "get-pods":
		cmd = exec.CommandContext(cmdCtx, "kubectl", "get", "pods", "-n", namespace, "-o", "json")
	case "describe-pod":
		if pod == "" {
			return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing parameter: pod"}, nil
		}
		cmd = exec.CommandContext(cmdCtx, "kubectl", "describe", "pod", pod, "-n", namespace)
	case "logs":
		if pod == "" {
			return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing parameter: pod"}, nil
		}
		cmd = exec.CommandContext(cmdCtx, "kubectl", "logs", "--tail=100", "--timestamps", pod, "-n", namespace)
	case "get-events":
		cmd = exec.CommandContext(cmdCtx, "kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp", "-o", "json")
	case "get-services":
		cmd = exec.CommandContext(cmdCtx, "kubectl", "get", "svc", "-n", namespace, "-o", "json")
	default:
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed",
			Error: fmt.Sprintf("unknown action: %s (use cluster-info|get-nodes|get-pods|describe-pod|logs|get-events|get-services)", action)}, nil
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("kubectl %s failed: %v", action, err),
			Output:     map[string]interface{}{"action": action, "namespace": namespace, "error": err.Error(), "output": truncate(outputStr, 8192)},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	result := map[string]interface{}{
		"action":    action,
		"namespace": namespace,
		"installed": true,
	}

	// Try to parse JSON for structured actions
	if action == "get-nodes" || action == "get-pods" || action == "get-events" || action == "get-services" {
		var k8sResult map[string]interface{}
		if json.Unmarshal([]byte(outputStr), &k8sResult) == nil {
			if items, ok := k8sResult["items"].([]interface{}); ok {
				result["count"] = len(items)
				// Extract summary for pods
				if action == "get-pods" {
					var podSummaries []map[string]interface{}
					for _, item := range items {
						if m, ok := item.(map[string]interface{}); ok {
							ps := extractPodSummary(m)
							if ps != nil {
								podSummaries = append(podSummaries, ps)
							}
						}
					}
					result["pods"] = podSummaries
				} else {
					result["items"] = k8sResult
				}
			} else {
				result["data"] = k8sResult
			}
		} else {
			result["raw_output"] = truncate(outputStr, 8192)
		}
	} else {
		result["raw_output"] = truncate(outputStr, 8192)
	}

	summary := fmt.Sprintf("kubectl %s completed", action)
	if count, ok := result["count"]; ok {
		summary = fmt.Sprintf("kubectl %s: %v items in namespace %s", action, count, namespace)
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func extractPodSummary(pod map[string]interface{}) map[string]interface{} {
	metadata, _ := pod["metadata"].(map[string]interface{})
	status, _ := pod["status"].(map[string]interface{})
	if metadata == nil || status == nil {
		return nil
	}

	summary := map[string]interface{}{
		"name":   metadata["name"],
		"phase":  status["phase"],
	}

	if conditions, ok := status["conditions"].([]interface{}); ok {
		var condStrs []string
		for _, c := range conditions {
			if cm, ok := c.(map[string]interface{}); ok {
				condStrs = append(condStrs, fmt.Sprintf("%v=%v", cm["type"], cm["status"]))
			}
		}
		summary["conditions"] = condStrs
	}

	if containerStatuses, ok := status["containerStatuses"].([]interface{}); ok {
		var cs []map[string]interface{}
		for _, c := range containerStatuses {
			if cm, ok := c.(map[string]interface{}); ok {
				cs = append(cs, map[string]interface{}{
					"name":          cm["name"],
					"ready":         cm["ready"],
					"restart_count": cm["restartCount"],
				})
			}
		}
		summary["containers"] = cs
	}

	return summary
}
