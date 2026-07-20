package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/audit"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/diagnosis"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/policy"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/store"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

const (
	maxLogRemoteCommandRunes = 256 * 1024
	maxLogToolOutputRunes    = 128 * 1024
)

type EventEnvelope struct {
	EventID   string      `json:"event_id"`
	EventType string      `json:"event_type"`
	TenantID  string      `json:"tenant_id,omitempty"`
	DeviceID  string      `json:"device_id,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Timestamp string      `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

type SessionTokenProvider interface {
	GetSessionToken(ctx context.Context, deviceID, tenantID string) (string, error)
}

type WSClient struct {
	serverURL     string
	deviceID      string
	tenantID      string
	sessionToken  string
	tokenProvider SessionTokenProvider
	conn          *websocket.Conn
	mu            sync.Mutex
	done          chan struct{}
	sendCh        chan []byte
	registry      *tools.Registry
	auditClient   *audit.Client
	policyEngine  *policy.Engine
	diagEngine    *diagnosis.Engine
	localStore    *store.Store
}

func NewWSClient(serverURL, deviceID, tenantID string, tokenProvider SessionTokenProvider, registry *tools.Registry, auditClient *audit.Client, policyEngine *policy.Engine, diagEngine *diagnosis.Engine, localStore *store.Store) *WSClient {
	return &WSClient{
		serverURL:     serverURL,
		deviceID:      deviceID,
		tenantID:      tenantID,
		tokenProvider: tokenProvider,
		done:          make(chan struct{}),
		sendCh:        make(chan []byte, 256),
		registry:      registry,
		auditClient:   auditClient,
		policyEngine:  policyEngine,
		diagEngine:    diagEngine,
		localStore:    localStore,
	}
}

func (c *WSClient) Start(ctx context.Context) {
	go c.connectLoop(ctx)
}

func (c *WSClient) Stop() {
	close(c.done)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *WSClient) connectLoop(ctx context.Context) {
	backoff := time.Second * 2
	maxBackoff := time.Minute

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		if c.tokenProvider != nil {
			token, err := c.tokenProvider.GetSessionToken(ctx, c.deviceID, c.tenantID)
			if err != nil {
				slog.Warn("[ws] Failed to obtain session token, retrying", "error", err, "backoff", backoff)
				time.Sleep(backoff)
				backoff = backoff * 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
			c.sessionToken = token
		}

		dialURL := c.buildDialURL()
		slog.Info("[ws] Connecting to session-gateway", "dial_url", dialURL)
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, dialURL, nil)
		if err != nil {
			slog.Warn("[ws] Dial error, retrying", "error", err, "backoff", backoff)
			time.Sleep(backoff)
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		backoff = time.Second * 2
		slog.Info("[ws] Connected to session-gateway")

		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()

		errChan := make(chan error, 2)
		go c.readPump(errChan)
		go c.writePump(errChan)

		err = <-errChan
		slog.Warn("[ws] Connection lost, reconnecting", "error", err)

		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()

		time.Sleep(time.Second)
	}
}

func (c *WSClient) buildDialURL() string {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		slog.Warn("[ws] Invalid server URL, using as-is", "error", err)
		return c.serverURL
	}

	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws/v1/sessions/" + c.deviceID
	} else if !strings.HasSuffix(u.Path, "/"+c.deviceID) {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + c.deviceID
	}

	q := u.Query()
	if c.sessionToken != "" {
		q.Set("token", c.sessionToken)
	}
	q.Set("tenant_id", c.tenantID)
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *WSClient) readPump(errChan chan<- error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			errChan <- fmt.Errorf("read error: %w", err)
			return
		}

		var evt EventEnvelope
		if err := json.Unmarshal(message, &evt); err != nil {
			slog.Warn("[ws] Invalid message", "error", err)
			continue
		}

		c.handleServerEvent(evt)
	}
}

func (c *WSClient) writePump(errChan chan<- error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case msg := <-c.sendCh:
			c.mu.Lock()
			err := conn.WriteMessage(websocket.TextMessage, msg)
			c.mu.Unlock()
			if err != nil {
				errChan <- fmt.Errorf("write error: %w", err)
				return
			}
		case <-ticker.C:
			c.mu.Lock()
			err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
			c.mu.Unlock()
			if err != nil {
				errChan <- fmt.Errorf("ping error: %w", err)
				return
			}
		}
	}
}

func (c *WSClient) handleServerEvent(evt EventEnvelope) {
	switch evt.EventType {
	case "session.created":
		slog.Info("[ws] Session created", "session_id", evt.SessionID)
		c.handleSessionCreated(evt)

	case "diagnosis.started":
		slog.Info("[ws] Diagnosis started", "session_id", evt.SessionID)

	case "diagnosis.completed":
		slog.Info("[ws] Diagnosis completed", "session_id", evt.SessionID)

	case "approval.requested":
		slog.Info("[ws] Approval requested", "session_id", evt.SessionID)
		c.handleApprovalRequested(evt)

	case "approval.expired":
		slog.Warn("[ws] Approval expired", "session_id", evt.SessionID)

	case "tool.started":
		slog.Info("[ws] Tool started", "session_id", evt.SessionID)
		c.handleToolStarted(evt)

	case "tool.completed":
		slog.Info("[ws] Tool completed", "session_id", evt.SessionID)

	case "session.completed":
		slog.Info("[ws] Session completed", "session_id", evt.SessionID)

	case "command.execute":
		c.handleCommandExecute(evt)

	case "file.execute":
		c.handleFileExecute(evt)

	case "heartbeat.pong":
		slog.Info("[ws] Heartbeat pong received")

	default:
		slog.Info("[ws] Unhandled event", "event_type", evt.EventType)
	}
}

func (c *WSClient) handleSessionCreated(evt EventEnvelope) {
	payloadMap, ok := evt.Payload.(map[string]interface{})
	if !ok {
		return
	}
	initialMessage, _ := payloadMap["initial_message"].(string)
	if initialMessage == "" {
		return
	}

	// 保存会话到本地存储
	if c.localStore != nil {
		err := c.localStore.SaveSession(evt.SessionID, c.tenantID, c.deviceID, "created", initialMessage)
		if err != nil {
			slog.Warn("[ws] Failed to save session to local store", "error", err)
		} else {
			slog.Info("[ws] Session saved to local store", "session_id", evt.SessionID)
		}
	}

	c.SendEvent(EventEnvelope{
		EventID:   generateEventID(),
		EventType: "diagnosis.started",
		DeviceID:  c.deviceID,
		SessionID: evt.SessionID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   map[string]interface{}{"intent": initialMessage},
	})

	go func() {
		result, err := c.diagEngine.RunDiagnosis(context.Background(), evt.SessionID, initialMessage)
		if err != nil {
			slog.Error("[ws] Diagnosis failed", "session_id", evt.SessionID, "error", err)
			c.SendEvent(EventEnvelope{
				EventID:   generateEventID(),
				EventType: "diagnosis.completed",
				DeviceID:  c.deviceID,
				SessionID: evt.SessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Payload:   map[string]interface{}{"error": err.Error()},
			})
			// 更新会话状态为失败
			if c.localStore != nil {
				err := c.localStore.SaveSession(evt.SessionID, c.tenantID, c.deviceID, "failed", initialMessage)
				if err != nil {
					slog.Warn("[ws] Failed to update session status", "error", err)
				}
			}
			return
		}

		c.SendEvent(EventEnvelope{
			EventID:   generateEventID(),
			EventType: "diagnosis.completed",
			DeviceID:  c.deviceID,
			SessionID: evt.SessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Payload:   result,
		})

		// 更新会话状态为完成
		if c.localStore != nil {
			err := c.localStore.SaveSession(evt.SessionID, c.tenantID, c.deviceID, "completed", initialMessage)
			if err != nil {
				slog.Warn("[ws] Failed to update session status", "error", err)
			}
		}

		if result.ApprovalRequired && len(result.RecommendedActions) > 0 {
			for _, action := range result.RecommendedActions {
				c.SendEvent(EventEnvelope{
					EventID:   generateEventID(),
					EventType: "approval.requested",
					DeviceID:  c.deviceID,
					SessionID: evt.SessionID,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Payload: map[string]interface{}{
						"tool_name":   action.ToolName,
						"description": action.Description,
						"risk_level":  action.RiskLevel,
						"params":      action.Params,
					},
				})
			}
		}

		c.auditClient.ReportEvent(context.Background(), "diagnosis.completed", evt.SessionID, map[string]interface{}{
			"problem_type": result.ProblemType,
			"confidence":   result.Confidence,
			"findings":     len(result.Findings),
			"actions":      len(result.RecommendedActions),
		})
	}()
}

func (c *WSClient) handleToolStarted(evt EventEnvelope) {
	payloadMap, ok := evt.Payload.(map[string]interface{})
	if !ok {
		return
	}
	toolName, _ := payloadMap["tool_name"].(string)
	if toolName == "" {
		return
	}

	tool, found := c.registry.Get(toolName)
	if !found {
		slog.Warn("[ws] remote tool: not registered", "session_id", evt.SessionID, "tool_name", toolName)
		c.sendToolResult(evt.SessionID, toolName, nil, fmt.Errorf("tool not found: %s", toolName))
		return
	}

	params, _ := payloadMap["params"].(map[string]interface{})
	paramsLog := mustJSONForLog(params)

	slog.Info("[ws] remote tool.execute",
		"session_id", evt.SessionID,
		"tool_name", toolName,
		"params", paramsLog,
	)

	toolCtx, toolCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer toolCancel()

	approved, err := c.policyEngine.Check(toolCtx, tool, params)
	if err != nil || !approved {
		reason := ""
		if err != nil {
			reason = err.Error()
		} else {
			reason = "not approved"
		}
		slog.Info("[ws] remote tool.denied",
			"session_id", evt.SessionID,
			"tool_name", toolName,
			"params", paramsLog,
			"reason", reason,
		)
		c.auditClient.ReportEvent(context.Background(), "tool.denied", evt.SessionID, map[string]interface{}{
			"tool_name": toolName,
			"reason":    fmt.Sprintf("%v", err),
		})
		c.sendToolResult(evt.SessionID, toolName, nil, fmt.Errorf("denied by policy"))
		return
	}

	result, err := tool.Execute(toolCtx, params)

	eventType := "tool.completed"
	if err != nil || (result != nil && result.Status == "failed") {
		eventType = "tool.failed"
	}

	if err != nil {
		slog.Info("[ws] remote tool.execute result",
			"session_id", evt.SessionID,
			"tool_name", toolName,
			"event_type", eventType,
			"invoke_error", err.Error(),
		)
	} else if result != nil {
		outStr := stringFromToolOutput(result.Output)
		slog.Info("[ws] remote tool.execute result",
			"session_id", evt.SessionID,
			"tool_name", toolName,
			"event_type", eventType,
			"status", result.Status,
			"duration_ms", result.DurationMs,
			"summary", result.Summary,
			"output", truncateRunes(outStr, maxLogToolOutputRunes),
			"output_runes", len([]rune(outStr)),
			"result_error", result.Error,
		)
	}

	c.auditClient.ReportEvent(context.Background(), eventType, evt.SessionID, map[string]interface{}{
		"tool_name": toolName,
		"result":    result,
	})

	c.sendToolResult(evt.SessionID, toolName, result, err)
}

func (c *WSClient) handleApprovalRequested(evt EventEnvelope) {
	c.auditClient.ReportEvent(context.Background(), "approval.requested", evt.SessionID, evt.Payload)
}

func (c *WSClient) sendToolResult(sessionID, toolName string, result *tools.ToolResult, err error) {
	payload := map[string]interface{}{
		"tool_name": toolName,
	}
	evtType := "tool.completed"

	if err != nil {
		evtType = "tool.failed"
		payload["status"] = "failed"
		payload["error"] = err.Error()
	} else if result != nil {
		payload["status"] = result.Status
		payload["output"] = result.Output
		payload["summary"] = result.Summary
		payload["duration_ms"] = result.DurationMs
		if result.Error != "" {
			payload["error"] = result.Error
		}
		if result.Status == "failed" {
			evtType = "tool.failed"
		}
	}

	envelope := EventEnvelope{
		EventID:   generateEventID(),
		EventType: evtType,
		DeviceID:  c.deviceID,
		SessionID: sessionID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}

	data, _ := json.Marshal(envelope)
	select {
	case c.sendCh <- data:
	default:
		slog.Warn("[ws] Send buffer full, dropping tool result")
	}
}

func (c *WSClient) SendEvent(evt EventEnvelope) {
	if evt.EventID == "" {
		evt.EventID = generateEventID()
	}
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	data, _ := json.Marshal(evt)
	select {
	case c.sendCh <- data:
	default:
	}
}

func generateEventID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *WSClient) handleFileExecute(evt EventEnvelope) {
	payloadMap, ok := evt.Payload.(map[string]interface{})
	if !ok {
		slog.Warn("[ws] file.execute: invalid payload")
		return
	}

	requestID, _ := payloadMap["request_id"].(string)
	path, _ := payloadMap["path"].(string)
	action, _ := payloadMap["action"].(string)

	if requestID == "" || path == "" || action == "" {
		slog.Warn("[ws] file.execute: missing request_id, path, or action")
		return
	}

	slog.Info("[ws] file.execute",
		"request_id", requestID,
		"path", path,
		"action", action,
	)

	go func() {
		var toolName string
		var params map[string]interface{}
		var resultEventType string
		var downloadURL string

		switch action {
		case "browse":
			toolName = "read_dir_list"
			params = map[string]interface{}{"path": path}
			resultEventType = "file.browse_result"
		case "preview":
			toolName = "read_file_tail"
			params = map[string]interface{}{"path": path, "lines": float64(100)}
			resultEventType = "file.preview_result"
		case "download":
			uploadURL, _ := payloadMap["upload_url"].(string)
			downloadURL, _ = payloadMap["download_url"].(string)
			if uploadURL == "" {
				c.sendFileResult(requestID, "file.download_result", map[string]interface{}{
					"request_id": requestID,
					"status":     "failed",
					"error":      "download action requires upload_url (MinIO not configured on platform)",
				})
				return
			}
			toolName = "file_download"
			params = map[string]interface{}{"path": path, "upload_url": uploadURL}
			resultEventType = "file.download_result"
		default:
			slog.Warn("[ws] file.execute: unsupported action", "action", action)
			return
		}

		tool, found := c.registry.Get(toolName)
		if !found {
			slog.Warn("[ws] file.execute: tool not found", "tool_name", toolName)
			c.sendFileResult(requestID, resultEventType, map[string]interface{}{
				"request_id": requestID,
				"status":     "failed",
				"error":      fmt.Sprintf("tool %s not registered", toolName),
			})
			return
		}

		timeout := 30 * time.Second
		if action == "download" {
			timeout = 10 * time.Minute
		}
		toolCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		result, err := tool.Execute(toolCtx, params)

		payload := map[string]interface{}{
			"request_id": requestID,
		}
		if err != nil {
			payload["status"] = "failed"
			payload["error"] = err.Error()
		} else if result != nil {
			payload["status"] = result.Status
			payload["output"] = result.Output
			payload["summary"] = result.Summary
			payload["duration_ms"] = result.DurationMs
			if result.Error != "" {
				payload["error"] = result.Error
			}
		}

		if downloadURL != "" && (payload["status"] == "succeeded") {
			payload["download_url"] = downloadURL
		}

		c.sendFileResult(requestID, resultEventType, payload)

		slog.Info("[ws] file.execute completed",
			"request_id", requestID,
			"action", action,
			"result_type", resultEventType,
		)
	}()
}

func (c *WSClient) sendFileResult(requestID, eventType string, payload map[string]interface{}) {
	c.SendEvent(EventEnvelope{
		EventID:   generateEventID(),
		EventType: eventType,
		DeviceID:  c.deviceID,
		SessionID: requestID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	})
}

func (c *WSClient) handleCommandExecute(evt EventEnvelope) {
	payloadMap, ok := evt.Payload.(map[string]interface{})
	if !ok {
		slog.Warn("[ws] command.execute: invalid payload")
		return
	}

	taskID, _ := payloadMap["task_id"].(string)
	executionID, _ := payloadMap["execution_id"].(string)
	commandType, _ := payloadMap["command_type"].(string)
	commandPayload, _ := payloadMap["command_payload"].(string)

	if executionID == "" || commandPayload == "" {
		slog.Warn("[ws] command.execute: missing execution_id or command_payload")
		return
	}

	cmdForLog, cmdTrunc := truncateRunesWithFlag(commandPayload, maxLogRemoteCommandRunes)
	slog.Info("[ws] remote command.execute",
		"execution_id", executionID,
		"task_id", taskID,
		"command_type", commandType,
		"command_runes", len([]rune(commandPayload)),
		"command_truncated", cmdTrunc,
		"command", cmdForLog,
	)

	go func() {
		start := time.Now()
		var status, output, errMsg string
		var exitCode int

		switch commandType {
		case "shell":
			status, output, errMsg, exitCode = executeShellCommand(commandPayload)
		default:
			status = "failed"
			errMsg = fmt.Sprintf("unsupported command type: %s", commandType)
			exitCode = -1
		}

		durationMs := int(time.Since(start).Milliseconds())

		resultPayload := map[string]interface{}{
			"execution_id": executionID,
			"status":       status,
			"output":       output,
			"exit_code":    exitCode,
			"duration_ms":  durationMs,
		}
		if errMsg != "" {
			resultPayload["error"] = errMsg
		}

		c.SendEvent(EventEnvelope{
			EventID:   generateEventID(),
			EventType: "command.result",
			DeviceID:  c.deviceID,
			SessionID: taskID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Payload:   resultPayload,
		})

		outForLog, outTrunc := truncateRunesWithFlag(output, maxLogToolOutputRunes)
		errForLog, errTrunc := truncateRunesWithFlag(errMsg, 32*1024)
		slog.Info("[ws] remote command.execute result",
			"execution_id", executionID,
			"task_id", taskID,
			"status", status,
			"exit_code", exitCode,
			"duration_ms", durationMs,
			"stdout", outForLog,
			"stdout_runes", len([]rune(output)),
			"stdout_truncated", outTrunc,
			"stderr_or_err", errForLog,
			"stderr_truncated", errTrunc,
		)
	}()
}

func findPowerShellExe() string {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	return "powershell"
}

func executeShellCommand(command string) (status, output, errMsg string, exitCode int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		psExe := findPowerShellExe()
		cmd = exec.CommandContext(ctx, psExe, "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	maxOutput := 64 * 1024
	out := stdout.String()
	if len(out) > maxOutput {
		out = out[:maxOutput] + "\n... (truncated)"
	}

	if ctx.Err() == context.DeadlineExceeded {
		return "timeout", out, "command execution timed out (5m)", -1
	}

	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		combinedErr := stderr.String()
		if combinedErr == "" {
			combinedErr = err.Error()
		}
		return "failed", out, combinedErr, exitCode
	}

	return "succeeded", out, "", 0
}

func stringFromToolOutput(out interface{}) string {
	if out == nil {
		return ""
	}
	switch v := out.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", out)
		}
		return string(b)
	}
}

func mustJSONForLog(v interface{}) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(b)
}

func truncateRunes(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

func truncateRunesWithFlag(s string, maxRunes int) (text string, truncated bool) {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s, false
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s, false
	}
	return string(r[:maxRunes]) + "…", true
}
