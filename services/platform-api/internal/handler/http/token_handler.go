package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/enrollment"
)

type TokenHandler struct {
	enrollService *enrollment.Service
}

func NewTokenHandler(enrollService *enrollment.Service) *TokenHandler {
	return &TokenHandler{
		enrollService: enrollService,
	}
}

func (h *TokenHandler) RegisterRoutes(router *gin.RouterGroup) {
	links := router.Group("/tenants/:tenantId/download-links")
	{
		links.POST("", h.CreateToken)
		links.GET("", h.ListTokens)
	}
}

func (h *TokenHandler) CreateToken(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if tenantID == "" {
		mw.RespondValidationError(c, "tenantId is required")
		return
	}

	var req dto.CreateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.enrollService.CreateToken(c.Request.Context(), tenantID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, resp)
}

func (h *TokenHandler) ListTokens(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if tenantID == "" {
		mw.RespondValidationError(c, "tenantId is required")
		return
	}

	tokens, err := h.enrollService.ListTokens(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": tokens})
}
