package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type DNSLookupTool struct{}

func NewDNSLookupTool() *DNSLookupTool { return &DNSLookupTool{} }

func (t *DNSLookupTool) Name() string        { return "dns_lookup" }
func (t *DNSLookupTool) Description() string  { return "Resolves a domain name to IP addresses via DNS" }
func (t *DNSLookupTool) IsReadOnly() bool     { return true }
func (t *DNSLookupTool) RiskLevel() string    { return "L0" }

func (t *DNSLookupTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {Type: "string", Description: "Domain name or hostname to resolve"},
		},
		Required: []string{"host"},
	}
}

func (t *DNSLookupTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: host"}, nil
	}

	start := time.Now()
	resolver := net.DefaultResolver

	ips, err := resolver.LookupHost(ctx, host)
	elapsed := time.Since(start)

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("DNS lookup failed for %s: %v", host, err),
			Output:     map[string]interface{}{"host": host, "resolved": false, "error": err.Error(), "latency_ms": elapsed.Milliseconds()},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	cname, _ := resolver.LookupCNAME(ctx, host)

	var mxRecords []string
	mxs, _ := resolver.LookupMX(ctx, host)
	for _, mx := range mxs {
		mxRecords = append(mxRecords, fmt.Sprintf("%s (priority %d)", mx.Host, mx.Pref))
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Resolved %s to %d IP(s) in %dms", host, len(ips), elapsed.Milliseconds()),
		Output: map[string]interface{}{
			"host":       host,
			"resolved":   true,
			"ips":        ips,
			"cname":      cname,
			"mx_records": mxRecords,
			"latency_ms": elapsed.Milliseconds(),
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
