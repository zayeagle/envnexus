package agent

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/session"
)

type ApprovalHandler struct {
	sessionService *session.Service
}

func NewApprovalHandler(sessionService *session.Service) *ApprovalHandler {
	return &ApprovalHandler{sessionService: sessionService}
}

func (h *ApprovalHandler) RegisterRoutes(router *gin.RouterGroup) {
	agentGroup := router.Group("/agent/v1")
	{
		agentGroup.POST("/sessions", h.CreateSession)
		agentGroup.POST("/approvals", h.CreateApproval)
		agentGroup.POST("/approvals/:approvalId/executing", h.MarkExecuting)
		agentGroup.POST("/approvals/:approvalId/succeeded", h.MarkSucceeded)
		agentGroup.POST("/approvals/:approvalId/failed", h.MarkFailed)
		agentGroup.POST("/approvals/:approvalId/rolled-back", h.MarkRolledBack)
		agentGroup.GET("/approvals/:approvalId", h.GetApproval)
		agentGroup.GET("/sessions/:sessionId/approvals/pending", h.GetPendingApproval)
	}
}

func (h *ApprovalHandler) CreateSession(c *gin.Context) {
	deviceID, _ := c.Get("device_id")
	deviceIDStr, _ := deviceID.(string)

	var req struct {
		DeviceID      string `json:"device_id"`
		Transport     string `json:"transport"`
		InitiatorType string `json:"initiator_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	if req.DeviceID == "" {
		req.DeviceID = deviceIDStr
	}
	if req.Transport == "" {
		req.Transport = "websocket"
	}
	if req.InitiatorType == "" {
		req.InitiatorType = "agent"
	}

	result, err := h.sessionService.CreateSession(c.Request.Context(), dto.CreateSessionRequest{
		DeviceID:      req.DeviceID,
		Transport:     req.Transport,
		InitiatorType: req.InitiatorType,
	})
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, gin.H{
		"session_id": result.Session.ID,
		"ws_token":   result.WSToken,
		"status":     result.Session.Status,
	})
}

type CreateApprovalRequest struct {
	SessionID string      `json:"session_id" binding:"required"`
	ToolName  string      `json:"tool_name" binding:"required"`
	RiskLevel string      `json:"risk_level" binding:"required"`
	Params    interface{} `json:"params"`
}

func (h *ApprovalHandler) CreateApproval(c *gin.Context) {
	var req CreateApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	deviceID, _ := c.Get("device_id")
	deviceIDStr, _ := deviceID.(string)

	actionJSON, _ := json.Marshal(map[string]interface{}{
		"tool_name":   req.ToolName,
		"risk_level":  req.RiskLevel,
		"params":      req.Params,
	})

	approval, err := h.sessionService.CreateApprovalRequest(
		c.Request.Context(),
		req.SessionID,
		deviceIDStr,
		string(actionJSON),
		req.RiskLevel,
	)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, gin.H{
		"approval_id": approval.ID,
		"status":      approval.Status,
		"expires_at":  approval.ExpiresAt,
	})
}

func (h *ApprovalHandler) MarkExecuting(c *gin.Context) {
	approvalID := c.Param("approvalId")
	if err := h.sessionService.MarkApprovalExecuting(c.Request.Context(), approvalID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": domain.ApprovalStatusExecuting})
}

func (h *ApprovalHandler) MarkSucceeded(c *gin.Context) {
	approvalID := c.Param("approvalId")
	if err := h.sessionService.MarkApprovalSucceeded(c.Request.Context(), approvalID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": domain.ApprovalStatusSucceeded})
}

func (h *ApprovalHandler) MarkFailed(c *gin.Context) {
	approvalID := c.Param("approvalId")
	if err := h.sessionService.MarkApprovalFailed(c.Request.Context(), approvalID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": domain.ApprovalStatusFailed})
}

func (h *ApprovalHandler) MarkRolledBack(c *gin.Context) {
	approvalID := c.Param("approvalId")
	if err := h.sessionService.MarkApprovalRolledBack(c.Request.Context(), approvalID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": domain.ApprovalStatusRolledBack})
}

func (h *ApprovalHandler) GetApproval(c *gin.Context) {
	approvalID := c.Param("approvalId")

	approval, err := h.sessionService.GetApprovalByID(c.Request.Context(), approvalID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, approval)
}

func (h *ApprovalHandler) GetPendingApproval(c *gin.Context) {
	sessionID := c.Param("sessionId")

	approval, err := h.sessionService.GetPendingApproval(c.Request.Context(), sessionID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	if approval == nil {
		mw.RespondSuccess(c, http.StatusOK, gin.H{"pending": false})
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{
		"pending":     true,
		"approval_id": approval.ID,
		"status":      approval.Status,
		"risk_level":  approval.RiskLevel,
		"action":      approval.RequestedActionJSON,
		"expires_at":  approval.ExpiresAt,
	})
}
