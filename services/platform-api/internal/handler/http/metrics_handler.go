package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/metrics"
)

type MetricsHandler struct {
	svc *metrics.Service
}

func NewMetricsHandler(svc *metrics.Service) *MetricsHandler {
	return &MetricsHandler{svc: svc}
}

func (h *MetricsHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/tenants/:tenantId/metrics")
	g.GET("/current", h.Current)
	g.GET("/history", h.History)
}

func (h *MetricsHandler) Current(c *gin.Context) {
	tenantID := c.Param("tenantId")
	data, err := h.svc.GetCurrentPeriod(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	mw.RespondSuccess(c, http.StatusOK, data)
}

func (h *MetricsHandler) History(c *gin.Context) {
	tenantID := c.Param("tenantId")
	months, _ := strconv.Atoi(c.DefaultQuery("months", "6"))
	data, err := h.svc.GetHistory(c.Request.Context(), tenantID, months)
	if err != nil {
		mw.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": data})
}
