package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type HTTPCheckTool struct{}

func NewHTTPCheckTool() *HTTPCheckTool { return &HTTPCheckTool{} }

func (t *HTTPCheckTool) Name() string        { return "http_check" }
func (t *HTTPCheckTool) Description() string  { return "Tests HTTP/HTTPS endpoint availability, returns status code, headers, and response time" }
func (t *HTTPCheckTool) IsReadOnly() bool     { return true }
func (t *HTTPCheckTool) RiskLevel() string    { return "L0" }

func (t *HTTPCheckTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"url":    {Type: "string", Description: "HTTP/HTTPS endpoint URL to check"},
			"method": {Type: "string", Description: "HTTP method, default GET"},
		},
		Required: []string{"url"},
	}
}

func (t *HTTPCheckTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	url, _ := params["url"].(string)
	if url == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: url"}, nil
	}

	start := time.Now()
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects (5)")
			}
			return nil
		},
	}

	method := "GET"
	if m, ok := params["method"].(string); ok && m != "" {
		method = m
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Invalid request for %s: %v", url, err),
			Output:     map[string]interface{}{"url": url, "reachable": false, "error": err.Error()},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	req.Header.Set("User-Agent", "envnexus-agent/1.0")

	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("HTTP request to %s failed: %v", url, err),
			Output:     map[string]interface{}{"url": url, "reachable": false, "error": err.Error(), "latency_ms": elapsed.Milliseconds()},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	headers := make(map[string]string)
	for _, key := range []string{"Content-Type", "Server", "X-Powered-By", "Location", "Cache-Control"} {
		if v := resp.Header.Get(key); v != "" {
			headers[key] = v
		}
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("HTTP %s %s → %d (%dms)", method, url, resp.StatusCode, elapsed.Milliseconds()),
		Output: map[string]interface{}{
			"url":          url,
			"reachable":    true,
			"status_code":  resp.StatusCode,
			"status":       resp.Status,
			"headers":      headers,
			"body_preview": string(bodyBytes),
			"tls":          resp.TLS != nil,
			"latency_ms":   elapsed.Milliseconds(),
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
