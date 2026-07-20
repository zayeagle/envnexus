package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ProxyToggleTool struct{}

func NewProxyToggleTool() *ProxyToggleTool { return &ProxyToggleTool{} }

func (t *ProxyToggleTool) Name() string        { return "proxy_toggle" }
func (t *ProxyToggleTool) IsReadOnly() bool     { return false }
func (t *ProxyToggleTool) RiskLevel() string    { return "L1" }

func (t *ProxyToggleTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"action": {
				Type:        "string",
				Description: "Action to perform",
				Enum:        []string{"enable", "disable"},
			},
			"proxy_url": {
				Type:        "string",
				Description: "Proxy URL, required when action is enable",
			},
		},
		Required: []string{"action"},
	}
}

func (t *ProxyToggleTool) Description() string {
	return "Enable or disable system/application-level HTTP proxy. Params: action (enable|disable), proxy_url (optional, required when enabling)."
}

func (t *ProxyToggleTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	action, _ := params["action"].(string)
	if action != "enable" && action != "disable" {
		return nil, fmt.Errorf("action must be 'enable' or 'disable'")
	}

	switch runtime.GOOS {
	case "linux":
		return t.toggleLinux(ctx, action, params)
	case "darwin":
		return t.toggleMacOS(ctx, action, params)
	case "windows":
		return t.toggleWindows(ctx, action, params)
	default:
		return &tools.ToolResult{
			Status: "error",
			Output:  fmt.Sprintf("proxy.toggle not supported on %s", runtime.GOOS),
		}, nil
	}
}

func (t *ProxyToggleTool) toggleLinux(ctx context.Context, action string, params map[string]interface{}) (*tools.ToolResult, error) {
	envFiles := []string{"/etc/environment", "/etc/profile.d/proxy.sh"}
	_ = envFiles

	proxyURL, _ := params["proxy_url"].(string)

	if action == "enable" {
		if proxyURL == "" {
			return nil, fmt.Errorf("proxy_url is required when enabling proxy")
		}
		// Write to a temp profile script
		script := fmt.Sprintf("export http_proxy=%s\nexport https_proxy=%s\nexport HTTP_PROXY=%s\nexport HTTPS_PROXY=%s\n",
			proxyURL, proxyURL, proxyURL, proxyURL)
		if err := os.WriteFile("/tmp/enx_proxy.sh", []byte(script), 0644); err != nil {
			return nil, fmt.Errorf("write proxy config: %w", err)
		}
		return &tools.ToolResult{
			Status: "ok",
			Output:  fmt.Sprintf("Proxy enabled: %s (written to /tmp/enx_proxy.sh). Source the file or restart shell.", proxyURL),
		}, nil
	}

	// disable: clear proxy env vars in current process context (best effort)
	os.Unsetenv("http_proxy")
	os.Unsetenv("https_proxy")
	os.Unsetenv("HTTP_PROXY")
	os.Unsetenv("HTTPS_PROXY")
	os.Remove("/tmp/enx_proxy.sh")

	return &tools.ToolResult{Status: "ok", Output: "Proxy disabled (env vars cleared in current session)."}, nil
}

func (t *ProxyToggleTool) toggleMacOS(ctx context.Context, action string, params map[string]interface{}) (*tools.ToolResult, error) {
	proxyURL, _ := params["proxy_url"].(string)

	// Get primary network service
	out, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("list network services: %w", err)
	}
	services := strings.Split(strings.TrimSpace(string(out)), "\n")
	service := "Wi-Fi"
	if len(services) > 1 {
		service = strings.TrimSpace(services[1])
	}

	if action == "enable" {
		if proxyURL == "" {
			return nil, fmt.Errorf("proxy_url is required when enabling proxy")
		}
		// Parse host:port from proxyURL
		host := proxyURL
		port := "8080"
		if idx := strings.LastIndex(proxyURL, ":"); idx > 6 {
			host = proxyURL[:idx]
			port = proxyURL[idx+1:]
		}
		host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")

		cmd := exec.CommandContext(ctx, "networksetup", "-setwebproxy", service, host, port)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("set web proxy: %w", err)
		}
		return &tools.ToolResult{
			Status: "ok",
			Output:  fmt.Sprintf("Web proxy enabled for %s: %s:%s", service, host, port),
		}, nil
	}

	// disable
	cmd := exec.CommandContext(ctx, "networksetup", "-setwebproxystate", service, "off")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("disable web proxy: %w", err)
	}
	return &tools.ToolResult{Status: "ok", Output: fmt.Sprintf("Web proxy disabled for %s.", service)}, nil
}

func (t *ProxyToggleTool) toggleWindows(ctx context.Context, action string, params map[string]interface{}) (*tools.ToolResult, error) {
	proxyURL, _ := params["proxy_url"].(string)

	if action == "enable" {
		if proxyURL == "" {
			return nil, fmt.Errorf("proxy_url is required when enabling proxy")
		}
		cmds := [][]string{
			{"reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f"},
			{"reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxyURL, "/f"},
		}
		for _, args := range cmds {
			if err := exec.CommandContext(ctx, args[0], args[1:]...).Run(); err != nil {
				return nil, fmt.Errorf("reg command failed: %w", err)
			}
		}
		return &tools.ToolResult{Status: "ok", Output: fmt.Sprintf("Proxy enabled: %s", proxyURL)}, nil
	}

	if err := exec.CommandContext(ctx, "reg", "add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run(); err != nil {
		return nil, fmt.Errorf("disable proxy: %w", err)
	}
	return &tools.ToolResult{Status: "ok", Output: "Proxy disabled."}, nil
}

