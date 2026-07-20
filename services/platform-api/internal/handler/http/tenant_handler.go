package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/tenant"
)

type TenantHandler struct {
	tenantService *tenant.Service
}

func NewTenantHandler(tenantService *tenant.Service) *TenantHandler {
	return &TenantHandler{
		tenantService: tenantService,
	}
}

func (h *TenantHandler) RegisterRoutes(router *gin.RouterGroup) {
	tenants := router.Group("/tenants")
	{
		tenants.POST("", h.CreateTenant)
		tenants.GET("", h.ListTenants)
		tenants.GET("/:tenantId", h.GetTenant)
		tenants.PUT("/:tenantId", h.UpdateTenant)
		tenants.DELETE("/:tenantId", h.DeleteTenant)
	}
}

func (h *TenantHandler) CreateTenant(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.tenantService.CreateTenant(c.Request.Context(), req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, resp)
}

func (h *TenantHandler) GetTenant(c *gin.Context) {
	id := c.Param("tenantId")

	resp, err := h.tenantService.GetTenant(c.Request.Context(), id)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *TenantHandler) ListTenants(c *gin.Context) {
	userTenantID := c.GetString("tenant_id")
	super := false
	if v, ok := c.Get("platform_super_admin"); ok {
		if b, ok := v.(bool); ok {
			super = b
		}
	}

	// Get pagination parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// Validate pagination parameters
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	if super {
		// For super admins, return paginated list
		tenants, total, err := h.tenantService.ListTenantsWithPagination(c.Request.Context(), page, pageSize)
		if err != nil {
			mw.RespondError(c, err)
			return
		}
		mw.RespondSuccess(c, http.StatusOK, gin.H{"items": tenants, "total": total, "page": page, "page_size": pageSize})
	} else {
		// For regular users, return only their home tenant
		resp, err := h.tenantService.ListTenantsForActor(c.Request.Context(), super, userTenantID)
		if err != nil {
			mw.RespondError(c, err)
			return
		}
		mw.RespondSuccess(c, http.StatusOK, gin.H{"items": resp, "total": len(resp), "page": 1, "page_size": 1})
	}
}

func (h *TenantHandler) UpdateTenant(c *gin.Context) {
	id := c.Param("tenantId")

	var req dto.UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.tenantService.UpdateTenant(c.Request.Context(), id, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *TenantHandler) DeleteTenant(c *gin.Context) {
	id := c.Param("tenantId")

	err := h.tenantService.DeleteTenant(c.Request.Context(), id)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}
