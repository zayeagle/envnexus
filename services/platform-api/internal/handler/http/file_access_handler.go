package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/file_access"
)

type FileAccessHandler struct {
	svc *file_access.Service
}

func NewFileAccessHandler(svc *file_access.Service) *FileAccessHandler {
	return &FileAccessHandler{svc: svc}
}

func (h *FileAccessHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.POST("/tenants/:tenantId/file-access-requests", h.Create)
	router.GET("/tenants/:tenantId/file-access-requests", h.List)
	router.GET("/tenants/:tenantId/file-access-requests/:requestId", h.Get)
	router.POST("/tenants/:tenantId/file-access-requests/:requestId/approve", h.Approve)
	router.POST("/tenants/:tenantId/file-access-requests/:requestId/deny", h.Deny)
	router.DELETE("/tenants/:tenantId/file-access-requests", h.ClearHistory)
	router.GET("/tenants/:tenantId/pending-file-approvals", h.ListPendingApprovals)
}

func (h *FileAccessHandler) RegisterInternalRoutes(router *gin.RouterGroup) {
	router.POST("/file-access-results", h.HandleFileAccessResult)
}

type createFileAccessReq struct {
	DeviceID string `json:"device_id" binding:"required"`
	Path     string `json:"path" binding:"required"`
	Action   string `json:"action" binding:"required,oneof=browse preview download"`
	Note     string `json:"note"`
}

func (h *FileAccessHandler) Create(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req createFileAccessReq
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	if uid == "" {
		mw.RespondError(c, domain.ErrUnauthorized)
		return
	}

	far, err := h.svc.CreateRequest(c.Request.Context(), tenantID, req.DeviceID, uid, req.Path, domain.FileAccessAction(req.Action), req.Note)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, far)
}

func (h *FileAccessHandler) List(c *gin.Context) {
	tenantID := c.Param("tenantId")
	status := c.Query("status")
	items, err := h.svc.ListByTenant(c.Request.Context(), tenantID, status)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": items})
}

func (h *FileAccessHandler) Get(c *gin.Context) {
	requestID := c.Param("requestId")
	far, err := h.svc.GetByID(c.Request.Context(), requestID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, far)
}

func (h *FileAccessHandler) Approve(c *gin.Context) {
	requestID := c.Param("requestId")
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	isPSA, _ := c.Get("is_platform_super_admin")
	isPlatformSuperAdmin, _ := isPSA.(bool)
	far, err := h.svc.Approve(c.Request.Context(), requestID, uid, isPlatformSuperAdmin)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, far)
}

func (h *FileAccessHandler) Deny(c *gin.Context) {
	requestID := c.Param("requestId")
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	far, err := h.svc.Deny(c.Request.Context(), requestID, uid)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, far)
}

func (h *FileAccessHandler) ListPendingApprovals(c *gin.Context) {
	tenantID := c.Param("tenantId")
	items, err := h.svc.ListPending(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": items})
}

func (h *FileAccessHandler) ClearHistory(c *gin.Context) {
	tenantID := c.Param("tenantId")
	deleted, err := h.svc.ClearHistory(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"deleted": deleted})
}

// HandleFileAccessResult receives file operation results forwarded by session-gateway.
// Mirrors the command.result callback pattern.
func (h *FileAccessHandler) HandleFileAccessResult(c *gin.Context) {
	var evt struct {
		EventType string          `json:"event_type"`
		DeviceID  string          `json:"device_id"`
		SessionID string          `json:"session_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := c.ShouldBindJSON(&evt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	requestID := evt.SessionID
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session_id (request_id)"})
		return
	}

	resultJSON := string(evt.Payload)
	if err := h.svc.SetResult(c.Request.Context(), requestID, resultJSON); err != nil {
		slog.Error("[file_access] Failed to store result",
			"request_id", requestID,
			"event_type", evt.EventType,
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	slog.Info("[file_access] Result stored",
		"request_id", requestID,
		"event_type", evt.EventType,
		"device_id", evt.DeviceID,
	)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
