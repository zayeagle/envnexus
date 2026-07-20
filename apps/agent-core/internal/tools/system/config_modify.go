package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

// Allowlist of safe config keys that can be modified.
// Operators must explicitly whitelist keys; arbitrary file writes are not permitted.
var allowedConfigKeys = map[string]bool{
	"http_proxy":         true,
	"https_proxy":        true,
	"no_proxy":           true,
	"HTTP_PROXY":         true,
	"HTTPS_PROXY":        true,
	"NO_PROXY":           true,
	"ENX_LOG_LEVEL":      true,
	"ENX_LLM_PRIMARY":    true,
	"ENX_PLATFORM_URL":   true,
}

type ConfigModifyTool struct{}

func NewConfigModifyTool() *ConfigModifyTool { return &ConfigModifyTool{} }

func (t *ConfigModifyTool) Name() string     { return "config_modify" }
func (t *ConfigModifyTool) IsReadOnly() bool  { return false }
func (t *ConfigModifyTool) RiskLevel() string { return "L1" }

func (t *ConfigModifyTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"file": {
				Type:        "string",
				Description: "Path to env file, default /tmp/enx_agent.env",
			},
			"key": {
				Type:        "string",
				Description: "Configuration key to modify, must be in allowlist",
			},
			"value": {
				Type:        "string",
				Description: "New value, empty to remove the key",
			},
		},
		Required: []string{"key"},
	}
}

func (t *ConfigModifyTool) Description() string {
	return "Modify a whitelisted configuration key in the agent environment file. " +
		"Params: file (path to env file), key (must be in allowlist), value (new value)."
}

func (t *ConfigModifyTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	filePath, _ := params["file"].(string)
	key, _ := params["key"].(string)
	value, _ := params["value"].(string)

	if filePath == "" {
		filePath = "/tmp/enx_agent.env"
	}
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if !allowedConfigKeys[key] {
		return nil, fmt.Errorf("key %q is not in the allowed config modification list; allowed keys: %v", key, allowedConfigKeyList())
	}

	// Read existing file
	lines := []string{}
	if data, err := os.ReadFile(filePath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
	}

	// Update or append
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") || strings.HasPrefix(line, "export "+key+"=") {
			if value == "" {
				lines = append(lines[:i], lines[i+1:]...)
			} else {
				lines[i] = fmt.Sprintf("%s=%s", key, value)
			}
			found = true
			break
		}
	}
	if !found && value != "" {
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
	}

	// Write back
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("write config file %s: %w", filePath, err)
	}

	action := "updated"
	if value == "" {
		action = "removed"
	}
	if !found && value != "" {
		action = "added"
	}

	return &tools.ToolResult{
		Status: "ok",
		Output:  fmt.Sprintf("Config key %q %s in %s.", key, action, filePath),
	}, nil
}

func allowedConfigKeyList() []string {
	keys := make([]string, 0, len(allowedConfigKeys))
	for k := range allowedConfigKeys {
		keys = append(keys, k)
	}
	return keys
}

