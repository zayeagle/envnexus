package service

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ContainerReloadTool struct{}

func NewContainerReloadTool() *ContainerReloadTool { return &ContainerReloadTool{} }

func (t *ContainerReloadTool) Name() string     { return "container_reload" }
func (t *ContainerReloadTool) IsReadOnly() bool  { return false }
func (t *ContainerReloadTool) RiskLevel() string { return "L2" }

func (t *ContainerReloadTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"target": {
				Type:        "string",
				Description: "Container name/ID or process name to reload",
			},
			"mode": {
				Type:        "string",
				Description: "Reload mode, default docker",
				Enum:        []string{"docker", "process", "systemd"},
			},
		},
		Required: []string{"target"},
	}
}

func (t *ContainerReloadTool) Description() string {
	return "Reload a Docker container or send SIGHUP to a process to apply config changes. " +
		"Params: target (container name/id or process name), mode (docker|process|systemd). Default mode: docker."
}

func (t *ContainerReloadTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	target, _ := params["target"].(string)
	mode, _ := params["mode"].(string)

	if target == "" {
		return nil, fmt.Errorf("target is required")
	}
	if mode == "" {
		mode = "docker"
	}

	switch mode {
	case "docker":
		return t.reloadDocker(ctx, target)
	case "process":
		return t.reloadProcess(ctx, target)
	case "systemd":
		return t.reloadSystemd(ctx, target)
	default:
		return nil, fmt.Errorf("unknown mode %q, must be docker|process|systemd", mode)
	}
}

func (t *ContainerReloadTool) reloadDocker(ctx context.Context, name string) (*tools.ToolResult, error) {
	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		return &tools.ToolResult{
			Status: "error",
			Output:  "docker not found in PATH; cannot reload container",
		}, nil
	}

	// Send SIGHUP (soft reload) first, fallback to restart if container doesn't handle it
	out, err := exec.CommandContext(ctx, "docker", "kill", "--signal=SIGHUP", name).CombinedOutput()
	if err != nil {
		// Try restart
		out2, err2 := exec.CommandContext(ctx, "docker", "restart", name).CombinedOutput()
		if err2 != nil {
			return nil, fmt.Errorf("docker reload failed (SIGHUP: %v; restart: %v; output: %s)", err, err2, string(out2))
		}
		return &tools.ToolResult{
			Status: "ok",
			Output:  fmt.Sprintf("Container %q restarted (SIGHUP not handled). Output: %s", name, strings.TrimSpace(string(out2))),
		}, nil
	}

	return &tools.ToolResult{
		Status: "ok",
		Output:  fmt.Sprintf("Sent SIGHUP to container %q. Output: %s", name, strings.TrimSpace(string(out))),
	}, nil
}

func (t *ContainerReloadTool) reloadProcess(ctx context.Context, name string) (*tools.ToolResult, error) {
	if runtime.GOOS == "windows" {
		return &tools.ToolResult{
			Status: "error",
			Output:  "process SIGHUP reload not supported on Windows",
		}, nil
	}
	// Use pkill -HUP to send reload signal
	out, err := exec.CommandContext(ctx, "pkill", "-HUP", "-f", name).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pkill -HUP %q: %v (output: %s)", name, err, string(out))
	}
	return &tools.ToolResult{
		Status: "ok",
		Output:  fmt.Sprintf("Sent SIGHUP to process(es) matching %q.", name),
	}, nil
}

func (t *ContainerReloadTool) reloadSystemd(ctx context.Context, name string) (*tools.ToolResult, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return &tools.ToolResult{
			Status: "error",
			Output:  "systemctl not found; not a systemd-managed system",
		}, nil
	}
	out, err := exec.CommandContext(ctx, "systemctl", "reload-or-restart", name).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("systemctl reload-or-restart %q: %v (output: %s)", name, err, string(out))
	}
	return &tools.ToolResult{
		Status: "ok",
		Output:  fmt.Sprintf("systemd service %q reloaded. Output: %s", name, strings.TrimSpace(string(out))),
	}, nil
}

