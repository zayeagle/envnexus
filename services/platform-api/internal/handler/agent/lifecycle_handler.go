package agent

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/infrastructure"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/device"
	device_binding "github.com/zayeagle/envnexus/services/platform-api/internal/service/device_binding"
)

// LifecycleHandler handles agent heartbeat and config pull.
// Device identity is resolved by device.Service; profile lookups go
// through their respective repositories (no dedicated profile service exists yet).
type LifecycleHandler struct {
	deviceService     *device.Service
	bindingService    *device_binding.Service
	agentProfileRepo  repository.AgentProfileRepository
	modelProfileRepo  repository.ModelProfileRepository
	policyProfileRepo repository.PolicyProfileRepository
	packageRepo       repository.PackageRepository
	minioClient       *infrastructure.MinIOClient
}

func NewLifecycleHandler(
	deviceService *device.Service,
	bindingService *device_binding.Service,
	agentProfileRepo repository.AgentProfileRepository,
	modelProfileRepo repository.ModelProfileRepository,
	policyProfileRepo repository.PolicyProfileRepository,
	packageRepo repository.PackageRepository,
	minioClient *infrastructure.MinIOClient,
) *LifecycleHandler {
	return &LifecycleHandler{
		deviceService:     deviceService,
		bindingService:    bindingService,
		agentProfileRepo:  agentProfileRepo,
		modelProfileRepo:  modelProfileRepo,
		policyProfileRepo: policyProfileRepo,
		packageRepo:       packageRepo,
		minioClient:       minioClient,
	}
}

func (h *LifecycleHandler) RegisterRoutes(router *gin.RouterGroup) {
	agentGroup := router.Group("/agent/v1")
	{
		agentGroup.POST("/heartbeat", h.Heartbeat)
		agentGroup.GET("/config", h.GetConfig)
		agentGroup.GET("/check-update", h.CheckUpdate)
	}
}

func (h *LifecycleHandler) Heartbeat(c *gin.Context) {
	var req dto.AgentHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	deviceID := req.DeviceID
	if ctxDeviceID, exists := c.Get("device_id"); exists {
		deviceID = ctxDeviceID.(string)
	}

	dev, err := h.deviceService.Heartbeat(c.Request.Context(), deviceID, req.AgentVersion, req.DistributionPackageVersion, req.PolicyVersion, req.Environment, req.Status)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	if h.bindingService != nil && req.DeviceCode != "" && req.DistributionPackageVersion != "" {
		if err := h.bindingService.MigrateBindingForVersionChange(
			c.Request.Context(),
			req.DeviceCode,
			dev.TenantID,
			dev.Platform,
			dev.Arch,
			req.DistributionPackageVersion,
		); err != nil {
			slog.Warn("[lifecycle] binding migration failed (non-fatal)",
				"device_id", deviceID,
				"device_code", req.DeviceCode,
				"version", req.DistributionPackageVersion,
				"error", err,
			)
		}
	}

	mw.RespondSuccess(c, http.StatusOK, gin.H{
		"status":         "ok",
		"config_version": dev.PolicyVersion,
	})
}

func (h *LifecycleHandler) GetConfig(c *gin.Context) {
	deviceID := c.Query("device_id")
	if ctxDeviceID, exists := c.Get("device_id"); exists {
		deviceID = ctxDeviceID.(string)
	}
	if deviceID == "" {
		mw.RespondValidationError(c, "device_id is required")
		return
	}

	dev, err := h.deviceService.GetConfig(c.Request.Context(), deviceID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	var agentProfile interface{}
	var modelProfile interface{}
	var policyProfile interface{}

	if dev.AgentProfileID != "" {
		ap, _ := h.agentProfileRepo.GetByID(c.Request.Context(), dev.AgentProfileID, dev.TenantID)
		if ap != nil {
			agentProfile = ap
			mp, _ := h.modelProfileRepo.GetByID(c.Request.Context(), ap.ModelProfileID, dev.TenantID)
			modelProfile = mp
			pp, _ := h.policyProfileRepo.GetByID(c.Request.Context(), ap.PolicyProfileID, dev.TenantID)
			policyProfile = pp
		}
	}

	currentVersion := 0
	if v := c.Query("current_config_version"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			currentVersion = parsed
		}
	}

	mw.RespondSuccess(c, http.StatusOK, dto.AgentConfigResponse{
		HasUpdate:     dev.PolicyVersion > currentVersion,
		ConfigVersion: dev.PolicyVersion,
		AgentProfile:  agentProfile,
		ModelProfile:  modelProfile,
		PolicyProfile: policyProfile,
	})
}

// CheckUpdate returns the latest available agent-core binary version for the
// requesting device's platform/arch. Compares semver with the caller's current
// version and provides a presigned download URL when an update is available.
func (h *LifecycleHandler) CheckUpdate(c *gin.Context) {
	currentVersion := c.Query("current_version")
	platform := c.Query("platform")
	arch := c.Query("arch")

	if currentVersion == "" || platform == "" || arch == "" {
		slog.Warn("[agent] check-update missing query params",
			"current_version", currentVersion,
			"platform", platform,
			"arch", arch,
		)
		mw.RespondValidationError(c, "current_version, platform, and arch are required")
		return
	}

	deviceID, _ := c.Get("device_id")

	// Look up the device to get tenant_id for scoped package lookup
	var tenantID string
	if did, ok := deviceID.(string); ok && did != "" {
		dev, err := h.deviceService.GetConfig(c.Request.Context(), did)
		if err == nil && dev != nil {
			tenantID = dev.TenantID
		}
	}

	didStr := ""
	if v, ok := deviceID.(string); ok {
		didStr = v
	}
	slog.Info("[agent] check-update request",
		"device_id", didStr,
		"tenant_id", tenantID,
		"current_version", currentVersion,
		"platform", platform,
		"arch", arch,
	)

	if tenantID == "" {
		mw.RespondSuccess(c, http.StatusOK, dto.CheckUpdateResponse{
			HasUpdate: false,
			Message:   "device not associated with a tenant",
		})
		return
	}

	if h.packageRepo == nil {
		mw.RespondSuccess(c, http.StatusOK, dto.CheckUpdateResponse{
			HasUpdate: false,
			Message:   "package repository not available",
		})
		return
	}

	packages, err := h.packageRepo.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	// Find the latest ready package matching platform/arch
	var best *domain.DownloadPackage
	for _, pkg := range packages {
		if pkg.Status != "ready" {
			continue
		}
		if pkg.Platform != platform || pkg.Arch != arch {
			continue
		}
		if best == nil || compareSemver(pkg.Version, best.Version) > 0 {
			best = pkg
		}
	}

	hasUpdate := best != nil && compareSemver(best.Version, currentVersion) > 0
	latestReady := ""
	if best != nil {
		latestReady = best.Version
	}
	slog.Info("[agent] check-update resolved",
		"tenant_id", tenantID,
		"current_version", currentVersion,
		"platform", platform,
		"arch", arch,
		"latest_ready_match", latestReady,
		"has_update", hasUpdate,
	)

	if best == nil || compareSemver(best.Version, currentVersion) <= 0 {
		mw.RespondSuccess(c, http.StatusOK, dto.CheckUpdateResponse{
			HasUpdate:      false,
			CurrentVersion: currentVersion,
		})
		return
	}

	resp := dto.CheckUpdateResponse{
		HasUpdate:      true,
		CurrentVersion: currentVersion,
		LatestVersion:  best.Version,
		PackageID:      best.ID,
		Checksum:       best.Checksum,
		ArtifactSize:   best.ArtifactSize,
	}

	// The agent-core updater expects a raw binary, not the distribution
	// package ZIP stored in ArtifactPath. Serve a presigned URL to the
	// raw binary in base-packages/ which the build pipeline uploads
	// separately (e.g. enx-agent-windows-amd64.exe).
	if h.minioClient != nil {
		ext := ""
		if platform == "windows" {
			ext = ".exe"
		}
		rawBinaryKey := fmt.Sprintf("base-packages/enx-agent-%s-%s%s", platform, arch, ext)
		ctx := c.Request.Context()

		if h.minioClient.ObjectExists(ctx, rawBinaryKey) {
			downloadURL, err := h.minioClient.PresignedGetURL(ctx, rawBinaryKey, 30*time.Minute)
			if err == nil {
				resp.DownloadURL = downloadURL.String()
				// Package checksum/size are for the distribution ZIP, not
				// the raw binary. Clear them to prevent the updater from
				// rejecting a valid download due to mismatch.
				resp.Checksum = ""
				resp.ArtifactSize = 0
			}
		} else {
			slog.Warn("[agent] check-update: raw binary not found in base-packages, update will not include download URL",
				"raw_key", rawBinaryKey)
		}
	}

	mw.RespondSuccess(c, http.StatusOK, resp)
}

// compareSemver does a simple lexicographic semver comparison (major.minor.patch).
// Returns >0 if a > b, <0 if a < b, 0 if equal.
func compareSemver(a, b string) int {
	partsA := parseSemverParts(a)
	partsB := parseSemverParts(b)
	for i := 0; i < 3; i++ {
		if partsA[i] != partsB[i] {
			return partsA[i] - partsB[i]
		}
	}
	return 0
}

func parseSemverParts(v string) [3]int {
	var parts [3]int
	// Strip leading 'v' if present
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	idx := 0
	for _, seg := range splitDot(v) {
		if idx >= 3 {
			break
		}
		n, _ := strconv.Atoi(seg)
		parts[idx] = n
		idx++
	}
	return parts
}

func splitDot(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

// deviceStatusFromRequest maps an agent-reported status string to a domain status.
func deviceStatusFromRequest(s string) domain.DeviceStatus {
	switch s {
	case "active":
		return domain.DeviceStatusActive
	case "quarantined":
		return domain.DeviceStatusQuarantined
	default:
		return domain.DeviceStatusActive
	}
}
