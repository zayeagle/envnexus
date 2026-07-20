package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/session"
)

type SessionHandler struct {
	sessionService *session.Service
}

func NewSessionHandler(sessionService *session.Service) *SessionHandler {
	return &SessionHandler{sessionService: sessionService}
}

func (h *SessionHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/tenants/:tenantId/sessions", h.List)
	router.GET("/tenants/:tenantId/sessions/:sessionId", h.Get)
	router.GET("/tenants/:tenantId/sessions/active-count", h.GetActiveCount)
	router.POST("/tenants/:tenantId/sessions/batch-delete", h.BatchDelete)
	router.POST("/sessions", h.Create)
	router.POST("/sessions/:sessionId/approve", h.Approve)
	router.POST("/sessions/:sessionId/deny", h.Deny)
	router.POST("/sessions/:sessionId/abort", h.Abort)
	router.GET("/sessions/:sessionId/tool-invocations", h.ListToolInvocations)
}

func (h *SessionHandler) List(c *gin.Context) {
	tenantID := c.Param("tenantId")
	
	// 解析分页参数
	page := 1
	pageSize := 10
	
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	
	if ps := c.Query("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}
	
	sessions, total, err := h.sessionService.ListByTenant(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": sessions, "total": total})
}

func (h *SessionHandler) Create(c *gin.Context) {
	var req dto.CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	result, err := h.sessionService.CreateSession(c.Request.Context(), req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, gin.H{
		"session_id": result.Session.ID,
		"status":     result.Session.Status,
		"ws_token":   result.WSToken,
	})
}

func (h *SessionHandler) Approve(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var req dto.ApproveSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	userID, _ := c.Get("user_id")
	approverID, _ := userID.(string)

	if err := h.sessionService.ApproveSession(c.Request.Context(), sessionID, req.ApprovalRequestID, approverID, req.Comment); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "approved"})
}

func (h *SessionHandler) Deny(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var req dto.DenySessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	if err := h.sessionService.DenySession(c.Request.Context(), sessionID, req.ApprovalRequestID, req.Reason); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "denied"})
}

func (h *SessionHandler) Abort(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var req dto.AbortSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	if err := h.sessionService.AbortSession(c.Request.Context(), sessionID, req.Reason); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "aborted"})
}

func (h *SessionHandler) ListToolInvocations(c *gin.Context) {
	sessionID := c.Param("sessionId")
	invocations, err := h.sessionService.ListToolInvocations(c.Request.Context(), sessionID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": invocations})
}

func (h *SessionHandler) BatchDelete(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req struct {
		SessionIDs []string `json:"session_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	if err := h.sessionService.BatchDeleteSessions(c.Request.Context(), tenantID, req.SessionIDs); err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}

func (h *SessionHandler) Get(c *gin.Context) {
	tenantID := c.Param("tenantId")
	sessionID := c.Param("sessionId")

	session, err := h.sessionService.GetByID(c.Request.Context(), sessionID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	// 验证会话属于当前租户
	if session.TenantID != tenantID {
		mw.RespondError(c, domain.ErrSessionNotFound)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, session)
}

func (h *SessionHandler) GetActiveCount(c *gin.Context) {
	tenantID := c.Param("tenantId")

	count, err := h.sessionService.CountActiveByTenant(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"count": count})
}
