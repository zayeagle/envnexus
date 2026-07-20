package system

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadSystemInfoTool struct{}

func NewReadSystemInfoTool() *ReadSystemInfoTool {
	return &ReadSystemInfoTool{}
}

func (t *ReadSystemInfoTool) Name() string {
	return "read_system_info"
}

func (t *ReadSystemInfoTool) Description() string {
	return "Reads basic system information like OS, architecture, and hostname."
}

func (t *ReadSystemInfoTool) IsReadOnly() bool {
	return true
}

func (t *ReadSystemInfoTool) RiskLevel() string {
	return "L0"
}

func (t *ReadSystemInfoTool) Parameters() *tools.ParamSchema {
	return tools.NoParams()
}

func (t *ReadSystemInfoTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	result := map[string]interface{}{
		"os":           runtime.GOOS,
		"architecture": runtime.GOARCH,
		"num_cpu":      runtime.NumCPU(),
		"hostname":     hostname,
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    "Successfully read system info",
		Output:     result,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
