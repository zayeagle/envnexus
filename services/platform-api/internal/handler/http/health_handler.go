package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/health"
)

type HealthHandler struct {
	svc *health.Service
}

func NewHealthHandler(svc *health.Service) *HealthHandler {
	return &HealthHandler{svc: svc}
}

func (h *HealthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/tenants/:tenantId/health")
	g.GET("/summary", h.Summary)
	g.GET("/devices", h.Devices)
}

func (h *HealthHandler) Summary(c *gin.Context) {
	tenantID := c.Param("tenantId")
	summary, err := h.svc.GetTenantSummary(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, summary)
}

func (h *HealthHandler) Devices(c *gin.Context) {
	tenantID := c.Param("tenantId")
	devices, err := h.svc.ListDeviceHealth(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": devices})
}
