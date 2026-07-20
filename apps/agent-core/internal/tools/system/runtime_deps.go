package system

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type CheckRuntimeDepsTool struct{}

func NewCheckRuntimeDepsTool() *CheckRuntimeDepsTool { return &CheckRuntimeDepsTool{} }

func (t *CheckRuntimeDepsTool) Name() string        { return "check_runtime_deps" }
func (t *CheckRuntimeDepsTool) Description() string  { return "Checks if common runtime dependencies are installed (Java, Python, Node.js, .NET, Go, Docker, Git, etc.)" }
func (t *CheckRuntimeDepsTool) IsReadOnly() bool     { return true }
func (t *CheckRuntimeDepsTool) RiskLevel() string    { return "L0" }

func (t *CheckRuntimeDepsTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"filter": {
				Type:        "string",
				Description: "Filter dependencies by name substring",
			},
		},
	}
}

type runtimeCheck struct {
	Name    string
	Binary  string
	Args    []string
}

var runtimeChecks = []runtimeCheck{
	{"Java", "java", []string{"-version"}},
	{"Python 3", "python3", []string{"--version"}},
	{"Python", "python", []string{"--version"}},
	{"Node.js", "node", []string{"--version"}},
	{"npm", "npm", []string{"--version"}},
	{"Go", "go", []string{"version"}},
	{"Rust (rustc)", "rustc", []string{"--version"}},
	{"Docker", "docker", []string{"--version"}},
	{"Docker Compose", "docker-compose", []string{"--version"}},
	{"Git", "git", []string{"--version"}},
	{"curl", "curl", []string{"--version"}},
	{"OpenSSL", "openssl", []string{"version"}},
	{"PowerShell", "pwsh", []string{"--version"}},
}

var windowsExtraChecks = []runtimeCheck{
	{".NET Framework", "reg", []string{"query", `HKLM\SOFTWARE\Microsoft\NET Framework Setup\NDP\v4\Full`, "/v", "Release"}},
	{".NET SDK", "dotnet", []string{"--list-sdks"}},
	{".NET Runtime", "dotnet", []string{"--list-runtimes"}},
	{"Visual C++ (cl)", "cl", nil},
}

var linuxExtraChecks = []runtimeCheck{
	{"GCC", "gcc", []string{"--version"}},
	{"Make", "make", []string{"--version"}},
	{"systemd", "systemctl", []string{"--version"}},
	{"kubectl", "kubectl", []string{"version", "--client", "--short"}},
}

func (t *CheckRuntimeDepsTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	checks := make([]runtimeCheck, len(runtimeChecks))
	copy(checks, runtimeChecks)

	switch runtime.GOOS {
	case "windows":
		checks = append(checks, windowsExtraChecks...)
	case "linux":
		checks = append(checks, linuxExtraChecks...)
	case "darwin":
		checks = append(checks, linuxExtraChecks...)
		checks = append(checks, runtimeCheck{"Xcode CLI Tools", "xcode-select", []string{"-p"}})
		checks = append(checks, runtimeCheck{"Homebrew", "brew", []string{"--version"}})
	}

	// Optional filter
	filter, _ := params["filter"].(string)
	filterLower := strings.ToLower(filter)

	type depResult struct {
		Name      string `json:"name"`
		Binary    string `json:"binary"`
		Installed bool   `json:"installed"`
		Version   string `json:"version,omitempty"`
		Path      string `json:"path,omitempty"`
	}

	var results []depResult
	installed, missing := 0, 0

	for _, check := range checks {
		if filter != "" && !strings.Contains(strings.ToLower(check.Name), filterLower) &&
			!strings.Contains(strings.ToLower(check.Binary), filterLower) {
			continue
		}

		dr := depResult{Name: check.Name, Binary: check.Binary}

		binPath, err := exec.LookPath(check.Binary)
		if err != nil {
			dr.Installed = false
			missing++
			results = append(results, dr)
			continue
		}
		dr.Path = binPath
		dr.Installed = true
		installed++

		if len(check.Args) > 0 {
			cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			out, err := exec.CommandContext(cmdCtx, check.Binary, check.Args...).CombinedOutput()
			cancel()
			if err == nil {
				version := strings.TrimSpace(string(out))
				// Take first line only
				if idx := strings.IndexByte(version, '\n'); idx > 0 {
					version = version[:idx]
				}
				if len(version) > 200 {
					version = version[:200]
				}
				dr.Version = version
			}
		}

		results = append(results, dr)
	}

	elapsed := time.Since(start)

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Checked %d runtimes: %d installed, %d missing", len(results), installed, missing),
		Output: map[string]interface{}{
			"runtimes":  results,
			"installed": installed,
			"missing":   missing,
			"total":     len(results),
			"os":        runtime.GOOS,
			"filter":    filter,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
