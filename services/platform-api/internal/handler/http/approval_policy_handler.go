package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/command"
)

type ApprovalPolicyHandler struct {
	policyService *command.ApprovalPolicyService
}

func NewApprovalPolicyHandler(policyService *command.ApprovalPolicyService) *ApprovalPolicyHandler {
	return &ApprovalPolicyHandler{policyService: policyService}
}

func (h *ApprovalPolicyHandler) RegisterRoutes(router *gin.RouterGroup) {
	policies := router.Group("/tenants/:tenantId/approval-policies")
	{
		policies.POST("", h.Create)
		policies.GET("", h.List)
		policies.PUT("/:policyId", h.Update)
		policies.DELETE("/:policyId", h.Delete)
	}
}

func (h *ApprovalPolicyHandler) Create(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req dto.CreateApprovalPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	resp, err := h.policyService.Create(c.Request.Context(), tenantID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, resp)
}

func (h *ApprovalPolicyHandler) List(c *gin.Context) {
	tenantID := c.Param("tenantId")
	resp, err := h.policyService.List(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *ApprovalPolicyHandler) Update(c *gin.Context) {
	policyID := c.Param("policyId")
	var req dto.UpdateApprovalPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	resp, err := h.policyService.Update(c.Request.Context(), policyID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *ApprovalPolicyHandler) Delete(c *gin.Context) {
	policyID := c.Param("policyId")
	if err := h.policyService.Delete(c.Request.Context(), policyID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}
