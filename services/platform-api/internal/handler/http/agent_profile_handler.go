package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/agent_profile"
)

type AgentProfileHandler struct {
	profileService *agent_profile.Service
}

func NewAgentProfileHandler(profileService *agent_profile.Service) *AgentProfileHandler {
	return &AgentProfileHandler{
		profileService: profileService,
	}
}

func (h *AgentProfileHandler) RegisterRoutes(router *gin.RouterGroup) {
	profiles := router.Group("/tenants/:tenantId/agent-profiles")
	{
		profiles.GET("", h.ListProfiles)
		profiles.POST("", h.CreateProfile)
		profiles.PUT("/:id", h.UpdateProfile)
		profiles.DELETE("/:id", h.DeleteProfile)
	}
}

func (h *AgentProfileHandler) ListProfiles(c *gin.Context) {
	tenantID := c.Param("tenantId")
	resp, err := h.profileService.ListProfiles(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *AgentProfileHandler) CreateProfile(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req dto.CreateAgentProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.profileService.CreateProfile(c.Request.Context(), tenantID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, resp)
}

func (h *AgentProfileHandler) UpdateProfile(c *gin.Context) {
	tenantID := c.Param("tenantId")
	id := c.Param("id")

	var req dto.UpdateAgentProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.profileService.UpdateProfile(c.Request.Context(), tenantID, id, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *AgentProfileHandler) DeleteProfile(c *gin.Context) {
	tenantID := c.Param("tenantId")
	id := c.Param("id")

	err := h.profileService.DeleteProfile(c.Request.Context(), tenantID, id)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}
