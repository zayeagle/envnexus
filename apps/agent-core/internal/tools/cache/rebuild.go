package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type RebuildTool struct{}

func NewRebuildTool() *RebuildTool { return &RebuildTool{} }

func (t *RebuildTool) Name() string        { return "cache_rebuild" }
func (t *RebuildTool) Description() string  { return "Clears and rebuilds a specified cache directory" }
func (t *RebuildTool) IsReadOnly() bool     { return false }
func (t *RebuildTool) RiskLevel() string    { return "L2" }

func (t *RebuildTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"cache_type": {
				Type:        "string",
				Description: "Cache type to rebuild, default system",
				Enum:        []string{"system", "browser", "npm", "pip", "docker", "app"},
			},
		},
	}
}

func (t *RebuildTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	cacheType, _ := params["cache_type"].(string)
	if cacheType == "" {
		cacheType = "system"
	}

	targetDir, err := resolveCacheDir(cacheType)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Cannot resolve cache dir for type %q: %v", cacheType, err),
			DurationMs: time.Since(start).Milliseconds(),
		}, err
	}

	if !isAllowedCacheDir(targetDir) {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Cache directory %q is not in the allowed list", targetDir),
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("directory %q not allowed for cache rebuild", targetDir)
	}

	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(targetDir, 0755); mkErr != nil {
				return &tools.ToolResult{
					ToolName:   t.Name(),
					Status:     "failed",
					Summary:    fmt.Sprintf("Failed to create cache dir: %v", mkErr),
					DurationMs: time.Since(start).Milliseconds(),
				}, mkErr
			}
			return &tools.ToolResult{
				ToolName:   t.Name(),
				Status:     "succeeded",
				Summary:    fmt.Sprintf("Created fresh cache directory: %s", targetDir),
				Output:     map[string]string{"action": "created", "path": targetDir},
				DurationMs: time.Since(start).Milliseconds(),
			}, nil
		}
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Failed to stat cache dir: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}, err
	}
	if !info.IsDir() {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Path %s is not a directory", targetDir),
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("%s is not a directory", targetDir)
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return &tools.ToolResult{
			ToolName:   t.Name(),
			Status:     "failed",
			Summary:    fmt.Sprintf("Failed to read cache dir: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}, err
	}

	cleared := 0
	var errors []string
	for _, entry := range entries {
		path := filepath.Join(targetDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", entry.Name(), err))
		} else {
			cleared++
		}
	}

	status := "succeeded"
	summary := fmt.Sprintf("Cleared %d items from %s", cleared, targetDir)
	if len(errors) > 0 {
		status = "partial"
		summary = fmt.Sprintf("Cleared %d items, %d errors in %s", cleared, len(errors), targetDir)
	}

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   status,
		Summary:  summary,
		Output: map[string]interface{}{
			"path":    targetDir,
			"cleared": cleared,
			"errors":  errors,
		},
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func resolveCacheDir(cacheType string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	switch strings.ToLower(cacheType) {
	case "system":
		switch runtime.GOOS {
		case "windows":
			return filepath.Join(os.Getenv("LOCALAPPDATA"), "Temp"), nil
		case "linux":
			return filepath.Join(homeDir, ".cache"), nil
		case "darwin":
			return filepath.Join(homeDir, "Library", "Caches"), nil
		}
	case "browser":
		switch runtime.GOOS {
		case "windows":
			return filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "User Data", "Default", "Cache"), nil
		case "linux":
			return filepath.Join(homeDir, ".cache", "google-chrome"), nil
		case "darwin":
			return filepath.Join(homeDir, "Library", "Caches", "Google", "Chrome"), nil
		}
	case "npm":
		return filepath.Join(homeDir, ".npm", "_cacache"), nil
	case "pip":
		switch runtime.GOOS {
		case "windows":
			return filepath.Join(os.Getenv("LOCALAPPDATA"), "pip", "cache"), nil
		default:
			return filepath.Join(homeDir, ".cache", "pip"), nil
		}
	case "docker":
		return filepath.Join(homeDir, ".cache", "docker"), nil
	case "app":
		return filepath.Join(homeDir, ".envnexus", "cache"), nil
	}
	return "", fmt.Errorf("unknown cache type: %s", cacheType)
}

func isAllowedCacheDir(dir string) bool {
	homeDir, _ := os.UserHomeDir()
	allowedPrefixes := []string{
		filepath.Join(homeDir, ".cache"),
		filepath.Join(homeDir, ".npm"),
		filepath.Join(homeDir, ".envnexus"),
		filepath.Join(homeDir, "Library", "Caches"),
	}
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			allowedPrefixes = append(allowedPrefixes,
				filepath.Join(localAppData, "Temp"),
				filepath.Join(localAppData, "pip"),
			)
		}
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, prefix := range allowedPrefixes {
		absPrefix, _ := filepath.Abs(prefix)
		if strings.HasPrefix(absDir, absPrefix) {
			return true
		}
	}
	return false
}
