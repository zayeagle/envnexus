package network

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadProxyConfigTool struct{}

func NewReadProxyConfigTool() *ReadProxyConfigTool {
	return &ReadProxyConfigTool{}
}

func (t *ReadProxyConfigTool) Name() string {
	return "read_proxy_config"
}

func (t *ReadProxyConfigTool) Description() string {
	return "Detects proxy configuration from environment variables and system settings."
}

func (t *ReadProxyConfigTool) IsReadOnly() bool {
	return true
}

func (t *ReadProxyConfigTool) RiskLevel() string {
	return "L0"
}

func (t *ReadProxyConfigTool) Parameters() *tools.ParamSchema {
	return tools.NoParams()
}

func (t *ReadProxyConfigTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	envVars := map[string]string{
		"HTTP_PROXY":  os.Getenv("HTTP_PROXY"),
		"HTTPS_PROXY": os.Getenv("HTTPS_PROXY"),
		"NO_PROXY":    os.Getenv("NO_PROXY"),
		"http_proxy":  os.Getenv("http_proxy"),
		"https_proxy": os.Getenv("https_proxy"),
		"no_proxy":    os.Getenv("no_proxy"),
		"ALL_PROXY":   os.Getenv("ALL_PROXY"),
		"all_proxy":   os.Getenv("all_proxy"),
	}

	activeProxies := make(map[string]string)
	for k, v := range envVars {
		if v != "" {
			activeProxies[k] = v
		}
	}

	hasProxy := len(activeProxies) > 0

	goProxy := ""
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport.Proxy != nil {
		testReq, _ := http.NewRequest("GET", "http://example.com", nil)
		if proxyURL, err := transport.Proxy(testReq); err == nil && proxyURL != nil {
			goProxy = proxyURL.String()
			hasProxy = true
		}
	}

	var systemProxy string
	if runtime.GOOS == "windows" {
		systemProxy = detectWindowsProxy()
	}
	if systemProxy != "" {
		hasProxy = true
	}

	result := map[string]interface{}{
		"has_proxy":       hasProxy,
		"env_proxies":     activeProxies,
		"go_http_proxy":   goProxy,
		"system_proxy":    systemProxy,
		"platform":        runtime.GOOS,
	}

	summary := "No proxy detected"
	if hasProxy {
		var parts []string
		for k, v := range activeProxies {
			parts = append(parts, k+"="+v)
		}
		if goProxy != "" {
			parts = append(parts, "Go HTTP proxy: "+goProxy)
		}
		if systemProxy != "" {
			parts = append(parts, "System proxy: "+systemProxy)
		}
		summary = "Proxy detected: " + strings.Join(parts, ", ")
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func detectWindowsProxy() string {
	out, err := exec.Command("netsh", "winhttp", "show", "proxy").Output()
	if err != nil {
		return ""
	}
	output := string(out)
	if strings.Contains(output, "Direct access") || strings.Contains(output, "直接访问") {
		return ""
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Proxy Server") || strings.Contains(line, "代理服务器") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
