package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ReadDirListTool struct{}

func NewReadDirListTool() *ReadDirListTool { return &ReadDirListTool{} }

func (t *ReadDirListTool) Name() string        { return "read_dir_list" }
func (t *ReadDirListTool) Description() string  { return "Lists files and subdirectories in a directory with size and modification time" }
func (t *ReadDirListTool) IsReadOnly() bool     { return true }
func (t *ReadDirListTool) RiskLevel() string    { return "L0" }

func (t *ReadDirListTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"path": {
				Type:        "string",
				Description: "Directory path to list contents",
			},
		},
		Required: []string{"path"},
	}
}

const maxDirEntries = 200

func (t *ReadDirListTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return &tools.ToolResult{ToolName: t.Name(), Status: "failed", Error: "missing required parameter: path"}, nil
	}

	start := time.Now()

	// On Windows, "/" means "list all drives" since there's no single root.
	if runtime.GOOS == "windows" && (path == "/" || path == "\\") {
		return t.listWindowsDrives(start), nil
	}

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
			Output:     map[string]interface{}{"path": path, "error": err.Error(), "exists": !os.IsNotExist(err)},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
	if !info.IsDir() {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("'%s' is a file, not a directory", path),
			Output:     map[string]interface{}{"path": path, "error": "path is a file, not a directory"},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "succeeded",
			Summary:    fmt.Sprintf("Cannot read directory '%s': %v", path, err),
			Output:     map[string]interface{}{"path": path, "error": err.Error()},
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}

	type entryInfo struct {
		Name       string `json:"name"`
		IsDir      bool   `json:"is_dir"`
		SizeBytes  int64  `json:"size_bytes"`
		ModifiedAt string `json:"modified_at"`
		Mode       string `json:"mode"`
	}

	truncated := false
	if len(entries) > maxDirEntries {
		entries = entries[:maxDirEntries]
		truncated = true
	}

	items := make([]entryInfo, 0, len(entries))
	var totalSize int64
	dirCount, fileCount := 0, 0

	for _, e := range entries {
		ei := entryInfo{
			Name:  e.Name(),
			IsDir: e.IsDir(),
		}
		if fi, err := e.Info(); err == nil {
			ei.SizeBytes = fi.Size()
			ei.ModifiedAt = fi.ModTime().Format(time.RFC3339)
			ei.Mode = fi.Mode().String()
			totalSize += fi.Size()
		}
		if e.IsDir() {
			dirCount++
		} else {
			fileCount++
		}
		items = append(items, ei)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})

	absPath, _ := filepath.Abs(path)
	elapsed := time.Since(start)

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Listed %d dirs, %d files in '%s'", dirCount, fileCount, path),
		Output: map[string]interface{}{
			"path":        absPath,
			"entries":     items,
			"dir_count":   dirCount,
			"file_count":  fileCount,
			"total_size":  totalSize,
			"truncated":   truncated,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

// listWindowsDrives enumerates available drive letters (A:-Z:) and returns
// them as virtual directory entries so the file browser can navigate into any drive.
func (t *ReadDirListTool) listWindowsDrives(start time.Time) *tools.ToolResult {
	type entryInfo struct {
		Name       string `json:"name"`
		IsDir      bool   `json:"is_dir"`
		SizeBytes  int64  `json:"size_bytes"`
		ModifiedAt string `json:"modified_at"`
		Mode       string `json:"mode"`
	}

	var drives []entryInfo
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + `:\`
		if _, err := os.Stat(root); err == nil {
			drives = append(drives, entryInfo{
				Name:  string(letter) + ":",
				IsDir: true,
			})
		}
	}

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Listed %d drives on Windows", len(drives)),
		Output: map[string]interface{}{
			"path":       "/",
			"entries":    drives,
			"dir_count":  len(drives),
			"file_count": 0,
			"total_size": 0,
			"truncated":  false,
			"is_drives":  true,
		},
		DurationMs: time.Since(start).Milliseconds(),
	}
}
