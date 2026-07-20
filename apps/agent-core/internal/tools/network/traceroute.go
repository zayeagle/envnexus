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

type TracerouteTool struct{}

func NewTracerouteTool() *TracerouteTool { return &TracerouteTool{} }

func (t *TracerouteTool) Name() string        { return "traceroute" }
func (t *TracerouteTool) Description() string  { return "Traces the network path to a destination host showing each hop" }
func (t *TracerouteTool) IsReadOnly() bool     { return true }
func (t *TracerouteTool) RiskLevel() string    { return "L0" }

func (t *TracerouteTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {Type: "string", Description: "Destination host to trace route to"},
		},
		Required: []string{"host"},
	}
}

func (t *TracerouteTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: host"}, nil
	}

	start := time.Now()
	traceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(traceCtx, "tracert", "-d", "-w", "2000", "-h", "15", host)
	default:
		bin := "traceroute"
		if _, err := exec.LookPath("traceroute"); err != nil {
			bin = "tracepath"
		}
		cmd = exec.CommandContext(traceCtx, bin, "-n", "-m", "15", "-w", "2", host)
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	lines := strings.Split(outputStr, "\n")
	var hops []map[string]interface{}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || i == 0 {
			continue
		}
		hops = append(hops, map[string]interface{}{"hop": i, "raw": line})
	}

	if err != nil && len(hops) == 0 {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Traceroute to %s failed: %v", host, err),
			Output:     map[string]interface{}{"host": host, "error": err.Error(), "raw_output": outputStr},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Traced %d hops to %s in %dms", len(hops), host, elapsed.Milliseconds()),
		Output:     map[string]interface{}{"host": host, "hops": hops, "total_hops": len(hops), "raw_output": outputStr},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
