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

type RouteTableTool struct{}

func NewRouteTableTool() *RouteTableTool { return &RouteTableTool{} }

func (t *RouteTableTool) Name() string        { return "read_route_table" }
func (t *RouteTableTool) Description() string  { return "Reads the system routing table to show network paths and gateways" }
func (t *RouteTableTool) IsReadOnly() bool     { return true }
func (t *RouteTableTool) RiskLevel() string    { return "L0" }

func (t *RouteTableTool) Parameters() *tools.ParamSchema {
	return tools.NoParams()
}

func (t *RouteTableTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(cmdCtx, "route", "print")
	case "darwin":
		cmd = exec.CommandContext(cmdCtx, "netstat", "-rn")
	default:
		if _, err := exec.LookPath("ip"); err == nil {
			cmd = exec.CommandContext(cmdCtx, "ip", "route", "show")
		} else {
			cmd = exec.CommandContext(cmdCtx, "route", "-n")
		}
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Failed to read route table: %v", err),
			Output:     map[string]interface{}{"error": err.Error(), "raw_output": outputStr},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	lines := strings.Split(outputStr, "\n")
	var routes []map[string]string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		routes = append(routes, map[string]string{"raw": line})
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Read %d route entries", len(routes)),
		Output:     map[string]interface{}{"routes": routes, "route_count": len(routes), "os": runtime.GOOS, "raw_output": outputStr},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
