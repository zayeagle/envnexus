package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type PortScanTool struct{}

func NewPortScanTool() *PortScanTool { return &PortScanTool{} }

func (t *PortScanTool) Name() string        { return "port_scan" }
func (t *PortScanTool) Description() string  { return "Scans common ports on a host to check which are open (TCP connect scan)" }
func (t *PortScanTool) IsReadOnly() bool     { return true }
func (t *PortScanTool) RiskLevel() string    { return "L0" }

func (t *PortScanTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {Type: "string", Description: "Target host to scan common ports"},
		},
		Required: []string{"host"},
	}
}

var commonPorts = []struct {
	Port    int
	Service string
}{
	{22, "SSH"}, {53, "DNS"}, {80, "HTTP"}, {443, "HTTPS"},
	{3306, "MySQL"}, {5432, "PostgreSQL"}, {6379, "Redis"},
	{8080, "HTTP-Alt"}, {8443, "HTTPS-Alt"}, {27017, "MongoDB"},
	{3389, "RDP"}, {5900, "VNC"}, {8081, "HTTP-Alt2"},
}

func (t *PortScanTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: host"}, nil
	}

	start := time.Now()
	timeout := 3 * time.Second

	type portResult struct {
		Port    int    `json:"port"`
		Service string `json:"service"`
		Open    bool   `json:"open"`
	}

	results := make([]portResult, len(commonPorts))
	var wg sync.WaitGroup

	for i, p := range commonPorts {
		wg.Add(1)
		go func(idx int, port int, svc string) {
			defer wg.Done()
			addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
			conn, err := net.DialTimeout("tcp", addr, timeout)
			results[idx] = portResult{Port: port, Service: svc, Open: err == nil}
			if conn != nil {
				conn.Close()
			}
		}(i, p.Port, p.Service)
	}
	wg.Wait()

	elapsed := time.Since(start)
	var openPorts []portResult
	for _, r := range results {
		if r.Open {
			openPorts = append(openPorts, r)
		}
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Scanned %d ports on %s: %d open", len(commonPorts), host, len(openPorts)),
		Output: map[string]interface{}{
			"host":        host,
			"scanned":     len(commonPorts),
			"open_count":  len(openPorts),
			"open_ports":  openPorts,
			"all_results": results,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
