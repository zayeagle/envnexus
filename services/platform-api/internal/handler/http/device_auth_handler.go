package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/device_auth"
)

// DeviceAuthHandler exposes RFC 8628-style device login and IDE token refresh.
type DeviceAuthHandler struct {
	svc *device_auth.Service
}

func NewDeviceAuthHandler(svc *device_auth.Service) *DeviceAuthHandler {
	return &DeviceAuthHandler{svc: svc}
}

// RegisterPublicRoutes device init, poll, and refresh (no user session).
func (h *DeviceAuthHandler) RegisterPublicRoutes(router *gin.RouterGroup) {
	router.POST("/device-auth/init", h.Init)
	router.POST("/device-auth/poll", h.Poll)
	router.POST("/device-auth/refresh", h.Refresh)
}

// RegisterProtectedRoutes require JWT (confirm flow and tenant IDE token admin).
func (h *DeviceAuthHandler) RegisterProtectedRoutes(router *gin.RouterGroup) {
	router.POST("/device-auth/confirm", h.Confirm)
	t := router.Group("/tenants/:tenantId")
	{
		t.GET("/ide-tokens", h.ListIdeTokens)
		t.DELETE("/ide-tokens/:id", h.RevokeIdeToken)
	}
}

func (h *DeviceAuthHandler) Init(c *gin.Context) {
	var req dto.DeviceAuthInitAPIRequest
	_ = c.ShouldBindJSON(&req)
	out, err := h.svc.InitDeviceAuthorization(c.Request.Context(), req.DeviceInfo)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, out)
}

func (h *DeviceAuthHandler) Poll(c *gin.Context) {
	var req dto.DeviceAuthPollAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	out, err := h.svc.PollForTokens(c.Request.Context(), req.DeviceCode)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	// 200 for RFC 8628-style errors: authorization_pending, access_denied, expired_token, or tokens.
	mw.RespondSuccess(c, http.StatusOK, out)
}

func (h *DeviceAuthHandler) Refresh(c *gin.Context) {
	var req dto.IdeClientRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	out, err := h.svc.RefreshTokens(c.Request.Context(), req.RefreshToken)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, out)
}

func (h *DeviceAuthHandler) Confirm(c *gin.Context) {
	var req dto.DeviceAuthConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	userID, ok := c.Get("user_id")
	if !ok {
		mw.RespondError(c, mw.ErrUnauthorizedFromContext())
		return
	}
	tenantID, ok := c.Get("tenant_id")
	if !ok {
		mw.RespondError(c, mw.ErrUnauthorizedFromContext())
		return
	}
	uid, ok := userID.(string)
	if !ok || uid == "" {
		mw.RespondError(c, mw.ErrUnauthorizedFromContext())
		return
	}
	tid, ok := tenantID.(string)
	if !ok || tid == "" {
		mw.RespondError(c, mw.ErrUnauthorizedFromContext())
		return
	}
	if err := h.svc.ConfirmUserCode(c.Request.Context(), req.UserCode, uid, tid, req.Approve); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *DeviceAuthHandler) requireTenantScope(c *gin.Context, tenantID string) bool {
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

func (h *DeviceAuthHandler) ListIdeTokens(c *gin.Context) {
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
	items, total, err := h.svc.ListIdeTokens(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *DeviceAuthHandler) RevokeIdeToken(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	id := c.Param("id")
	if id == "" {
		mw.RespondValidationError(c, "id is required")
		return
	}
	if err := h.svc.RevokeIdeToken(c.Request.Context(), tenantID, id); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "ok"})
}
