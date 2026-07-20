package network

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type FlushDNSTool struct{}

func NewFlushDNSTool() *FlushDNSTool {
	return &FlushDNSTool{}
}

func (t *FlushDNSTool) Name() string        { return "dns_flush_cache" }
func (t *FlushDNSTool) Description() string  { return "Flushes the local DNS resolver cache" }
func (t *FlushDNSTool) IsReadOnly() bool     { return false }
func (t *FlushDNSTool) RiskLevel() string    { return "L2" }

func (t *FlushDNSTool) Parameters() *tools.ParamSchema {
	return tools.NoParams()
}

func (t *FlushDNSTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "ipconfig", "/flushdns")
	case "linux":
		cmd = exec.CommandContext(ctx, "systemd-resolve", "--flush-caches")
		if _, err := exec.LookPath("systemd-resolve"); err != nil {
			cmd = exec.CommandContext(ctx, "systemctl", "restart", "systemd-resolved")
		}
	case "darwin":
		cmd = exec.CommandContext(ctx, "dscacheutil", "-flushcache")
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
			Summary:    fmt.Sprintf("DNS flush failed: %v", err),
			Output:     map[string]string{"output": outputStr, "error": err.Error()},
			Error:      err.Error(),
			DurationMs: duration,
		}, nil
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    "Successfully flushed DNS cache",
		Output:     map[string]string{"output": outputStr, "action": "dns_flushed"},
		DurationMs: duration,
	}, nil
}
