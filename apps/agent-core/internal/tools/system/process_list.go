package system

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadProcessListTool struct{}

func NewReadProcessListTool() *ReadProcessListTool {
	return &ReadProcessListTool{}
}

func (t *ReadProcessListTool) Name() string        { return "read_process_list" }
func (t *ReadProcessListTool) Description() string  { return "Read list of running processes with basic info" }
func (t *ReadProcessListTool) IsReadOnly() bool      { return true }
func (t *ReadProcessListTool) RiskLevel() string     { return "L0" }

func (t *ReadProcessListTool) Parameters() *tools.ParamSchema {
	return tools.NoParams()
}

func (t *ReadProcessListTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	type ProcessInfo struct {
		PID  int    `json:"pid"`
		Name string `json:"name"`
	}

	var processes []ProcessInfo

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		entries, err := os.ReadDir("/proc")
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				pid, err := strconv.Atoi(entry.Name())
				if err != nil {
					continue
				}
				cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
				if err != nil {
					continue
				}
				name := strings.TrimSpace(string(cmdline))
				processes = append(processes, ProcessInfo{PID: pid, Name: name})
			}
		}
	}

	// Fallback: at minimum report the current process
	if len(processes) == 0 {
		processes = append(processes, ProcessInfo{
			PID:  os.Getpid(),
			Name: "enx-agent (self)",
		})
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "success",
		Summary:    fmt.Sprintf("Found %d processes", len(processes)),
		Output:     map[string]interface{}{"processes": processes, "count": len(processes), "os": runtime.GOOS},
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
