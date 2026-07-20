package system

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadFileInfoTool struct{}

func NewReadFileInfoTool() *ReadFileInfoTool { return &ReadFileInfoTool{} }

func (t *ReadFileInfoTool) Name() string        { return "read_file_info" }
func (t *ReadFileInfoTool) Description() string  { return "Checks if a file or directory exists, and returns size, permissions, and modification time" }
func (t *ReadFileInfoTool) IsReadOnly() bool     { return true }
func (t *ReadFileInfoTool) RiskLevel() string    { return "L0" }

func (t *ReadFileInfoTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"path": {
				Type:        "string",
				Description: "File or directory path to inspect",
			},
		},
		Required: []string{"path"},
	}
}

func (t *ReadFileInfoTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: path"}, nil
	}

	start := time.Now()
	info, err := os.Stat(path)
	elapsed := time.Since(start)

	if err != nil {
		exists := !os.IsNotExist(err)
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Path '%s' check failed: %v", path, err),
			Output: map[string]interface{}{
				"path":       path,
				"exists":     exists,
				"error":      err.Error(),
				"permission_denied": os.IsPermission(err),
			},
			DurationMs: elapsed.Milliseconds(),
		}, nil
	}

	result := map[string]interface{}{
		"path":          path,
		"exists":        true,
		"is_dir":        info.IsDir(),
		"size_bytes":    info.Size(),
		"mode":          info.Mode().String(),
		"modified_at":   info.ModTime().Format(time.RFC3339),
		"modified_ago":  fmt.Sprintf("%s", time.Since(info.ModTime()).Round(time.Second)),
	}

	if !info.IsDir() {
		sizeMB := float64(info.Size()) / 1024 / 1024
		result["size_mb"] = fmt.Sprintf("%.2f", sizeMB)
	}

	if runtime.GOOS != "windows" {
		result["permissions"] = fmt.Sprintf("%o", info.Mode().Perm())
	}

	summary := fmt.Sprintf("'%s' exists", path)
	if info.IsDir() {
		summary += " (directory)"
	} else {
		summary += fmt.Sprintf(" (file, %.2f MB)", float64(info.Size())/1024/1024)
	}

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    summary,
		Output:     result,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
