package device_binding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

const (
	fingerprintMatchThreshold = 3 // at least 3 of 5 components must match
	totalComponentTypes       = 5
)

type Service struct {
	bindingRepo repository.DeviceBindingRepository
	pkgRepo     repository.PackageRepository
}

func NewService(bindingRepo repository.DeviceBindingRepository, pkgRepo repository.PackageRepository) *Service {
	return &Service{
		bindingRepo: bindingRepo,
		pkgRepo:     pkgRepo,
	}
}

// RegisterDevice — Agent calls this in manual mode to register its device code
func (s *Service) RegisterDevice(ctx context.Context, req dto.RegisterDeviceRequest) (*dto.RegisterDeviceResponse, error) {
	hwHash := computeCompositeHash(req.Components)
	deviceCode := generateDeviceCode(hwHash)

	existing, err := s.bindingRepo.GetPendingByCode(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &dto.RegisterDeviceResponse{DeviceCode: existing.DeviceCode}, nil
	}

	var infoStr *string
	if req.DeviceInfo != nil {
		b, _ := json.Marshal(req.DeviceInfo)
		s := string(b)
		infoStr = &s
	}

	pending := &domain.PendingDevice{
		ID:           ulid.Make().String(),
		DeviceCode:   deviceCode,
		HardwareHash: hwHash,
		DeviceInfo:   infoStr,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.bindingRepo.CreatePending(ctx, pending); err != nil {
		return nil, err
	}

	components := buildComponents(deviceCode, req.Components)
	if err := s.bindingRepo.CreateComponents(ctx, components); err != nil {
		return nil, err
	}

	return &dto.RegisterDeviceResponse{DeviceCode: deviceCode}, nil
}

// ActivateAuto — Agent calls this in auto mode with activation_key
func (s *Service) ActivateAuto(ctx context.Context, req dto.ActivateDeviceRequest) (*dto.ActivateDeviceResponse, error) {
	keyHash := hashActivationKey(req.ActivationKey)
	pkg, err := s.findPackageByKeyHash(ctx, keyHash)
	if err != nil {
		slog.Error("[device_binding] findPackageByKeyHash failed", "error", err)
		return nil, err
	}
	if pkg == nil {
		slog.Warn("[device_binding] No package found for activation key hash", "key_hash_prefix", keyHash[:8]+"...")
		return &dto.ActivateDeviceResponse{Error: domain.ErrActivationKeyInvalid.Message}, domain.ErrActivationKeyInvalid
	}
	slog.Info("[device_binding] Package found for activation",
		"package_id", pkg.ID,
		"activation_mode", pkg.ActivationMode,
		"max_devices", pkg.MaxDevices,
		"bound_count", pkg.BoundCount,
	)
	if !pkg.SupportsAutoActivation() {
		return &dto.ActivateDeviceResponse{Error: "package requires manual activation"}, domain.ErrActivationKeyInvalid
	}

	resp, err := s.bindDevice(ctx, pkg, req.DeviceCode, req.Components, "system")
	if err != nil {
		slog.Error("[device_binding] bindDevice failed", "device_code", req.DeviceCode, "error", err)
	}
	return resp, err
}

// BindManual — Admin binds a pending device to a package (manual mode)
func (s *Service) BindManual(ctx context.Context, tenantID, packageID, adminUserID string, req dto.BindDeviceRequest) (*dto.BindDeviceResponse, error) {
	pkg, err := s.pkgRepo.GetByID(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg == nil || pkg.TenantID != tenantID {
		return nil, domain.ErrPackageNotFound
	}

	pending, err := s.bindingRepo.GetPendingByCode(ctx, req.DeviceCode)
	if err != nil {
		return nil, err
	}
	if pending == nil {
		return nil, domain.ErrDeviceCodeNotFound
	}

	components, err := s.bindingRepo.GetComponentsByCode(ctx, req.DeviceCode)
	if err != nil {
		return nil, err
	}
	compInfos := domainComponentsToDTO(components)

	resp, err := s.bindDevice(ctx, pkg, req.DeviceCode, compInfos, adminUserID)
	if err != nil {
		return nil, err
	}

	return &dto.BindDeviceResponse{
		BindingID:  resp.PackageID,
		DeviceCode: req.DeviceCode,
		Status:     domain.BindingStatusActive,
		BoundAt:    time.Now(),
	}, nil
}

// Unbind — Admin revokes a device binding
func (s *Service) Unbind(ctx context.Context, tenantID, bindingID, adminUserID string) error {
	binding, err := s.bindingRepo.GetByID(ctx, bindingID)
	if err != nil {
		return err
	}
	if binding == nil {
		return domain.ErrBindingNotFound
	}

	if err := s.bindingRepo.UpdateStatus(ctx, bindingID, domain.BindingStatusRevoked); err != nil {
		return err
	}
	if err := s.bindingRepo.DecrementBoundCount(ctx, binding.PackageID); err != nil {
		return err
	}

	s.logAudit(ctx, tenantID, binding.PackageID, binding.DeviceCode, domain.AuditActionUnbind, adminUserID, nil)
	return nil
}

// CheckHeartbeat — validates device is still active
func (s *Service) CheckHeartbeat(ctx context.Context, req dto.HeartbeatRequest) (*dto.HeartbeatResponse, error) {
	binding, err := s.bindingRepo.GetByDeviceCode(ctx, req.DeviceCode)
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.Status == domain.BindingStatusRevoked {
		return &dto.HeartbeatResponse{Status: "revoked"}, nil
	}

	if len(req.Components) > 0 {
		stored, err := s.bindingRepo.GetComponentsByCode(ctx, req.DeviceCode)
		if err != nil {
			return nil, err
		}
		if !matchFingerprint(stored, req.Components) {
			s.logAudit(ctx, binding.TenantID, binding.PackageID, binding.DeviceCode, domain.AuditActionHeartbeatFail, "system", map[string]string{"reason": "fingerprint_mismatch"})
			return &dto.HeartbeatResponse{Status: "revoked"}, nil
		}
	}

	_ = s.bindingRepo.UpdateHeartbeat(ctx, req.DeviceCode)
	return &dto.HeartbeatResponse{Status: "ok"}, nil
}

// UpdateMaxDevices — Admin updates the max device count for a package
func (s *Service) UpdateMaxDevices(ctx context.Context, tenantID, packageID string, maxDevices int) error {
	pkg, err := s.pkgRepo.GetByID(ctx, packageID)
	if err != nil {
		return err
	}
	if pkg == nil || pkg.TenantID != tenantID {
		return domain.ErrPackageNotFound
	}
	return s.pkgRepo.UpdateMaxDevices(ctx, packageID, maxDevices)
}

// ListBindings — list all device bindings for a package
func (s *Service) ListBindings(ctx context.Context, packageID string) ([]*dto.DeviceBindingResponse, error) {
	bindings, err := s.bindingRepo.ListByPackage(ctx, packageID)
	if err != nil {
		return nil, err
	}
	return s.toBindingResponses(bindings), nil
}

// ListAuditLogs — paginated audit logs for a tenant
func (s *Service) ListAuditLogs(ctx context.Context, tenantID string, limit, offset int) ([]*dto.ActivationAuditLogResponse, int64, error) {
	logs, total, err := s.bindingRepo.ListAuditLogs(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return s.toAuditResponses(logs), total, nil
}

// ListAuditLogsByPackage — paginated audit logs for a specific package
func (s *Service) ListAuditLogsByPackage(ctx context.Context, packageID string, limit, offset int) ([]*dto.ActivationAuditLogResponse, int64, error) {
	logs, total, err := s.bindingRepo.ListAuditLogsByPackage(ctx, packageID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return s.toAuditResponses(logs), total, nil
}

// GetActivationStatus — Agent checks if its device code is bound
func (s *Service) GetActivationStatus(ctx context.Context, deviceCode string) (*dto.ActivationStatusResponse, error) {
	binding, err := s.bindingRepo.GetByDeviceCode(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return &dto.ActivationStatusResponse{Activated: false}, nil
	}

	pkg, err := s.pkgRepo.GetByID(ctx, binding.PackageID)
	if err != nil {
		return nil, err
	}

	return &dto.ActivationStatusResponse{
		Activated:      binding.Status == domain.BindingStatusActive,
		PackageID:      binding.PackageID,
		TenantID:       binding.TenantID,
		ActivationMode: pkg.ActivationMode,
	}, nil
}

// MigrateBindingForVersionChange moves a device's binding from the old package
// to the new package when the agent reports an updated distribution_package_version.
func (s *Service) MigrateBindingForVersionChange(ctx context.Context, deviceCode, tenantID, platform, arch, newVersion string) error {
	newVersion = stripVersionPrefix(newVersion)
	if deviceCode == "" || newVersion == "" {
		return nil
	}

	binding, err := s.bindingRepo.GetByDeviceCode(ctx, deviceCode)
	if err != nil || binding == nil {
		return err
	}

	oldPkg, err := s.pkgRepo.GetByID(ctx, binding.PackageID)
	if err != nil || oldPkg == nil {
		return err
	}

	if stripVersionPrefix(oldPkg.Version) == newVersion {
		return nil
	}

	packages, err := s.pkgRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return err
	}

	var newPkg *domain.DownloadPackage
	for _, pkg := range packages {
		if pkg.Platform == platform && pkg.Arch == arch && stripVersionPrefix(pkg.Version) == newVersion && pkg.Status == "ready" {
			newPkg = pkg
			break
		}
	}
	if newPkg == nil {
		slog.Warn("[device_binding] No matching package found for version migration",
			"device_code", deviceCode,
			"new_version", newVersion,
			"platform", platform,
			"arch", arch,
		)
		return nil
	}

	if err := s.bindingRepo.UpdateBindingPackage(ctx, binding.ID, newPkg.ID); err != nil {
		slog.Error("[device_binding] Failed to update binding package", "binding_id", binding.ID, "error", err)
		return err
	}
	if err := s.bindingRepo.DecrementBoundCount(ctx, oldPkg.ID); err != nil {
		slog.Error("[device_binding] Failed to decrement old package bound_count", "package_id", oldPkg.ID, "error", err)
	}
	if err := s.bindingRepo.IncrementBoundCount(ctx, newPkg.ID); err != nil {
		slog.Error("[device_binding] Failed to increment new package bound_count", "package_id", newPkg.ID, "error", err)
	}

	s.logAudit(ctx, tenantID, newPkg.ID, deviceCode, "version_migrate", "system",
		map[string]string{"from_package": oldPkg.ID, "from_version": oldPkg.Version, "to_version": newVersion})

	slog.Info("[device_binding] Migrated binding to new package version",
		"device_code", deviceCode,
		"old_package", oldPkg.ID,
		"old_version", oldPkg.Version,
		"new_package", newPkg.ID,
		"new_version", newVersion,
	)
	return nil
}

// --- internal helpers ---

func (s *Service) bindDevice(ctx context.Context, pkg *domain.DownloadPackage, deviceCode string, components []dto.ComponentInfo, actor string) (*dto.ActivateDeviceResponse, error) {
	existing, err := s.bindingRepo.GetByDeviceCode(ctx, deviceCode)
	if err != nil {
		slog.Error("[device_binding] GetByDeviceCode failed", "device_code", deviceCode, "error", err)
		return nil, err
	}
	if existing != nil {
		return &dto.ActivateDeviceResponse{Error: domain.ErrDeviceAlreadyBound.Message}, domain.ErrDeviceAlreadyBound
	}

	if pkg.BoundCount >= pkg.MaxDevices {
		return &dto.ActivateDeviceResponse{Error: domain.ErrDeviceLimitReached.Message}, domain.ErrDeviceLimitReached
	}

	hwHash := computeCompositeHash(components)
	var infoStr *string

	binding := &domain.DeviceBinding{
		ID:           ulid.Make().String(),
		TenantID:     pkg.TenantID,
		PackageID:    pkg.ID,
		DeviceCode:   deviceCode,
		HardwareHash: hwHash,
		DeviceInfo:   infoStr,
		Status:       domain.BindingStatusActive,
		BoundAt:      time.Now(),
		BoundBy:      actor,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.bindingRepo.CreateBinding(ctx, binding); err != nil {
		slog.Error("[device_binding] CreateBinding failed", "device_code", deviceCode, "error", err)
		return nil, err
	}
	if err := s.bindingRepo.IncrementBoundCount(ctx, pkg.ID); err != nil {
		slog.Error("[device_binding] IncrementBoundCount failed", "package_id", pkg.ID, "error", err)
		return nil, err
	}

	comps := buildComponents(deviceCode, components)
	_ = s.bindingRepo.CreateComponents(ctx, comps)

	_ = s.bindingRepo.DeletePending(ctx, deviceCode)

	action := domain.AuditActionActivate
	if actor != "system" {
		action = domain.AuditActionBind
	}
	s.logAudit(ctx, pkg.TenantID, pkg.ID, deviceCode, action, actor, nil)

	return &dto.ActivateDeviceResponse{
		Activated: true,
		PackageID: pkg.ID,
		TenantID:  pkg.TenantID,
	}, nil
}

func (s *Service) findPackageByKeyHash(ctx context.Context, keyHash string) (*domain.DownloadPackage, error) {
	return s.pkgRepo.GetByActivationKeyHash(ctx, keyHash)
}

func (s *Service) logAudit(ctx context.Context, tenantID, packageID, deviceCode, action, actor string, detail interface{}) {
	var detailStr *string
	if detail != nil {
		b, _ := json.Marshal(detail)
		s := string(b)
		detailStr = &s
	}
	log := &domain.ActivationAuditLog{
		ID:         ulid.Make().String(),
		TenantID:   tenantID,
		PackageID:  packageID,
		DeviceCode: deviceCode,
		Action:     action,
		Actor:      actor,
		Detail:     detailStr,
		CreatedAt:  time.Now(),
	}
	_ = s.bindingRepo.CreateAuditLog(ctx, log)
}

func (s *Service) toBindingResponses(bindings []*domain.DeviceBinding) []*dto.DeviceBindingResponse {
	res := make([]*dto.DeviceBindingResponse, 0, len(bindings))
	for _, b := range bindings {
		r := &dto.DeviceBindingResponse{
			ID:            b.ID,
			DeviceCode:    b.DeviceCode,
			Status:        b.Status,
			BoundBy:       b.BoundBy,
			BoundAt:       b.BoundAt,
			LastHeartbeat: b.LastHeartbeat,
		}
		if b.DeviceInfo != nil {
			var info dto.DeviceInfo
			if json.Unmarshal([]byte(*b.DeviceInfo), &info) == nil {
				r.DeviceInfo = &info
			}
		}
		res = append(res, r)
	}
	return res
}

func (s *Service) toAuditResponses(logs []*domain.ActivationAuditLog) []*dto.ActivationAuditLogResponse {
	res := make([]*dto.ActivationAuditLogResponse, 0, len(logs))
	for _, l := range logs {
		res = append(res, &dto.ActivationAuditLogResponse{
			ID:         l.ID,
			PackageID:  l.PackageID,
			DeviceCode: l.DeviceCode,
			Action:     l.Action,
			Actor:      l.Actor,
			Detail:     l.Detail,
			CreatedAt:  l.CreatedAt,
		})
	}
	return res
}

// --- crypto / fingerprint helpers ---

func computeCompositeHash(components []dto.ComponentInfo) string {
	var parts []string
	for _, c := range components {
		parts = append(parts, c.Type+":"+c.Hash)
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", h)
}

func hashActivationKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h)
}

func generateDeviceCode(hwHash string) string {
	raw := sha256.Sum256([]byte(hwHash))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:10])
	code := strings.ToUpper(encoded[:12])
	return fmt.Sprintf("ENX-%s-%s-%s", code[0:4], code[4:8], code[8:12])
}

func GenerateActivationKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return strings.ToUpper(encoded[:32])
}

func HashActivationKey(key string) string {
	return hashActivationKey(key)
}

func buildComponents(deviceCode string, components []dto.ComponentInfo) []*domain.DeviceComponent {
	comps := make([]*domain.DeviceComponent, 0, len(components))
	for _, c := range components {
		comps = append(comps, &domain.DeviceComponent{
			ID:            ulid.Make().String(),
			DeviceCode:    deviceCode,
			ComponentType: c.Type,
			ComponentHash: c.Hash,
			CreatedAt:     time.Now(),
		})
	}
	return comps
}

func matchFingerprint(stored []*domain.DeviceComponent, incoming []dto.ComponentInfo) bool {
	storedMap := make(map[string]string)
	for _, c := range stored {
		storedMap[c.ComponentType] = c.ComponentHash
	}

	matched := 0
	for _, c := range incoming {
		if hash, ok := storedMap[c.Type]; ok && hash == c.Hash {
			matched++
		}
	}
	return matched >= fingerprintMatchThreshold
}

func domainComponentsToDTO(comps []*domain.DeviceComponent) []dto.ComponentInfo {
	res := make([]dto.ComponentInfo, 0, len(comps))
	for _, c := range comps {
		res = append(res, dto.ComponentInfo{Type: c.ComponentType, Hash: c.ComponentHash})
	}
	return res
}

func stripVersionPrefix(v string) string {
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		return v[1:]
	}
	return v
}
