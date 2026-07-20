package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/license"
)

type LicenseHandler struct {
	svc *license.Service
}

func NewLicenseHandler(svc *license.Service) *LicenseHandler {
	return &LicenseHandler{svc: svc}
}

func (h *LicenseHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/tenants/:tenantId/license")
	g.GET("", h.Get)
	g.POST("/activate", h.Activate)
	g.POST("/revoke/:licenseId", h.Revoke)
}

func (h *LicenseHandler) Get(c *gin.Context) {
	tenantID := c.Param("tenantId")
	row, err := h.svc.GetForTenant(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if row == nil {
		// Return trial info
		result, _ := h.svc.Validate(c.Request.Context(), tenantID)
		mw.RespondSuccess(c, http.StatusOK, result)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, row)
}

func (h *LicenseHandler) Activate(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req struct {
		LicenseKey string `json:"license_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	row, err := h.svc.Activate(c.Request.Context(), tenantID, req.LicenseKey)
	if err != nil {
		mw.RespondErrorCode(c, http.StatusBadRequest, "license_invalid", err.Error())
		return
	}
	mw.RespondSuccess(c, http.StatusOK, row)
}

func (h *LicenseHandler) Revoke(c *gin.Context) {
	licenseID := c.Param("licenseId")
	if err := h.svc.Revoke(c.Request.Context(), licenseID); err != nil {
		mw.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"revoked": licenseID})
}
