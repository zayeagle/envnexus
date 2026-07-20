package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/device"
)

func (h *DeviceHandler) RotateToken(c *gin.Context) {
	tenantID := c.Param("tenantId")
	id := c.Param("id")

	newToken, err := h.deviceService.RotateDeviceToken(c.Request.Context(), tenantID, id)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"device_token": newToken})
}

type DeviceHandler struct {
	deviceService *device.Service
}

func NewDeviceHandler(deviceService *device.Service) *DeviceHandler {
	return &DeviceHandler{
		deviceService: deviceService,
	}
}

func (h *DeviceHandler) RegisterRoutes(router *gin.RouterGroup) {
	tenants := router.Group("/tenants/:tenantId/devices")
	{
		tenants.GET("", h.ListDevices)
		tenants.PUT("/:id", h.UpdateDevice)
		tenants.DELETE("/:id", h.DeleteDevice)
		tenants.POST("/:id/rotate-token", h.RotateToken)
	}
}

func (h *DeviceHandler) UpdateDevice(c *gin.Context) {
	tenantID := c.Param("tenantId")
	id := c.Param("id")

	var req dto.UpdateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.deviceService.UpdateDevice(c.Request.Context(), tenantID, id, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	tenantID := c.Param("tenantId")
	id := c.Param("id")

	err := h.deviceService.DeleteDevice(c.Request.Context(), tenantID, id)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}

func (h *DeviceHandler) ListDevices(c *gin.Context) {
	tenantID := c.Param("tenantId")
	activeOnly := c.Query("active_only") == "true" || c.Query("active_only") == "1"
	requirePlatformArch := c.Query("require_platform_arch") == "true" || c.Query("require_platform_arch") == "1"
	
	// Parse pagination parameters
	page := 1
	pageSize := 10
	if c.Query("page") != "" {
		if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
			page = p
		}
	}
	if c.Query("page_size") != "" {
		if ps, err := strconv.Atoi(c.Query("page_size")); err == nil && ps > 0 {
			pageSize = ps
		}
	}

	resp, total, err := h.deviceService.ListDevices(c.Request.Context(), tenantID, activeOnly, requirePlatformArch, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{
		"items": resp,
		"total": total,
		"page": page,
		"page_size": pageSize,
	})
}
