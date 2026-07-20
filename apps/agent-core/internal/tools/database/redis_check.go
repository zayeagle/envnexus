package database

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type RedisCheckTool struct{}

func NewRedisCheckTool() *RedisCheckTool { return &RedisCheckTool{} }

func (t *RedisCheckTool) Name() string { return "redis_check" }
func (t *RedisCheckTool) Description() string {
	return "Checks Redis connectivity and basic info. Params: host (default 127.0.0.1), port (default 6379). Sends PING and INFO commands — no auth required for basic check."
}
func (t *RedisCheckTool) IsReadOnly() bool  { return true }
func (t *RedisCheckTool) RiskLevel() string { return "L0" }

func (t *RedisCheckTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {
				Type:        "string",
				Description: "Redis host, default 127.0.0.1",
			},
			"port": {
				Type:        "string",
				Description: "Redis port, default 6379",
			},
		},
	}
}

func (t *RedisCheckTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		host = "127.0.0.1"
	}
	port := "6379"
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
			Summary:    fmt.Sprintf("Redis at %s is unreachable: %v", addr, err),
			Output:     result,
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	defer conn.Close()

	result["reachable"] = true
	result["connect_ms"] = elapsed.Milliseconds()

	reader := bufio.NewReader(conn)

	// Send PING
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(conn, "*1\r\n$4\r\nPING\r\n")

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	pingResp, err := reader.ReadString('\n')
	if err != nil {
		result["ping"] = "failed"
		result["ping_error"] = err.Error()
	} else {
		pingResp = strings.TrimSpace(pingResp)
		if pingResp == "+PONG" {
			result["ping"] = "PONG"
			result["auth_required"] = false
		} else if strings.Contains(pingResp, "NOAUTH") {
			result["ping"] = "NOAUTH"
			result["auth_required"] = true
		} else {
			result["ping"] = pingResp
		}
	}

	// If no auth required, try INFO server
	if result["ping"] == "PONG" {
		conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprintf(conn, "*2\r\n$4\r\nINFO\r\n$6\r\nserver\r\n")

		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		info := readRedisInfo(reader)
		if info != nil {
			result["server_info"] = info
		}
	}

	summary := fmt.Sprintf("Redis at %s reachable, ping=%v", addr, result["ping"])
	if info, ok := result["server_info"].(map[string]string); ok {
		if v, ok := info["redis_version"]; ok {
			summary += ", version: " + v
		}
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func readRedisInfo(reader *bufio.Reader) map[string]string {
	// First line is the bulk string length: $NNN
	firstLine, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(firstLine, "$") {
		return nil
	}

	info := make(map[string]string)
	interestingKeys := map[string]bool{
		"redis_version": true, "redis_mode": true, "os": true,
		"uptime_in_seconds": true, "connected_clients": true,
		"used_memory_human": true, "role": true, "tcp_port": true,
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && interestingKeys[parts[0]] {
			info[parts[0]] = parts[1]
		}
	}

	if len(info) == 0 {
		return nil
	}
	return info
}
