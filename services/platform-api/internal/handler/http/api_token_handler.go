package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/api_token"
)

type ApiTokenHandler struct {
	svc *api_token.Service
}

func NewApiTokenHandler(svc *api_token.Service) *ApiTokenHandler {
	return &ApiTokenHandler{svc: svc}
}

func (h *ApiTokenHandler) RegisterRoutes(router *gin.RouterGroup) {
	t := router.Group("/tenants/:tenantId/api-tokens")
	{
		t.POST("", h.Create)
		t.GET("", h.List)
		t.DELETE("/:id", h.Revoke)
	}
}

func (h *ApiTokenHandler) requireTenantScope(c *gin.Context, tenantID string) bool {
	jwtTenant, ok := c.Get("tenant_id")
	var super bool
	if v, ok2 := c.Get("platform_super_admin"); ok2 {
		if b, ok3 := v.(bool); ok3 {
			super = b
		}
	}
	if !ok {
		mw.RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "missing tenant context")
		return false
	}
	jt, ok := jwtTenant.(string)
	if !ok {
		mw.RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "invalid tenant context")
		return false
	}
	if jt != tenantID && !super {
		mw.RespondErrorCode(c, http.StatusForbidden, "forbidden", "tenant scope mismatch")
		return false
	}
	return true
}

func (h *ApiTokenHandler) Create(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	userID, ok := c.Get("user_id")
	if !ok {
		mw.RespondError(c, mw.ErrUnauthorizedFromContext())
		return
	}
	uid, _ := userID.(string)

	var super bool
	if v, ok2 := c.Get("platform_super_admin"); ok2 {
		if b, ok3 := v.(bool); ok3 {
			super = b
		}
	}

	var req dto.CreateApiTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	wantSuper := req.IsSuperAdmin
	if wantSuper && !super {
		mw.RespondErrorCode(c, http.StatusForbidden, "forbidden",
			"only platform super admins can create super admin tokens")
		return
	}

	out, err := h.svc.Create(c.Request.Context(), uid, tenantID, wantSuper, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, out)
}

func (h *ApiTokenHandler) List(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := h.svc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *ApiTokenHandler) Revoke(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	id := c.Param("id")
	if id == "" {
		mw.RespondValidationError(c, "id is required")
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), tenantID, id); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "ok"})
}
