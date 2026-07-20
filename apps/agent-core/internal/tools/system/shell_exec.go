package system

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

// safeReadOnlyCommands lists commands that are genuinely read-only diagnostics.
// Commands NOT in this list will trigger the approval flow — no hard blocking.
//
// IMPORTANT: Only add commands here that CANNOT modify system state.
// Commands like net, sc, schtasks, netsh, iptables, systemctl, docker, kubectl
// are excluded because they have write sub-commands.
var safeReadOnlyCommands = map[string]map[string]bool{
	"windows": {
		"ipconfig": true, "netstat": true, "nslookup": true, "ping": true,
		"tracert": true, "pathping": true, "arp": true,
		"getmac": true, "nbtstat": true,
		"hostname": true, "systeminfo": true, "tasklist": true, "whoami": true,
		"findstr": true, "type": true, "dir": true, "where": true,
		"echo": true, "ver": true, "vol": true, "tree": true, "set": true,
		"get-childitem": true, "get-content": true, "get-item": true,
		"get-itemproperty": true, "get-process": true, "get-service": true,
		"get-netadapter": true, "get-netipaddress": true, "get-netroute": true,
		"get-nettcpconnection": true, "get-disk": true, "get-volume": true,
		"get-psdrive": true, "get-hotfix": true, "get-computerinfo": true,
		"get-wmiobject": true, "get-ciminstance": true, "get-date": true,
		"get-host": true, "get-location": true, "get-command": true,
		"select-object": true, "where-object": true, "format-table": true,
		"format-list": true, "out-string": true, "measure-object": true,
		"sort-object": true, "group-object": true, "select-string": true,
		"test-connection": true, "test-netconnection": true, "test-path": true,
		"resolve-dnsname": true, "write-output": true, "write-host": true,
	},
	"linux": {
		"ifconfig": true, "netstat": true, "ss": true,
		"nslookup": true, "dig": true, "ping": true, "traceroute": true,
		"tracepath": true, "arp": true, "ethtool": true,
		"hostname": true, "uname": true, "uptime": true,
		"free": true, "df": true, "ps": true, "lsof": true, "whoami": true,
		"id": true, "dmesg": true, "lsblk": true,
		"journalctl": true, "env": true, "printenv": true,
		"cat": true, "head": true, "tail": true, "grep": true, "ls": true,
		"stat": true, "wc": true, "awk": true, "find": true, "which": true,
		"file": true, "du": true, "top": true, "htop": true,
	},
	"darwin": {
		"ifconfig": true, "netstat": true, "nslookup": true, "dig": true,
		"ping": true, "traceroute": true, "arp": true,
		"scutil": true,
		"hostname": true, "uname": true, "uptime": true, "df": true,
		"ps": true, "lsof": true, "whoami": true, "id": true,
		"system_profiler": true, "sw_vers": true, "dscacheutil": true,
		"env": true, "printenv": true,
		"cat": true, "head": true, "tail": true, "grep": true, "ls": true,
		"stat": true, "wc": true, "awk": true, "find": true, "which": true,
		"file": true, "du": true, "top": true, "htop": true,
	},
}

type ShellExecTool struct{}

func NewShellExecTool() *ShellExecTool { return &ShellExecTool{} }

func (t *ShellExecTool) Name() string { return "shell_exec" }
func (t *ShellExecTool) Description() string {
	if runtime.GOOS == "windows" {
		return "Executes a PowerShell command on the local Windows machine. Whitelisted diagnostic commands (e.g. ipconfig, netstat, ping, systeminfo, Get-ChildItem, Get-Process) run directly. Non-whitelisted or write commands require user approval before execution. Use PowerShell syntax."
	}
	return "Executes a shell command on the local machine via sh. Whitelisted diagnostic commands (e.g. ifconfig, netstat, ping, ls, ps, df) run directly. Non-whitelisted or write commands require user approval before execution."
}
func (t *ShellExecTool) IsReadOnly() bool  { return false }
func (t *ShellExecTool) RiskLevel() string { return "L2" }

// shellOperators are characters/sequences that chain or redirect commands,
// making a "safe" first token meaningless from a security standpoint.
var shellOperators = []string{"&&", "||", ";", "|", ">", ">>", "<", "$(", "`"}

func (t *ShellExecTool) NeedsApproval(params map[string]interface{}) bool {
	command, _ := params["command"].(string)
	if command == "" {
		return false
	}

	cmdLower := strings.ToLower(command)
	for _, op := range shellOperators {
		if strings.Contains(cmdLower, op) {
			return true
		}
	}

	parts := parseCommand(command)
	if len(parts) == 0 {
		return false
	}
	binary := strings.ToLower(parts[0])
	safe := safeReadOnlyCommands[runtime.GOOS]
	if safe == nil {
		safe = safeReadOnlyCommands["linux"]
	}
	return !safe[binary]
}

func (t *ShellExecTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"command": {
				Type:        "string",
				Description: "Command to execute. On Windows, this runs in PowerShell (use PowerShell syntax like Get-ChildItem, Rename-Item, etc.). On Linux/macOS, this runs in sh. Whitelisted read-only commands run directly; write commands require user approval.",
			},
		},
		Required: []string{"command"},
	}
}

func (t *ShellExecTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	command, _ := params["command"].(string)
	if command == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: command"}, nil
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "empty command"}, nil
	}

	slog.Info("[agent] shell_exec start", "command", truncShellLog(command, 4096), "os", runtime.GOOS)

	start := time.Now()
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		psExe := findPowerShell()
		cmd = exec.CommandContext(execCtx, psExe, "-NoProfile", "-NonInteractive", "-Command", command)
	default:
		cmd = exec.CommandContext(execCtx, "sh", "-c", command)
	}

	output, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	outputStr := strings.TrimSpace(string(output))

	const maxOutput = 8192
	if len(outputStr) > maxOutput {
		outputStr = outputStr[:maxOutput] + "\n... (output truncated)"
	}

	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		slog.Info("[agent] shell_exec done", "command", truncShellLog(command, 4096), "exit_code", exitCode, "output_excerpt", truncShellLog(outputStr, 2000), "error", err.Error(), "duration_ms", elapsed.Milliseconds())
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Command exited with code %d (%dms)", exitCode, elapsed.Milliseconds()),
			Output:     map[string]interface{}{"command": command, "exit_code": exitCode, "output": outputStr, "error": err.Error(), "os": runtime.GOOS},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	slog.Info("[agent] shell_exec done", "command", truncShellLog(command, 4096), "exit_code", 0, "output_excerpt", truncShellLog(outputStr, 2000), "duration_ms", elapsed.Milliseconds())
	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Command completed successfully (%dms)", elapsed.Milliseconds()),
		Output:     map[string]interface{}{"command": command, "exit_code": 0, "output": outputStr, "os": runtime.GOOS},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func findPowerShell() string {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	return "powershell"
}

func parseCommand(cmd string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
		} else if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func truncShellLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
