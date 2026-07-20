package agent

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/governance"
)

type GovernanceHandler struct {
	svc     *governance.Service
	ruleSvc *governance.RuleService
}

func NewGovernanceHandler(svc *governance.Service) *GovernanceHandler {
	return &GovernanceHandler{svc: svc}
}

func (h *GovernanceHandler) SetRuleService(rs *governance.RuleService) {
	h.ruleSvc = rs
}

func (h *GovernanceHandler) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/agent/v1/governance")
	g.POST("/baselines", h.ReportBaseline)
	g.POST("/drifts", h.ReportDrifts)
	g.GET("/sync", h.SyncRules)
}

func (h *GovernanceHandler) ReportBaseline(c *gin.Context) {
	var req dto.ReportBaselineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	deviceID, _ := c.Get("device_id")
	tenantID, _ := c.Get("tenant_id")
	did, _ := deviceID.(string)
	tid, _ := tenantID.(string)
	if did == "" {
		did = c.GetHeader("X-Device-ID")
	}
	if tid == "" {
		tid = c.GetHeader("X-Tenant-ID")
	}

	baseline, err := h.svc.ReportBaseline(c.Request.Context(), did, tid, req.SnapshotJSON)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, gin.H{
		"baseline_id": baseline.ID,
	})
}

func (h *GovernanceHandler) ReportDrifts(c *gin.Context) {
	var req dto.ReportDriftsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	deviceID, _ := c.Get("device_id")
	tenantID, _ := c.Get("tenant_id")
	did, _ := deviceID.(string)
	tid, _ := tenantID.(string)
	if did == "" {
		did = c.GetHeader("X-Device-ID")
	}
	if tid == "" {
		tid = c.GetHeader("X-Tenant-ID")
	}

	var drifts []governance.DriftReport
	for _, d := range req.Drifts {
		drifts = append(drifts, governance.DriftReport{
			DriftType:     d.DriftType,
			KeyName:       d.KeyName,
			ExpectedValue: d.ExpectedValue,
			ActualValue:   d.ActualValue,
			Severity:      d.Severity,
		})
	}

	count, err := h.svc.ReportDrifts(c.Request.Context(), did, tid, drifts)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, gin.H{
		"accepted": count,
	})
}

func (h *GovernanceHandler) SyncRules(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")
	tid, _ := tenantID.(string)
	if tid == "" {
		tid = c.GetHeader("X-Tenant-ID")
	}

	result := gin.H{}

	if h.ruleSvc != nil {
		rules, err := h.ruleSvc.ListRules(c.Request.Context(), tid)
		if err != nil {
			mw.RespondError(c, err)
			return
		}
		result["rules"] = rules

		perms, err := h.ruleSvc.ListToolPermissions(c.Request.Context(), tid)
		if err != nil {
			mw.RespondError(c, err)
			return
		}
		result["tool_permissions"] = perms
	} else {
		result["rules"] = []interface{}{}
		result["tool_permissions"] = []interface{}{}
	}

	mw.RespondSuccess(c, http.StatusOK, result)
}
