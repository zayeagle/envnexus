package service

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type RestartTool struct{}

func NewRestartTool() *RestartTool { return &RestartTool{} }

func (t *RestartTool) Name() string        { return "service_restart" }
func (t *RestartTool) Description() string  { return "Restarts a specified system service" }
func (t *RestartTool) IsReadOnly() bool     { return false }
func (t *RestartTool) RiskLevel() string    { return "L2" }

func (t *RestartTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"service_name": {
				Type:        "string",
				Description: "Name of the system service to restart",
			},
		},
		Required: []string{"service_name"},
	}
}

func (t *RestartTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	serviceName, _ := params["service_name"].(string)
	if serviceName == "" {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    "Missing required parameter: service_name",
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("missing service_name parameter")
	}

	if !isAllowedService(serviceName) {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Service %q is not in the allowed restart list", serviceName),
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("service %q not allowed", serviceName)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Restart-Service -Name '%s' -Force", serviceName))
	case "linux":
		cmd = exec.CommandContext(ctx, "systemctl", "restart", serviceName)
	case "darwin":
		cmd = exec.CommandContext(ctx, "launchctl", "kickstart", "-k",
			fmt.Sprintf("system/%s", serviceName))
	default:
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Unsupported OS: %s", runtime.GOOS),
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Failed to restart service %s: %v", serviceName, err),
			Output:     map[string]string{"output": outputStr, "error": err.Error()},
			DurationMs: duration,
		}, nil
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Successfully restarted service %s", serviceName),
		Output:     map[string]string{"service": serviceName, "output": outputStr},
		DurationMs: duration,
	}, nil
}

var allowedServices = map[string]bool{
	"wuauserv":          true,
	"spooler":           true,
	"w32time":           true,
	"dnscache":          true,
	"WinHttpAutoProxySvc": true,
	"systemd-resolved":  true,
	"NetworkManager":    true,
	"nscd":              true,
	"dnsmasq":           true,
	"nginx":             true,
	"apache2":           true,
	"httpd":             true,
	"docker":            true,
}

func isAllowedService(name string) bool {
	return allowedServices[name]
}
