package system

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadEnvVarsTool struct{}

func NewReadEnvVarsTool() *ReadEnvVarsTool { return &ReadEnvVarsTool{} }

func (t *ReadEnvVarsTool) Name() string        { return "read_env_vars" }
func (t *ReadEnvVarsTool) Description() string  { return "Reads environment variables (filters sensitive values like passwords/tokens)" }
func (t *ReadEnvVarsTool) IsReadOnly() bool     { return true }
func (t *ReadEnvVarsTool) RiskLevel() string    { return "L0" }

func (t *ReadEnvVarsTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"filter": {
				Type:        "string",
				Description: "Filter environment variable names by substring",
			},
		},
	}
}

var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "api_key", "apikey",
	"private_key", "access_key", "credential", "auth",
}

func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

func (t *ReadEnvVarsTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	filter, _ := params["filter"].(string)
	filterLower := strings.ToLower(filter)

	envs := os.Environ()
	sort.Strings(envs)

	result := make(map[string]string)
	for _, env := range envs {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]

		if filter != "" && !strings.Contains(strings.ToLower(key), filterLower) {
			continue
		}

		if isSensitive(key) {
			val = "***REDACTED***"
		}

		result[key] = val
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Read %d environment variables", len(result)),
		Output:     map[string]interface{}{"variables": result, "count": len(result), "filter": filter},
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
