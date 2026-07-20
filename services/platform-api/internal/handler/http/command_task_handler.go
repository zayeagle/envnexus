package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/libs/shared/pkg/agentprompt"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/command"
)

type CommandTaskHandler struct {
	commandService *command.Service
	nlGenerator    *command.NLGenerator
	deviceRepo     repository.DeviceRepository
	userRepo       repository.UserRepository
}

func NewCommandTaskHandler(commandService *command.Service, nlGenerator *command.NLGenerator, deviceRepo repository.DeviceRepository, userRepo repository.UserRepository) *CommandTaskHandler {
	return &CommandTaskHandler{
		commandService: commandService,
		nlGenerator:    nlGenerator,
		deviceRepo:     deviceRepo,
		userRepo:       userRepo,
	}
}

func platformSuperAdminFromJWT(c *gin.Context) bool {
	v, ok := c.Get("platform_super_admin")
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// effectivePlatformSuperAdmin is true when JWT says so or the user row does (covers stale tokens after DB promotion).
func (h *CommandTaskHandler) effectivePlatformSuperAdmin(c *gin.Context) bool {
	if platformSuperAdminFromJWT(c) {
		return true
	}
	if h.userRepo == nil {
		return false
	}
	uid := c.GetString("user_id")
	if uid == "" {
		return false
	}
	u, err := h.userRepo.GetByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		return false
	}
	return u.PlatformSuperAdmin
}

func (h *CommandTaskHandler) RegisterRoutes(router *gin.RouterGroup) {
	tasks := router.Group("/tenants/:tenantId/command-tasks")
	{
		tasks.POST("", h.CreateTask)
		tasks.GET("", h.ListTasks)
		tasks.POST("/generate", h.GenerateCommand)
		tasks.GET("/:taskId", h.GetTask)
		tasks.PUT("/:taskId", h.UpdateTask)
		tasks.POST("/:taskId/submit", h.SubmitTask)
		tasks.DELETE("/:taskId", h.DeleteTask)
		tasks.POST("/:taskId/approve", h.ApproveTask)
		tasks.POST("/:taskId/deny", h.DenyTask)
		tasks.POST("/:taskId/cancel", h.CancelTask)
	}
	approvals := router.Group("/tenants/:tenantId/pending-approvals")
	{
		approvals.GET("", h.ListPendingApprovals)
		approvals.GET("/count", h.CountPendingApprovals)
	}
}

// RegisterInternalRoutes registers routes for internal service-to-service communication
// (e.g., session-gateway forwarding command results). No JWT auth required.
func (h *CommandTaskHandler) RegisterInternalRoutes(router *gin.RouterGroup) {
	router.POST("/command-results", h.HandleCommandResult)
}

func (h *CommandTaskHandler) CreateTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	userID := c.GetString("user_id")
	var req dto.CreateCommandTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	resp, err := h.commandService.CreateTask(c.Request.Context(), tenantID, userID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, resp)
}

func (h *CommandTaskHandler) GetTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	resp, err := h.commandService.GetTask(c.Request.Context(), tenantID, taskID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *CommandTaskHandler) SubmitTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	userID := c.GetString("user_id")
	resp, err := h.commandService.SubmitTask(c.Request.Context(), tenantID, userID, taskID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *CommandTaskHandler) UpdateTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	userID := c.GetString("user_id")
	var req dto.UpdateCommandTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	resp, err := h.commandService.UpdateTask(c.Request.Context(), tenantID, userID, taskID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *CommandTaskHandler) DeleteTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	userID := c.GetString("user_id")
	if err := h.commandService.DeleteTask(c.Request.Context(), tenantID, userID, taskID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}

func (h *CommandTaskHandler) ListTasks(c *gin.Context) {
	tenantID := c.Param("tenantId")
	filters := repository.CommandTaskFilters{
		Status:    c.Query("status"),
		CreatedBy: c.Query("created_by"),
		RiskLevel: c.Query("risk_level"),
		IncludeArchived: strings.EqualFold(c.Query("include_archived"), "true") || c.Query("include_archived") == "1",
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	resp, err := h.commandService.ListTasks(c.Request.Context(), tenantID, filters, limit, offset)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *CommandTaskHandler) ApproveTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	approverID := c.GetString("user_id")
	var req dto.ApproveCommandTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = dto.ApproveCommandTaskRequest{}
	}
	if err := h.commandService.ApproveTask(c.Request.Context(), tenantID, taskID, approverID, req.Note, h.effectivePlatformSuperAdmin(c)); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "approved"})
}

func (h *CommandTaskHandler) DenyTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	approverID := c.GetString("user_id")
	var req dto.DenyCommandTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = dto.DenyCommandTaskRequest{}
	}
	if err := h.commandService.DenyTask(c.Request.Context(), tenantID, taskID, approverID, req.Reason, h.effectivePlatformSuperAdmin(c)); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "denied"})
}

func (h *CommandTaskHandler) CancelTask(c *gin.Context) {
	tenantID := c.Param("tenantId")
	taskID := c.Param("taskId")
	userID := c.GetString("user_id")
	if err := h.commandService.CancelTask(c.Request.Context(), tenantID, taskID, userID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "cancelled"})
}

func (h *CommandTaskHandler) ListPendingApprovals(c *gin.Context) {
	tenantID := c.Param("tenantId")
	userID := c.GetString("user_id")
	resp, err := h.commandService.ListPendingApprovals(c.Request.Context(), tenantID, userID, h.effectivePlatformSuperAdmin(c))
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *CommandTaskHandler) CountPendingApprovals(c *gin.Context) {
	tenantID := c.Param("tenantId")
	userID := c.GetString("user_id")
	count, err := h.commandService.CountPendingApprovals(c.Request.Context(), tenantID, userID, h.effectivePlatformSuperAdmin(c))
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, dto.PendingApprovalCountResponse{Count: count})
}

func (h *CommandTaskHandler) HandleCommandResult(c *gin.Context) {
	var envelope struct {
		EventType string `json:"event_type"`
		DeviceID  string `json:"device_id"`
		SessionID string `json:"session_id"`
		Payload   struct {
			ExecutionID string  `json:"execution_id"`
			Status      string  `json:"status"`
			Output      *string `json:"output"`
			Error       *string `json:"error"`
			ExitCode    *int    `json:"exit_code"`
			DurationMs  *int    `json:"duration_ms"`
		} `json:"payload"`
	}

	if err := c.ShouldBindJSON(&envelope); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if envelope.Payload.ExecutionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing execution_id in payload"})
		return
	}

	err := h.commandService.HandleExecutionResult(
		c.Request.Context(),
		envelope.Payload.ExecutionID,
		envelope.Payload.Status,
		envelope.Payload.Output,
		envelope.Payload.ExitCode,
		envelope.Payload.DurationMs,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

func (h *CommandTaskHandler) GenerateCommand(c *gin.Context) {
	if h.nlGenerator == nil {
		mw.RespondError(c, fmt.Errorf("NL command generation is not configured"))
		return
	}

	tenantID := c.Param("tenantId")
	var req struct {
		Prompt   string `json:"prompt" binding:"required"`
		DeviceID string `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	target := agentprompt.DefaultNLTargetWhenNoDevice()
	if did := strings.TrimSpace(req.DeviceID); did != "" {
		if h.deviceRepo == nil {
			mw.RespondError(c, fmt.Errorf("device repository not available"))
			return
		}
		dev, err := h.deviceRepo.GetByID(c.Request.Context(), did)
		if err != nil {
			mw.RespondError(c, err)
			return
		}
		if dev == nil || dev.TenantID != tenantID {
			mw.RespondValidationError(c, "device not found")
			return
		}
		target = command.NLTargetFromDevice(dev)
	}

	result, err := h.nlGenerator.Generate(c.Request.Context(), tenantID, req.Prompt, target)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, result)
}
