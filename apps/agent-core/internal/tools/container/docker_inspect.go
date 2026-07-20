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

type DockerInspectTool struct{}

func NewDockerInspectTool() *DockerInspectTool { return &DockerInspectTool{} }

func (t *DockerInspectTool) Name() string { return "docker_inspect" }
func (t *DockerInspectTool) Description() string {
	return "Inspects Docker daemon status, lists containers, checks logs. Params: action (status|ps|logs|images|networks|volumes), container (for logs)."
}
func (t *DockerInspectTool) IsReadOnly() bool  { return true }
func (t *DockerInspectTool) RiskLevel() string { return "L0" }

func (t *DockerInspectTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"action": {
				Type:        "string",
				Description: "Docker action to perform, default status",
				Enum:        []string{"status", "ps", "logs", "images", "networks", "volumes"},
			},
			"container": {
				Type:        "string",
				Description: "Container name or ID, required for logs action",
			},
		},
	}
}

func (t *DockerInspectTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return &tools.ToolResult{
			ToolName: t.Name(), Status: "succeeded",
			Summary: "Docker is not installed or not in PATH",
			Output:  map[string]interface{}{"installed": false, "error": "docker not found"},
		}, nil
	}

	action, _ := params["action"].(string)
	if action == "" {
		action = "status"
	}

	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch action {
	case "status":
		cmd = exec.CommandContext(cmdCtx, "docker", "info", "--format", "json")
	case "ps":
		cmd = exec.CommandContext(cmdCtx, "docker", "ps", "-a", "--format", "json")
	case "logs":
		container, _ := params["container"].(string)
		if container == "" {
			return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing parameter: container (for logs action)"}, nil
		}
		cmd = exec.CommandContext(cmdCtx, "docker", "logs", "--tail", "100", "--timestamps", container)
	case "images":
		cmd = exec.CommandContext(cmdCtx, "docker", "images", "--format", "json")
	case "networks":
		cmd = exec.CommandContext(cmdCtx, "docker", "network", "ls", "--format", "json")
	case "volumes":
		cmd = exec.CommandContext(cmdCtx, "docker", "volume", "ls", "--format", "json")
	default:
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: fmt.Sprintf("unknown action: %s (use status|ps|logs|images|networks|volumes)", action)}, nil
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("docker %s failed: %v", action, err),
			Output:     map[string]interface{}{"action": action, "error": err.Error(), "output": truncate(outputStr, 8192)},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	result := map[string]interface{}{
		"action":    action,
		"installed": true,
	}

	// Try to parse JSON output for structured data
	if action == "status" {
		var info map[string]interface{}
		if json.Unmarshal([]byte(outputStr), &info) == nil {
			result["info"] = info
		} else {
			result["raw_output"] = truncate(outputStr, 8192)
		}
	} else if action == "ps" || action == "images" || action == "networks" || action == "volumes" {
		var items []map[string]interface{}
		for _, line := range strings.Split(outputStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var item map[string]interface{}
			if json.Unmarshal([]byte(line), &item) == nil {
				items = append(items, item)
			}
		}
		if items != nil {
			result["items"] = items
			result["count"] = len(items)
		} else {
			result["raw_output"] = truncate(outputStr, 8192)
		}
	} else {
		result["raw_output"] = truncate(outputStr, 8192)
	}

	summary := fmt.Sprintf("docker %s completed", action)
	if count, ok := result["count"]; ok {
		summary = fmt.Sprintf("docker %s: %v items", action, count)
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "\n... (truncated)"
	}
	return s
}
