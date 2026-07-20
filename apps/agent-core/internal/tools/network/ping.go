package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type PingTool struct{}

func NewPingTool() *PingTool {
	return &PingTool{}
}

func (t *PingTool) Name() string        { return "ping_host" }
func (t *PingTool) Description() string  { return "Tests TCP connectivity to a host:port (default port 80). Read-only network probe." }
func (t *PingTool) IsReadOnly() bool     { return true }
func (t *PingTool) RiskLevel() string    { return "L0" }

func (t *PingTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {Type: "string", Description: "Target host or IP address"},
			"port": {Type: "string", Description: "TCP port to test connectivity, default 80"},
		},
		Required: []string{"host"},
	}
}

func (t *PingTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		return &tools.ToolResult{
			ToolName: t.Name(),
			Status:   "failed",
			Error:    "missing required parameter: host",
		}, nil
	}

	port := "80"
	if p, ok := params["port"].(string); ok && p != "" {
		port = p
	} else if p, ok := params["port"].(float64); ok {
		port = fmt.Sprintf("%d", int(p))
	}

	timeout := 5 * time.Second
	start := time.Now()
	addr := net.JoinHostPort(host, port)

	conn, err := net.DialTimeout("tcp", addr, timeout)
	elapsed := time.Since(start)

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Host %s is NOT reachable on port %s", host, port),
			Output: map[string]interface{}{
				"host":        host,
				"port":        port,
				"reachable":   false,
				"error":       err.Error(),
				"latency_ms":  elapsed.Milliseconds(),
			},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	conn.Close()

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Host %s is reachable on port %s (%dms)", host, port, elapsed.Milliseconds()),
		Output: map[string]interface{}{
			"host":       host,
			"port":       port,
			"reachable":  true,
			"latency_ms": elapsed.Milliseconds(),
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
