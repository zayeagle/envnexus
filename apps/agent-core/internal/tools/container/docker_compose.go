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

type DockerComposeTool struct{}

func NewDockerComposeTool() *DockerComposeTool { return &DockerComposeTool{} }

func (t *DockerComposeTool) Name() string { return "docker_compose_check" }
func (t *DockerComposeTool) Description() string {
	return "Checks Docker Compose project status. Params: action (ps|logs|config), project_dir (working directory), service (for logs)."
}
func (t *DockerComposeTool) IsReadOnly() bool  { return true }
func (t *DockerComposeTool) RiskLevel() string { return "L0" }

func (t *DockerComposeTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"action": {
				Type:        "string",
				Description: "Compose action, default ps",
				Enum:        []string{"ps", "logs", "config"},
			},
			"project_dir": {
				Type:        "string",
				Description: "Docker Compose project directory",
			},
			"service": {
				Type:        "string",
				Description: "Service name for logs action",
			},
		},
	}
}

func (t *DockerComposeTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	// Try both "docker compose" (v2) and "docker-compose" (v1)
	composeBin := findComposeBin()
	if composeBin == "" {
		return &tools.ToolResult{
			ToolName: t.Name(), Status: "succeeded",
			Summary: "Docker Compose is not installed",
			Output:  map[string]interface{}{"installed": false, "error": "neither 'docker compose' nor 'docker-compose' found"},
		}, nil
	}

	action, _ := params["action"].(string)
	if action == "" {
		action = "ps"
	}
	projectDir, _ := params["project_dir"].(string)
	service, _ := params["service"].(string)

	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var args []string
	switch composeBin {
	case "docker":
		args = append(args, "compose")
	}

	switch action {
	case "ps":
		args = append(args, "ps", "--format", "json", "-a")
	case "logs":
		logArgs := []string{"logs", "--tail", "80", "--timestamps"}
		if service != "" {
			logArgs = append(logArgs, service)
		}
		args = append(args, logArgs...)
	case "config":
		args = append(args, "config")
	default:
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed",
			Error: fmt.Sprintf("unknown action: %s (use ps|logs|config)", action)}, nil
	}

	cmd := exec.CommandContext(cmdCtx, composeBin, args...)
	if projectDir != "" {
		cmd.Dir = projectDir
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("docker compose %s failed: %v", action, err),
			Output:     map[string]interface{}{"action": action, "error": err.Error(), "output": truncate(outputStr, 8192)},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	result := map[string]interface{}{
		"action":    action,
		"installed": true,
	}

	if action == "ps" {
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
			result["services"] = items
			result["count"] = len(items)
		} else {
			result["raw_output"] = truncate(outputStr, 8192)
		}
	} else {
		result["raw_output"] = truncate(outputStr, 8192)
	}

	summary := fmt.Sprintf("docker compose %s completed", action)
	if count, ok := result["count"]; ok {
		summary = fmt.Sprintf("docker compose %s: %v services", action, count)
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func findComposeBin() string {
	// Check docker compose (v2 plugin)
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err == nil {
		return "docker"
	}
	// Check docker-compose (v1 standalone)
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return "docker-compose"
	}
	return ""
}
