package agent

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	device_binding "github.com/zayeagle/envnexus/services/platform-api/internal/service/device_binding"
)

type ActivateHandler struct {
	bindingService *device_binding.Service
}

func NewActivateHandler(bindingService *device_binding.Service) *ActivateHandler {
	return &ActivateHandler{bindingService: bindingService}
}

func (h *ActivateHandler) RegisterRoutes(router *gin.RouterGroup) {
	agentGroup := router.Group("/agent/v1")
	{
		agentGroup.POST("/register-device", h.RegisterDevice)
		agentGroup.POST("/activate", h.Activate)
		agentGroup.GET("/activation-status/:deviceCode", h.GetActivationStatus)
		agentGroup.POST("/activation-heartbeat", h.Heartbeat)
	}
}

func (h *ActivateHandler) RegisterDevice(c *gin.Context) {
	var req dto.RegisterDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.bindingService.RegisterDevice(c.Request.Context(), req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *ActivateHandler) Activate(c *gin.Context) {
	var req dto.ActivateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.bindingService.ActivateAuto(c.Request.Context(), req)
	if err != nil {
		slog.Error("[activate] ActivateAuto failed",
			"device_code", req.DeviceCode,
			"error", err.Error(),
		)
		mw.RespondError(c, err)
		return
	}

	slog.Info("[activate] ActivateAuto result",
		"device_code", req.DeviceCode,
		"activated", resp.Activated,
	)
	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *ActivateHandler) GetActivationStatus(c *gin.Context) {
	deviceCode := c.Param("deviceCode")
	if deviceCode == "" {
		mw.RespondValidationError(c, "deviceCode is required")
		return
	}

	resp, err := h.bindingService.GetActivationStatus(c.Request.Context(), deviceCode)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *ActivateHandler) Heartbeat(c *gin.Context) {
	var req dto.HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.bindingService.CheckHeartbeat(c.Request.Context(), req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}
