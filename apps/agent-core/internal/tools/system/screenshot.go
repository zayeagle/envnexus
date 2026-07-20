package system

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ScreenshotTool struct{}

func NewScreenshotTool() *ScreenshotTool { return &ScreenshotTool{} }

func (t *ScreenshotTool) Name() string        { return "screenshot" }
func (t *ScreenshotTool) Description() string  { return "Captures a screenshot of the desktop" }
func (t *ScreenshotTool) IsReadOnly() bool     { return true }
func (t *ScreenshotTool) RiskLevel() string    { return "L1" }
func (t *ScreenshotTool) NeedsApproval(map[string]interface{}) bool { return true }

func (t *ScreenshotTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"format": {
				Type:        "string",
				Description: "Image format: png or jpg (default: png)",
			},
		},
		Required: []string{},
	}
}

func (t *ScreenshotTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	format := "png"
	if f, ok := params["format"].(string); ok && (f == "jpg" || f == "jpeg") {
		format = "jpg"
	}

	tmpDir := os.TempDir()
	outPath := filepath.Join(tmpDir, fmt.Sprintf("enx_screenshot_%d.%s", time.Now().UnixNano(), format))

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "screencapture", "-x", outPath)
	case "linux":
		cmd = exec.CommandContext(ctx, "scrot", "-o", outPath)
	case "windows":
		// Use PowerShell to take a screenshot
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Screen]::PrimaryScreen | ForEach-Object { $bmp = New-Object System.Drawing.Bitmap($_.Bounds.Width, $_.Bounds.Height); $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($_.Bounds.Location, [System.Drawing.Point]::Empty, $_.Bounds.Size); $bmp.Save('%s'); $g.Dispose(); $bmp.Dispose() }`, outPath)
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
	default:
		return failResult(t.Name(), fmt.Sprintf("unsupported OS: %s", runtime.GOOS), start), nil
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return failResult(t.Name(), fmt.Sprintf("screenshot command failed: %v — %s", err, string(output)), start), nil
	}
	defer os.Remove(outPath)

	data, err := os.ReadFile(outPath)
	if err != nil {
		return failResult(t.Name(), fmt.Sprintf("cannot read screenshot: %v", err), start), nil
	}

	b64 := base64.StdEncoding.EncodeToString(data)

	mediaType := "image/png"
	if format == "jpg" {
		mediaType = "image/jpeg"
	}

	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Captured %s screenshot (%d bytes)", format, len(data)),
		Output: map[string]interface{}{
			"format":      format,
			"media_type":  mediaType,
			"size_bytes":  len(data),
			"base64_data": b64,
		},
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
