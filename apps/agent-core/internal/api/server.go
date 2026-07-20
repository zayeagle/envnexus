package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/activation"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/agent"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/device"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/diagnosis"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/governance"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/policy"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/store"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/remediation"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/updater"
)

type LocalServer struct {
	port              int
	server            *http.Server
	identityManager   *device.IdentityManager
	policyEngine      *policy.Engine
	diagEngine        *diagnosis.Engine
	llmRouter         *router.Router
	toolRegistry      *tools.Registry
	activationMgr     *activation.Manager
	governanceEngine  *governance.Engine
	localStore        *store.Store
	startTime         time.Time
	platformConnected bool
	agentUpdater      *updater.Updater
	chatApprovals     sync.Map
	chatCancelFuncs   sync.Map
	chatAutoApprove   sync.Map
	planStore         *PlanStore
	planApprovals     sync.Map
	stepApprovals     sync.Map
	remediationPlanner *remediation.Planner
}

func NewLocalServer(port int, identityManager *device.IdentityManager, policyEngine *policy.Engine, diagEngine *diagnosis.Engine, llmRouter *router.Router, toolRegistry *tools.Registry) *LocalServer {
	return &LocalServer{
		port:            port,
		identityManager: identityManager,
		policyEngine:    policyEngine,
		diagEngine:      diagEngine,
		llmRouter:       llmRouter,
		toolRegistry:    toolRegistry,
		startTime:       time.Now(),
	}
}

func (s *LocalServer) SetActivationManager(mgr *activation.Manager) {
	s.activationMgr = mgr
}

func (s *LocalServer) SetStore(st *store.Store) {
	s.localStore = st
}

func (s *LocalServer) SetPlatformConnected(connected bool) {
	s.platformConnected = connected
}

func (s *LocalServer) SetGovernanceEngine(e *governance.Engine) {
	s.governanceEngine = e
}

func (s *LocalServer) SetUpdater(u *updater.Updater) {
	s.agentUpdater = u
}

func (s *LocalServer) SetRemediationPlanner(p *remediation.Planner) {
	s.remediationPlanner = p
	if s.planStore == nil {
		s.planStore = NewPlanStore()
	}
}

func (s *LocalServer) Start() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	api := router.Group("/local/v1")
	{
		api.GET("/runtime/status", s.handleRuntimeStatus)
		api.GET("/device", s.handleDevice)
		api.GET("/approvals/pending", s.handleGetPendingApprovals)
		api.POST("/approvals/:id/resolve", s.handleResolveApproval)
		api.POST("/diagnose", s.handleDiagnose)
		api.POST("/chat", s.handleChat)
		api.POST("/chat/approve", s.handleChatApprove)
		api.POST("/chat/cancel", s.handleChatCancel)
		api.POST("/chat/auto-approve", s.handleChatAutoApprove)
		api.POST("/diagnostics/export", s.handleDiagnosticsExport)
		api.GET("/sessions/recent", s.handleRecentSessions)
		api.GET("/update/status", s.handleUpdateStatus)
		api.POST("/update/check", s.handleUpdateCheck)
		api.POST("/update/download", s.handleUpdateDownload)
		api.POST("/update/apply", s.handleUpdateApply)

		s.RegisterPlanRoutes(api)
		s.RegisterWatchlistRoutes(api)
		s.RegisterFileRoutes(api)
	}

	s.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler: router,
	}

	slog.Info("Starting local API server", "addr", s.server.Addr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Local API server failed", "error", err)
			os.Exit(1)
		}
	}()

	return nil
}

func (s *LocalServer) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *LocalServer) handleRuntimeStatus(c *gin.Context) {
	resp := gin.H{
		"status":             "running",
		"uptime_ms":          time.Since(s.startTime).Milliseconds(),
		"started":            s.startTime.Format(time.RFC3339),
		"platform_connected": s.platformConnected,
	}

	if s.activationMgr != nil {
		st := s.activationMgr.GetStatus()
		resp["activation"] = gin.H{
			"activated":   st.Activated,
			"device_code": st.DeviceCode,
			"mode":        st.ActivationMode,
			"package_id":  st.PackageID,
			"tenant_id":   st.TenantID,
		}
		resp["device_code"] = st.DeviceCode
	}

	if id, err := s.identityManager.GetOrCreateDeviceID(); err == nil {
		resp["device_id"] = id
	}

	resp["pending_approvals"] = len(s.policyEngine.GetPending())

	if s.governanceEngine != nil {
		resp["governance"] = s.governanceEngine.GetStatus()
	}

	c.JSON(http.StatusOK, resp)
}

func (s *LocalServer) handleDevice(c *gin.Context) {
	deviceID, err := s.identityManager.GetOrCreateDeviceID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get device identity"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device_id": deviceID,
	})
}

func (s *LocalServer) handleGetPendingApprovals(c *gin.Context) {
	pending := s.policyEngine.GetPending()
	c.JSON(http.StatusOK, gin.H{
		"pending_approvals": pending,
	})
}

type ResolveApprovalRequest struct {
	Approved bool `json:"approved"`
}

func (s *LocalServer) handleResolveApproval(c *gin.Context) {
	id := c.Param("id")

	var req ResolveApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.policyEngine.Resolve(id, req.Approved); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "resolved"})
}

type DiagnoseRequest struct {
	SessionID string `json:"session_id"`
	Intent    string `json:"intent" binding:"required"`
}

func (s *LocalServer) handleDiagnose(c *gin.Context) {
	var req DiagnoseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.SessionID == "" {
		req.SessionID = "local"
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	writeSSE := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, jsonData)
		flusher.Flush()
	}

	onProgress := func(step string, detail string) {
		writeSSE("progress", gin.H{"step": step, "detail": detail})
	}

	diagCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, err := s.diagEngine.RunDiagnosisWithProgress(diagCtx, req.SessionID, req.Intent, onProgress)
	if err != nil {
		writeSSE("error", gin.H{"error": err.Error()})
		return
	}

	writeSSE("result", gin.H{"diagnosis": result})
}

func (s *LocalServer) handleDiagnosticsExport(c *gin.Context) {
	report := gin.H{
		"agent_version": "0.1.0",
		"export_time":   time.Now().UTC().Format(time.RFC3339),
		"uptime_ms":     time.Since(s.startTime).Milliseconds(),
		"runtime_status": gin.H{
			"status":  "running",
			"started": s.startTime.Format(time.RFC3339),
		},
	}

	deviceID, err := s.identityManager.GetOrCreateDeviceID()
	if err == nil {
		report["device_id"] = deviceID
	}

	report["pending_approvals"] = s.policyEngine.GetPending()

	c.JSON(http.StatusOK, gin.H{"diagnostic_bundle": report})
}

func (s *LocalServer) handleRecentSessions(c *gin.Context) {
	if s.localStore == nil {
		c.JSON(http.StatusOK, gin.H{"sessions": []any{}})
		return
	}

	sessions, err := s.localStore.ListRecentSessions(50)
	if err != nil {
		slog.Warn("Failed to list recent sessions", "error", err)
		c.JSON(http.StatusOK, gin.H{"sessions": []any{}})
		return
	}

	result := make([]gin.H, 0, len(sessions))
	for _, sess := range sessions {
		result = append(result, gin.H{
			"id":         sess.ID,
			"tenant_id":  sess.TenantID,
			"device_id":  sess.DeviceID,
			"status":     sess.Status,
			"intent":     sess.Intent,
			"started_at": sess.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"sessions": result})
}

type ChatRequest struct {
	Messages      []ChatMessage `json:"messages" binding:"required"`
	MaxIterations int           `json:"max_iterations,omitempty"`
}

type ChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// extractTextAndParts inspects raw content. Returns (text, parts, hasImages).
// - If raw is a string, returns (text, nil, false).
// - If raw is an array of ContentPart, returns (concatenated text, parts, hasImages).
func extractTextAndParts(raw json.RawMessage) (string, []router.ContentPart, bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	var asStr string
	if err := json.Unmarshal(raw, &asStr); err == nil {
		return asStr, nil, false
	}
	var parts []router.ContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var textBuf strings.Builder
		hasImg := false
		for _, p := range parts {
			if p.Type == "text" {
				if textBuf.Len() > 0 {
					textBuf.WriteString("\n")
				}
				textBuf.WriteString(p.Text)
			} else if p.Type == "image_url" {
				hasImg = true
			}
		}
		return textBuf.String(), parts, hasImg
	}
	return "", nil, false
}

func (s *LocalServer) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s.llmRouter == nil || s.toolRegistry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM router or tool registry not configured"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	writeSSE := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, jsonData)
		flusher.Flush()
	}

	messages := make([]router.Message, len(req.Messages))
	hasAnyImage := false
	mmMessages := make([]router.MultimodalMessage, len(req.Messages))
	for i, m := range req.Messages {
		text, parts, hasImg := extractTextAndParts(m.Content)
		messages[i] = router.Message{Role: m.Role, Content: text}
		if parts != nil {
			mmMessages[i] = router.MultimodalMessage{Role: m.Role, ContentParts: parts}
		} else {
			mmMessages[i] = router.MultimodalMessage{
				Role:         m.Role,
				ContentParts: []router.ContentPart{router.NewTextPart(text)},
			}
		}
		if hasImg {
			hasAnyImage = true
		}
	}

	chatCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	chatSessionID := fmt.Sprintf("chat-session-%d", time.Now().UnixNano())
	s.chatCancelFuncs.Store(chatSessionID, cancel)
	defer s.chatCancelFuncs.Delete(chatSessionID)
	defer s.chatAutoApprove.Delete(chatSessionID)

	writeSSE("session", gin.H{"session_id": chatSessionID})

	maxIter := agent.DefaultMaxIterations
	if req.MaxIterations > 0 {
		maxIter = req.MaxIterations
	}

	if hasAnyImage {
		mmReq := &router.MultimodalRequest{Messages: mmMessages}
		mmResp, err := s.llmRouter.CompleteMultimodal(chatCtx, mmReq)
		if err != nil {
			if chatCtx.Err() == context.Canceled {
				writeSSE("cancelled", gin.H{"message": "Chat cancelled by user"})
				return
			}
			writeSSE("error", gin.H{"error": err.Error()})
			return
		}
		writeSSE("message", gin.H{"content": mmResp.Content})
		writeSSE("done", gin.H{"content": mmResp.Content})
		return
	}

	loop := agent.NewLoop(
		s.llmRouter,
		s.toolRegistry,
		agent.WithMaxIterations(maxIter),
		agent.WithEventHandler(func(evt agent.Event) {
			writeSSE(string(evt.Type), evt.Content)
		}),
		agent.WithApprovalHandler(func(req agent.ApprovalRequest) agent.ApprovalResponse {
			if _, autoApproved := s.chatAutoApprove.Load(chatSessionID); autoApproved {
				slog.Info("[api] Auto-approved tool execution", "session_id", chatSessionID, "tool", req.ToolName)
				writeSSE("auto_approved", gin.H{
					"tool_name": req.ToolName,
					"params":    req.Params,
				})
				return agent.ApprovalResponse{Approved: true}
			}

			approvalID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
			ch := make(chan bool, 1)
			s.chatApprovals.Store(approvalID, ch)
			defer s.chatApprovals.Delete(approvalID)

			writeSSE("approval_required", gin.H{
				"approval_id": approvalID,
				"tool_name":   req.ToolName,
				"description": req.Description,
				"risk_level":  req.RiskLevel,
				"params":      req.Params,
			})

			select {
			case approved := <-ch:
				return agent.ApprovalResponse{Approved: approved}
			case <-chatCtx.Done():
				return agent.ApprovalResponse{Approved: false}
			case <-time.After(5 * time.Minute):
				return agent.ApprovalResponse{Approved: false}
			}
		}),
	)

	content, err := loop.Run(chatCtx, messages)
	if err != nil {
		if chatCtx.Err() == context.Canceled {
			writeSSE("cancelled", gin.H{"message": "Chat cancelled by user"})
			return
		}
		writeSSE("error", gin.H{"error": err.Error()})
		return
	}

	writeSSE("done", gin.H{"content": content})
}

type ChatApproveRequest struct {
	ApprovalID string `json:"approval_id" binding:"required"`
	Approved   bool   `json:"approved"`
}

func (s *LocalServer) handleChatApprove(c *gin.Context) {
	var req ChatApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	val, ok := s.chatApprovals.Load(req.ApprovalID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "approval not found or expired"})
		return
	}

	ch := val.(chan bool)
	select {
	case ch <- req.Approved:
		c.JSON(http.StatusOK, gin.H{"status": "resolved"})
	default:
		c.JSON(http.StatusConflict, gin.H{"error": "approval already resolved"})
	}
}

type ChatCancelRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

func (s *LocalServer) handleChatCancel(c *gin.Context) {
	var req ChatCancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	val, ok := s.chatCancelFuncs.Load(req.SessionID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat session not found or already finished"})
		return
	}

	cancelFunc := val.(context.CancelFunc)
	cancelFunc()
	slog.Info("[api] Chat session cancelled", "session_id", req.SessionID)
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

type ChatAutoApproveRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Enabled   bool   `json:"enabled"`
}

func (s *LocalServer) handleChatAutoApprove(c *gin.Context) {
	var req ChatAutoApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Enabled {
		s.chatAutoApprove.Store(req.SessionID, true)
		slog.Info("[api] Auto-approve enabled for chat session", "session_id", req.SessionID)
	} else {
		s.chatAutoApprove.Delete(req.SessionID)
		slog.Info("[api] Auto-approve disabled for chat session", "session_id", req.SessionID)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "auto_approve": req.Enabled})
}

// ── Self-update API ─────────────────────────────────────────────────────────

func (s *LocalServer) handleUpdateStatus(c *gin.Context) {
	if s.agentUpdater == nil {
		c.JSON(http.StatusOK, gin.H{
			"state":           "disabled",
			"current_version": "unknown",
			"message":         "updater not initialized (standalone mode or no platform connection)",
		})
		return
	}
	c.JSON(http.StatusOK, s.agentUpdater.GetStatus())
}

func (s *LocalServer) handleUpdateCheck(c *gin.Context) {
	if s.agentUpdater == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "updater not available"})
		return
	}
	info, err := s.agentUpdater.CheckForUpdate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

func (s *LocalServer) handleUpdateDownload(c *gin.Context) {
	if s.agentUpdater == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "updater not available"})
		return
	}
	info, err := s.agentUpdater.CheckForUpdate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !info.HasUpdate {
		c.JSON(http.StatusOK, gin.H{"status": "no_update", "message": "already on latest version"})
		return
	}
	path, err := s.agentUpdater.DownloadUpdate(c.Request.Context(), info)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "downloaded", "path": path, "version": info.LatestVersion})
}

func (s *LocalServer) handleUpdateApply(c *gin.Context) {
	if s.agentUpdater == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "updater not available"})
		return
	}
	installedPath, err := s.agentUpdater.ApplyUpdate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":            "applied",
		"message":           "binary replaced, restart required",
		"installed_binary":  filepath.Clean(installedPath),
	})
}
