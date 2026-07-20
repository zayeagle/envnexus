package agent

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/enrollment"
)

type EnrollHandler struct {
	enrollService *enrollment.Service
}

func NewEnrollHandler(enrollService *enrollment.Service) *EnrollHandler {
	return &EnrollHandler{enrollService: enrollService}
}

func (h *EnrollHandler) RegisterRoutes(router *gin.RouterGroup) {
	agentGroup := router.Group("/agent/v1")
	{
		agentGroup.POST("/enroll", h.Enroll)
	}
}

func (h *EnrollHandler) Enroll(c *gin.Context) {
	var req dto.AgentEnrollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.enrollService.EnrollAgent(c.Request.Context(), req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}
