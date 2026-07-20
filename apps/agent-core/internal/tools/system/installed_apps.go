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

type ReadInstalledAppsTool struct{}

func NewReadInstalledAppsTool() *ReadInstalledAppsTool { return &ReadInstalledAppsTool{} }

func (t *ReadInstalledAppsTool) Name() string        { return "read_installed_apps" }
func (t *ReadInstalledAppsTool) Description() string  { return "Lists installed applications/packages on the system. Supports optional name filter." }
func (t *ReadInstalledAppsTool) IsReadOnly() bool     { return true }
func (t *ReadInstalledAppsTool) RiskLevel() string    { return "L0" }

func (t *ReadInstalledAppsTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"filter": {
				Type:        "string",
				Description: "Filter installed apps by name substring",
			},
		},
	}
}

func (t *ReadInstalledAppsTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	filter, _ := params["filter"].(string)
	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Query both 64-bit and 32-bit uninstall registry keys
		ps := `Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*,HKLM:\Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall\* -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName } | Select-Object DisplayName,DisplayVersion,Publisher,InstallDate | Sort-Object DisplayName | Format-Table -AutoSize -Wrap`
		if filter != "" {
			ps = fmt.Sprintf(`Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*,HKLM:\Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall\* -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -like '*%s*' } | Select-Object DisplayName,DisplayVersion,Publisher,InstallDate | Sort-Object DisplayName | Format-Table -AutoSize -Wrap`, filter)
		}
		cmd = exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)

	case "linux":
		if _, err := exec.LookPath("dpkg"); err == nil {
			if filter != "" {
				cmd = exec.CommandContext(cmdCtx, "dpkg", "-l", fmt.Sprintf("*%s*", filter))
			} else {
				cmd = exec.CommandContext(cmdCtx, "dpkg", "-l")
			}
		} else if _, err := exec.LookPath("rpm"); err == nil {
			if filter != "" {
				cmd = exec.CommandContext(cmdCtx, "rpm", "-qa", fmt.Sprintf("*%s*", filter))
			} else {
				cmd = exec.CommandContext(cmdCtx, "rpm", "-qa", "--queryformat", "%{NAME} %{VERSION}-%{RELEASE} %{ARCH}\n")
			}
		} else if _, err := exec.LookPath("apk"); err == nil {
			cmd = exec.CommandContext(cmdCtx, "apk", "list", "--installed")
		} else {
			return &tools.ToolResult{
				ToolName:   t.Name(),
				Status:     "succeeded",
				Summary:    "No supported package manager found (dpkg/rpm/apk)",
				Output:     map[string]interface{}{"error": "no package manager found"},
				DurationMs: time.Since(start).Milliseconds(),
			}, nil
		}

	case "darwin":
		if filter != "" {
			// system_profiler + grep
			cmd = exec.CommandContext(cmdCtx, "bash", "-c",
				fmt.Sprintf("system_profiler SPApplicationsDataType 2>/dev/null | grep -i '%s' -A 3", filter))
		} else {
			cmd = exec.CommandContext(cmdCtx, "system_profiler", "SPApplicationsDataType")
		}

	default:
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Unsupported OS: %s", runtime.GOOS),
			Output:     map[string]interface{}{"error": fmt.Sprintf("unsupported OS: %s", runtime.GOOS)},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	const maxOutput = 16384
	truncated := false
	if len(outputStr) > maxOutput {
		outputStr = outputStr[:maxOutput] + "\n... (output truncated)"
		truncated = true
	}

	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		// dpkg -l returns exit code 1 when no match, still has useful output
		if outputStr != "" {
			return &tools.ToolResult{
				ToolName:   t.Name(),
				Status:     "succeeded",
				Summary:    fmt.Sprintf("Listed installed apps (exit code %d)", exitCode),
				Output:     map[string]interface{}{"filter": filter, "output": outputStr, "exit_code": exitCode, "truncated": truncated, "os": runtime.GOOS},
				DurationMs: elapsed.Milliseconds(),
			}, nil
		}
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Failed to list installed apps: %v", err),
			Output:     map[string]interface{}{"filter": filter, "error": err.Error(), "os": runtime.GOOS},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	lineCount := len(strings.Split(outputStr, "\n"))

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Listed %d lines of installed apps", lineCount),
		Output:     map[string]interface{}{"filter": filter, "output": outputStr, "line_count": lineCount, "truncated": truncated, "os": runtime.GOOS},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
