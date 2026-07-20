package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	device_binding "github.com/zayeagle/envnexus/services/platform-api/internal/service/device_binding"
	package_svc "github.com/zayeagle/envnexus/services/platform-api/internal/service/package"
)

type PackageHandler struct {
	pkgService     *package_svc.Service
	bindingService *device_binding.Service
}

func NewPackageHandler(pkgService *package_svc.Service, bindingService *device_binding.Service) *PackageHandler {
	return &PackageHandler{
		pkgService:     pkgService,
		bindingService: bindingService,
	}
}

func (h *PackageHandler) RegisterRoutes(router *gin.RouterGroup) {
	pkgs := router.Group("/tenants/:tenantId/download-packages")
	{
		pkgs.POST("", h.CreatePackage)
		pkgs.GET("", h.ListPackages)

		pkgs.DELETE("/:packageId", h.DeletePackage)
		pkgs.GET("/:packageId/download-url", h.GetDownloadURL)
		pkgs.POST("/:packageId/bind", h.BindDevice)
		pkgs.DELETE("/:packageId/bindings/:bindingId", h.UnbindDevice)
		pkgs.GET("/:packageId/bindings", h.ListBindings)
		pkgs.PUT("/:packageId/max-devices", h.UpdateMaxDevices)
		pkgs.GET("/:packageId/audit-logs", h.ListPackageAuditLogs)
	}

	auditLogs := router.Group("/tenants/:tenantId/activation-audit-logs")
	{
		auditLogs.GET("", h.ListTenantAuditLogs)
	}
}

func (h *PackageHandler) CreatePackage(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if tenantID == "" {
		mw.RespondValidationError(c, "tenantId is required")
		return
	}

	var req dto.CreatePackageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	resp, err := h.pkgService.CreatePackage(c.Request.Context(), tenantID, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, resp)
}

func (h *PackageHandler) ListPackages(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if tenantID == "" {
		mw.RespondValidationError(c, "tenantId is required")
		return
	}

	resp, err := h.pkgService.ListPackages(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *PackageHandler) DeletePackage(c *gin.Context) {
	tenantID := c.Param("tenantId")
	packageID := c.Param("packageId")
	if tenantID == "" || packageID == "" {
		mw.RespondValidationError(c, "tenantId and packageId are required")
		return
	}

	if err := h.pkgService.DeletePackage(c.Request.Context(), tenantID, packageID); err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"message": "package deleted"})
}

func (h *PackageHandler) GetDownloadURL(c *gin.Context) {
	tenantID := c.Param("tenantId")
	packageID := c.Param("packageId")
	if tenantID == "" || packageID == "" {
		mw.RespondValidationError(c, "tenantId and packageId are required")
		return
	}

	url, err := h.pkgService.GetPresignedURL(c.Request.Context(), tenantID, packageID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"download_url": url})
}

func (h *PackageHandler) BindDevice(c *gin.Context) {
	tenantID := c.Param("tenantId")
	packageID := c.Param("packageId")
	adminUserID, _ := c.Get("user_id")

	var req dto.BindDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	actor := ""
	if uid, ok := adminUserID.(string); ok {
		actor = uid
	}

	resp, err := h.bindingService.BindManual(c.Request.Context(), tenantID, packageID, actor, req)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *PackageHandler) UnbindDevice(c *gin.Context) {
	tenantID := c.Param("tenantId")
	bindingID := c.Param("bindingId")
	adminUserID, _ := c.Get("user_id")

	actor := ""
	if uid, ok := adminUserID.(string); ok {
		actor = uid
	}

	if err := h.bindingService.Unbind(c.Request.Context(), tenantID, bindingID, actor); err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"message": "device unbound"})
}

func (h *PackageHandler) ListBindings(c *gin.Context) {
	packageID := c.Param("packageId")

	resp, err := h.bindingService.ListBindings(c.Request.Context(), packageID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

func (h *PackageHandler) UpdateMaxDevices(c *gin.Context) {
	tenantID := c.Param("tenantId")
	packageID := c.Param("packageId")

	var req dto.UpdateMaxDevicesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	if err := h.bindingService.UpdateMaxDevices(c.Request.Context(), tenantID, packageID, req.MaxDevices); err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"message": "max devices updated"})
}

func (h *PackageHandler) ListPackageAuditLogs(c *gin.Context) {
	packageID := c.Param("packageId")
	limit, offset := parsePagination(c)

	logs, total, err := h.bindingService.ListAuditLogsByPackage(c.Request.Context(), packageID, limit, offset)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"data": logs, "total": total})
}

func (h *PackageHandler) ListTenantAuditLogs(c *gin.Context) {
	tenantID := c.Param("tenantId")
	limit, offset := parsePagination(c)

	logs, total, err := h.bindingService.ListAuditLogs(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{"data": logs, "total": total})
}

func parsePagination(c *gin.Context) (int, int) {
	limit := 20
	offset := 0
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	if o, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && o >= 0 {
		offset = o
	}
	return limit, offset
}
