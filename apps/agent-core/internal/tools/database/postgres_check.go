package database

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type PostgresCheckTool struct{}

func NewPostgresCheckTool() *PostgresCheckTool { return &PostgresCheckTool{} }

func (t *PostgresCheckTool) Name() string { return "postgres_check" }
func (t *PostgresCheckTool) Description() string {
	return "Checks PostgreSQL connectivity. Params: host (default 127.0.0.1), port (default 5432). Tests TCP connectivity and attempts protocol handshake — no credentials required."
}
func (t *PostgresCheckTool) IsReadOnly() bool  { return true }
func (t *PostgresCheckTool) RiskLevel() string { return "L0" }

func (t *PostgresCheckTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {
				Type:        "string",
				Description: "PostgreSQL host, default 127.0.0.1",
			},
			"port": {
				Type:        "string",
				Description: "PostgreSQL port, default 5432",
			},
		},
	}
}

func (t *PostgresCheckTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		host = "127.0.0.1"
	}
	port := "5432"
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
			Summary:    fmt.Sprintf("PostgreSQL at %s is unreachable: %v", addr, err),
			Output:     result,
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	defer conn.Close()

	result["reachable"] = true
	result["connect_ms"] = elapsed.Milliseconds()

	// Send a PostgreSQL SSLRequest to probe the server.
	// SSLRequest: 8-byte message: length (8) + code (80877103)
	sslRequest := []byte{0, 0, 0, 8, 4, 210, 22, 47}
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err = conn.Write(sslRequest)
	if err != nil {
		result["protocol_check"] = "write_failed"
		result["protocol_error"] = err.Error()
	} else {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		resp := make([]byte, 1)
		n, err := conn.Read(resp)
		if err != nil {
			result["protocol_check"] = "read_failed"
			result["protocol_error"] = err.Error()
		} else if n > 0 {
			result["protocol_check"] = "ok"
			switch resp[0] {
			case 'S':
				result["ssl_supported"] = true
			case 'N':
				result["ssl_supported"] = false
			default:
				result["ssl_response"] = fmt.Sprintf("0x%02x", resp[0])
			}
		}
	}

	summary := fmt.Sprintf("PostgreSQL at %s reachable (connect: %dms)", addr, elapsed.Milliseconds())

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
