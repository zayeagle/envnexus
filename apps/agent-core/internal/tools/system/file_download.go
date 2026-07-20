package system

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type FileDownloadTool struct{}

func NewFileDownloadTool() *FileDownloadTool { return &FileDownloadTool{} }

func (t *FileDownloadTool) Name() string { return "file_download" }
func (t *FileDownloadTool) Description() string {
	return "Downloads a file from the local filesystem and uploads it to object storage (MinIO), returning a presigned download URL"
}
func (t *FileDownloadTool) IsReadOnly() bool  { return false }
func (t *FileDownloadTool) RiskLevel() string { return "L2" }

func (t *FileDownloadTool) Parameters() *tools.ParamSchema {
	return &tools.ParamSchema{
		Type: "object",
		Properties: map[string]tools.ParamProperty{
			"path": {
				Type:        "string",
				Description: "File path to download",
			},
			"upload_url": {
				Type:        "string",
				Description: "MinIO presigned upload URL",
			},
			"max_size_mb": {
				Type:        "number",
				Description: "Max file size limit in MB, default 100",
			},
		},
		Required: []string{"path", "upload_url"},
	}
}

func (t *FileDownloadTool) NeedsApproval(map[string]interface{}) bool { return true }

const defaultMaxSizeMB = 100

// Path expansion and sensitive-path detection are in pathutil.go.

func failResult(toolName, err string, start time.Time) *tools.ToolResult {
	return &tools.ToolResult{
		ToolName:   toolName,
		Status:     "failed",
		Error:      err,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func (t *FileDownloadTool) Execute(ctx context.Context, params map[string]interface{}) (*tools.ToolResult, error) {
	start := time.Now()

	path, _ := params["path"].(string)
	if path == "" {
		return failResult(t.Name(), "missing required parameter: path", start), nil
	}
	uploadURL, _ := params["upload_url"].(string)
	if uploadURL == "" {
		return failResult(t.Name(), "missing required parameter: upload_url", start), nil
	}

	maxSizeMB := float64(defaultMaxSizeMB)
	if v, ok := params["max_size_mb"].(float64); ok && v > 0 {
		maxSizeMB = v
	}
	maxBytes := int64(maxSizeMB * 1024 * 1024)
	if maxBytes <= 0 {
		return failResult(t.Name(), "invalid max_size_mb", start), nil
	}

	uploadParsed, err := url.Parse(uploadURL)
	if err != nil || uploadParsed.Scheme == "" || (uploadParsed.Scheme != "http" && uploadParsed.Scheme != "https") {
		return failResult(t.Name(), "invalid upload_url: must be an http(s) URL", start), nil
	}

	absPath, err := ResolveSafePath(path)
	if err != nil {
		return failResult(t.Name(), fmt.Sprintf("path error: %v", err), start), nil
	}

	li, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return failResult(t.Name(), fmt.Sprintf("file does not exist: %s", absPath), start), nil
		}
		return failResult(t.Name(), fmt.Sprintf("cannot access path: %v", err), start), nil
	}
	if li.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			return failResult(t.Name(), fmt.Sprintf("cannot resolve symlink: %v", err), start), nil
		}
		resAbs, err := filepath.Abs(filepath.Clean(resolved))
		if err != nil {
			return failResult(t.Name(), fmt.Sprintf("could not resolve symlink target: %v", err), start), nil
		}
		if IsSensitivePath(resAbs) {
			return failResult(t.Name(), "access denied: symlink target is a sensitive path", start), nil
		}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return failResult(t.Name(), fmt.Sprintf("cannot stat file: %v", err), start), nil
	}
	if info.IsDir() {
		return failResult(t.Name(), "path is a directory, not a file", start), nil
	}
	if !info.Mode().IsRegular() {
		return failResult(t.Name(), "path is not a regular file", start), nil
	}

	if info.Size() > maxBytes {
		return failResult(t.Name(), fmt.Sprintf("file size %d bytes exceeds limit (%.0f MB)", info.Size(), maxSizeMB), start), nil
	}

	f, err := os.Open(absPath)
	if err != nil {
		return failResult(t.Name(), fmt.Sprintf("cannot open file: %v", err), start), nil
	}
	defer f.Close()

	head := make([]byte, 512)
	n, readErr := io.ReadFull(f, head)
	switch readErr {
	case nil:
	case io.EOF, io.ErrUnexpectedEOF:
		head = head[:n]
	default:
		return failResult(t.Name(), fmt.Sprintf("cannot read file: %v", readErr), start), nil
	}
	contentType := http.DetectContentType(head)
	if _, err := f.Seek(0, 0); err != nil {
		return failResult(t.Name(), fmt.Sprintf("cannot rewind file: %v", err), start), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
	if err != nil {
		return failResult(t.Name(), fmt.Sprintf("cannot build upload request: %v", err), start), nil
	}
	if contentType != "" && contentType != "application/octet-stream" {
		req.Header.Set("Content-Type", contentType)
	} else {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	req.ContentLength = info.Size()

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return failResult(t.Name(), fmt.Sprintf("upload request failed: %v", err), start), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return failResult(t.Name(), fmt.Sprintf("upload failed: HTTP %s - %s", resp.Status, msg), start), nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	elapsed := time.Since(start)
	return &tools.ToolResult{
		ToolName: t.Name(),
		Status:   "succeeded",
		Summary:  fmt.Sprintf("Uploaded '%s' (%d bytes) to object storage", absPath, info.Size()),
		Output: map[string]interface{}{
			"path":         absPath,
			"file_size":    info.Size(),
			"upload_url":   uploadURL,
			"content_type": contentType,
		},
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
