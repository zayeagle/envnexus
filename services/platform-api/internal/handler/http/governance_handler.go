package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/governance"
)

type GovernanceHandler struct {
	svc *governance.Service
}

func NewGovernanceHandler(svc *governance.Service) *GovernanceHandler {
	return &GovernanceHandler{svc: svc}
}

func (h *GovernanceHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/tenants/:tenantId/governance")
	g.GET("/summary", h.GetSummary)
	g.GET("/baselines", h.ListBaselines)
	g.GET("/drifts", h.ListDrifts)
	g.POST("/drifts/:driftId/resolve", h.ResolveDrift)
}

func (h *GovernanceHandler) GetSummary(c *gin.Context) {
	tenantID := c.Param("tenantId")
	summary, err := h.svc.GetSummary(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, summary)
}

func (h *GovernanceHandler) ListBaselines(c *gin.Context) {
	tenantID := c.Param("tenantId")
	deviceID := c.Query("device_id")

	baselines, err := h.svc.ListBaselines(c.Request.Context(), tenantID, deviceID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	var resp []*dto.GovernanceBaselineResponse
	for _, b := range baselines {
		resp = append(resp, &dto.GovernanceBaselineResponse{
			ID:           b.ID,
			DeviceID:     b.DeviceID,
			TenantID:     b.TenantID,
			SnapshotJSON: b.SnapshotJSON,
			CapturedAt:   b.CapturedAt,
		})
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *GovernanceHandler) ListDrifts(c *gin.Context) {
	tenantID := c.Param("tenantId")
	filters := repository.DriftFilters{
		DeviceID:       c.Query("device_id"),
		Severity:       c.Query("severity"),
		UnresolvedOnly: c.Query("unresolved") == "true",
	}

	drifts, err := h.svc.ListDrifts(c.Request.Context(), tenantID, filters)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	var resp []*dto.GovernanceDriftResponse
	for _, d := range drifts {
		resp = append(resp, &dto.GovernanceDriftResponse{
			ID:            d.ID,
			DeviceID:      d.DeviceID,
			TenantID:      d.TenantID,
			BaselineID:    d.BaselineID,
			DriftType:     d.DriftType,
			KeyName:       d.KeyName,
			ExpectedValue: d.ExpectedValue,
			ActualValue:   d.ActualValue,
			Severity:      d.Severity,
			DetectedAt:    d.DetectedAt,
			ResolvedAt:    d.ResolvedAt,
		})
	}
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *GovernanceHandler) ResolveDrift(c *gin.Context) {
	driftID := c.Param("driftId")
	if err := h.svc.ResolveDrift(c.Request.Context(), driftID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"message": "drift resolved"})
}
