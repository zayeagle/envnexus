package system

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadEventLogTool struct{}

func NewReadEventLogTool() *ReadEventLogTool { return &ReadEventLogTool{} }

func (t *ReadEventLogTool) Name() string        { return "read_event_log" }
func (t *ReadEventLogTool) Description() string  { return "Reads recent system event logs (errors/warnings). Windows EventLog or Linux journalctl." }
func (t *ReadEventLogTool) IsReadOnly() bool     { return true }
func (t *ReadEventLogTool) RiskLevel() string    { return "L0" }

func (t *ReadEventLogTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"source": {
				Type:        "string",
				Description: "Log source or unit name",
			},
			"count": {
				Type:        "number",
				Description: "Maximum number of log entries, default 30, max 100",
			},
		},
	}
}

func (t *ReadEventLogTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	source, _ := params["source"].(string)
	count := 30
	if n, ok := params["count"].(float64); ok && n > 0 {
		count = int(n)
		if count > 100 {
			count = 100
		}
	}

	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		logName := "Application"
		if source != "" {
			logName = source
		}
		// Get recent Error and Warning events
		ps := fmt.Sprintf(
			`Get-WinEvent -LogName '%s' -MaxEvents %d -ErrorAction SilentlyContinue | Where-Object { $_.LevelDisplayName -in 'Error','Warning','Critical' } | Select-Object TimeCreated,LevelDisplayName,ProviderName,Message | Format-List`,
			logName, count*3) // fetch more to filter
		cmd = exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)

	case "linux":
		args := []string{"--no-pager", "-p", "err..crit", "-n", fmt.Sprintf("%d", count), "--output", "short-iso"}
		if source != "" {
			args = append(args, "-u", source)
		}
		cmd = exec.CommandContext(cmdCtx, "journalctl", args...)

	case "darwin":
		args := []string{"show", "--predicate", "messageType == error || messageType == fault", "--last", fmt.Sprintf("%dm", 60), "--style", "compact"}
		if source != "" {
			args = []string{"show", "--predicate", fmt.Sprintf("subsystem == '%s'", source), "--last", "60m", "--style", "compact"}
		}
		cmd = exec.CommandContext(cmdCtx, "log", args...)

	default:
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Unsupported OS: %s", runtime.GOOS),
			Output:     map[string]interface{}{"error": fmt.Sprintf("unsupported OS: %s", runtime.GOOS)},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	const maxOutput = 16384
	truncated := false
	if len(outputStr) > maxOutput {
		outputStr = outputStr[:maxOutput] + "\n... (output truncated)"
		truncated = true
	}

	if err != nil && outputStr == "" {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Failed to read event log: %v", err),
			Output:     map[string]interface{}{"source": source, "error": err.Error(), "os": runtime.GOOS},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	lineCount := len(strings.Split(outputStr, "\n"))

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Read %d lines from event log", lineCount),
		Output: map[string]interface{}{
			"source":     source,
			"output":     outputStr,
			"line_count": lineCount,
			"truncated":  truncated,
			"os":         runtime.GOOS,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
