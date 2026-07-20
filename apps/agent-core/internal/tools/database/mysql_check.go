package database

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type MySQLCheckTool struct{}

func NewMySQLCheckTool() *MySQLCheckTool { return &MySQLCheckTool{} }

func (t *MySQLCheckTool) Name() string { return "mysql_check" }
func (t *MySQLCheckTool) Description() string {
	return "Checks MySQL connectivity and basic health. Params: host (default 127.0.0.1), port (default 3306). Only tests TCP connectivity and handshake — no credentials required."
}
func (t *MySQLCheckTool) IsReadOnly() bool  { return true }
func (t *MySQLCheckTool) RiskLevel() string { return "L0" }

func (t *MySQLCheckTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {
				Type:        "string",
				Description: "MySQL host, default 127.0.0.1",
			},
			"port": {
				Type:        "string",
				Description: "MySQL port, default 3306",
			},
		},
	}
}

func (t *MySQLCheckTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		host = "127.0.0.1"
	}
	port := "3306"
	if p, ok := params["port"].(string); ok && p != "" {
		port = p
	} else if pf, ok := params["port"].(float64); ok {
		port = fmt.Sprintf("%d", int(pf))
	}

	start := time.Now()
	addr := net.JoinHostPort(host, port)
	result := map[string]interface{}{
		"host": host,
		"port": port,
	}

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		result["reachable"] = false
		result["error"] = err.Error()
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("MySQL at %s is unreachable: %v", addr, err),
			Output:     result,
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	defer conn.Close()

	result["reachable"] = true
	result["connect_ms"] = elapsed.Milliseconds()

	// Read MySQL handshake greeting (first packet)
	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		result["handshake"] = "failed"
		result["handshake_error"] = err.Error()
	} else {
		greeting := buf[:n]
		result["handshake"] = "ok"
		version := extractMySQLVersion(greeting)
		if version != "" {
			result["server_version"] = version
		}
	}

	summary := fmt.Sprintf("MySQL at %s reachable", addr)
	if v, ok := result["server_version"].(string); ok {
		summary += ", version: " + v
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

// extractMySQLVersion parses the server version from the MySQL initial handshake packet.
// MySQL protocol: 4-byte header, then 1-byte protocol version, then null-terminated version string.
func extractMySQLVersion(data []byte) string {
	if len(data) < 6 {
		return ""
	}
	// Skip 4-byte packet header + 1-byte protocol version
	versionStart := 5
	for i := versionStart; i < len(data); i++ {
		if data[i] == 0 {
			return strings.TrimSpace(string(data[versionStart:i]))
		}
	}
	return ""
}
