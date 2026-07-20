package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type FileRenameTool struct{}

func NewFileRenameTool() *FileRenameTool { return &FileRenameTool{} }

func (t *FileRenameTool) Name() string { return "file_rename" }
func (t *FileRenameTool) Description() string {
	return "Rename or move a file or directory. Supports renaming in place or moving to a different path."
}
func (t *FileRenameTool) IsReadOnly() bool  { return false }
func (t *FileRenameTool) RiskLevel() string { return "L2" }

func (t *FileRenameTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"source": {
				Type:        "string",
				Description: "Current path of the file or directory",
			},
			"destination": {
				Type:        "string",
				Description: "New path (can be just a new name in the same directory, or a full path to move)",
			},
		},
		Required: []string{"source", "destination"},
	}
}

func (t *FileRenameTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	source, _ := params["source"].(string)
	destination, _ := params["destination"].(string)

	if source == "" || destination == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "both source and destination are required"}, nil
	}

	srcInfo, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: fmt.Sprintf("source path does not exist: %s", source)}, nil
		}
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: fmt.Sprintf("cannot access source: %v", err)}, nil
	}

	if _, err := os.Stat(destination); err == nil {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: fmt.Sprintf("destination already exists: %s", destination)}, nil
	}

	destDir := filepath.Dir(destination)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: fmt.Sprintf("destination parent directory does not exist: %s", destDir)}, nil
	}

	start := time.Now()
	if err := os.Rename(source, destination); err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Error:      fmt.Sprintf("rename failed: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	elapsed := time.Since(start)

	itemType := "file"
	if srcInfo.IsDir() {
		itemType = "directory"
	}

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Renamed %s: %s → %s", itemType, source, destination),
		Output: map[string]interface{}{
			"source":      source,
			"destination": destination,
			"type":        itemType,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
