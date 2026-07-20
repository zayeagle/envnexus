package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadFileTailTool struct{}

func NewReadFileTailTool() *ReadFileTailTool { return &ReadFileTailTool{} }

func (t *ReadFileTailTool) Name() string        { return "read_file_tail" }
func (t *ReadFileTailTool) Description() string  { return "Reads the last N lines of a file (default 50). Useful for reading log files." }
func (t *ReadFileTailTool) IsReadOnly() bool     { return true }
func (t *ReadFileTailTool) RiskLevel() string    { return "L0" }

func (t *ReadFileTailTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"path": {
				Type:        "string",
				Description: "File path to read",
			},
			"lines": {
				Type:        "number",
				Description: "Number of lines from end to read, default 50, max 200",
			},
		},
		Required: []string{"path"},
	}
}

const maxTailLines = 200
const maxTailBytes = 64 * 1024 // 64KB

func (t *ReadFileTailTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: path"}, nil
	}

	lines := 50
	if n, ok := params["lines"].(float64); ok && n > 0 {
		lines = int(n)
	}
	if lines > maxTailLines {
		lines = maxTailLines
	}

	start := time.Now()

	safePath, err := ResolveSafePath(path)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Cannot access '%s': %v", path, err),
			Output:     map[string]interface{}{"path": path, "error": err.Error()},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	path = safePath

	info, err := os.Stat(path)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Cannot access '%s': %v", path, err),
			Output:     map[string]interface{}{"path": path, "error": err.Error()},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	if info.IsDir() {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("'%s' is a directory, not a file", path),
			Output:     map[string]interface{}{"path": path, "error": "path is a directory"},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Cannot open '%s': %v", path, err),
			Output:     map[string]interface{}{"path": path, "error": err.Error()},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	defer f.Close()

	// Read from the end of file, up to maxTailBytes
	fileSize := info.Size()
	readSize := int64(maxTailBytes)
	if fileSize < readSize {
		readSize = fileSize
	}

	offset := fileSize - readSize
	if offset < 0 {
		offset = 0
	}

	_, _ = f.Seek(offset, 0)
	scanner := bufio.NewScanner(f)
	var allLines []string
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	// If we started mid-file, first line may be partial — skip it
	if offset > 0 && len(allLines) > 0 {
		allLines = allLines[1:]
	}

	// Take last N lines
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}

	content := strings.Join(allLines, "\n")
	elapsed := time.Since(start)

	return &tools.ToolResult{
		ToolName:   t.Name(),
		Status:     "succeeded",
		Summary:    fmt.Sprintf("Read last %d lines from '%s' (%d bytes)", len(allLines), path, len(content)),
		Output: map[string]interface{}{
			"path":        path,
			"lines_read":  len(allLines),
			"file_size":   fileSize,
			"content":     content,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
