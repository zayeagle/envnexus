package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type TLSCheckTool struct{}

func NewTLSCheckTool() *TLSCheckTool { return &TLSCheckTool{} }

func (t *TLSCheckTool) Name() string        { return "check_tls_cert" }
func (t *TLSCheckTool) Description() string  { return "Checks TLS/SSL certificate validity, expiry, and chain for a host" }
func (t *TLSCheckTool) IsReadOnly() bool     { return true }
func (t *TLSCheckTool) RiskLevel() string    { return "L0" }

func (t *TLSCheckTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"host": {Type: "string", Description: "Host to check TLS certificate"},
			"port": {Type: "string", Description: "TLS port, default 443"},
		},
		Required: []string{"host"},
	}
}

func (t *TLSCheckTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	host, _ := params["host"].(string)
	if host == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: host"}, nil
	}

	port := "443"
	if p, ok := params["port"].(string); ok && p != "" {
		port = p
	} else if p, ok := params["port"].(float64); ok {
		port = fmt.Sprintf("%d", int(p))
	}

	start := time.Now()
	addr := net.JoinHostPort(host, port)

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		ServerName: host,
	})
	elapsed := time.Since(start)

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("TLS connection to %s failed: %v", addr, err),
			Output:     map[string]interface{}{"host": host, "port": port, "valid": false, "error": err.Error(), "latency_ms": elapsed.Milliseconds()},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("No certificates returned by %s", addr),
			Output:     map[string]interface{}{"host": host, "port": port, "valid": false, "error": "no peer certificates"},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	cert := state.PeerCertificates[0]
	now := time.Now()
	daysUntilExpiry := int(cert.NotAfter.Sub(now).Hours() / 24)
	expired := now.After(cert.NotAfter)

	var chain []map[string]string
	for _, c := range state.PeerCertificates {
		chain = append(chain, map[string]string{
			"subject": c.Subject.CommonName,
			"issuer":  c.Issuer.CommonName,
		})
	}

	level := "info"
	if expired {
		level = "error"
	} else if daysUntilExpiry < 30 {
		level = "warning"
	}

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("TLS cert for %s: %s, expires in %d days", host, cert.Subject.CommonName, daysUntilExpiry),
		Output: map[string]interface{}{
			"host":              host,
			"port":              port,
			"valid":             !expired,
			"subject":           cert.Subject.CommonName,
			"issuer":            cert.Issuer.CommonName,
			"dns_names":         cert.DNSNames,
			"not_before":        cert.NotBefore.Format(time.RFC3339),
			"not_after":         cert.NotAfter.Format(time.RFC3339),
			"days_until_expiry": daysUntilExpiry,
			"expired":           expired,
			"tls_version":       tlsVersionName(state.Version),
			"cipher_suite":      tls.CipherSuiteName(state.CipherSuite),
			"chain":             chain,
			"level":             level,
			"latency_ms":        elapsed.Milliseconds(),
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", v)
	}
}
