package device

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/auth"
)

type Service struct {
	deviceRepo  repository.DeviceRepository
	authService *auth.Service
}

func NewService(deviceRepo repository.DeviceRepository, authService *auth.Service) *Service {
	return &Service{deviceRepo: deviceRepo, authService: authService}
}

func (s *Service) ListDevices(ctx context.Context, tenantID string, activeOnly, requirePlatformArch bool, page, pageSize int) ([]*dto.DeviceResponse, int64, error) {
	devices, total, err := s.deviceRepo.ListByTenantID(ctx, tenantID, activeOnly, requirePlatformArch, page, pageSize)
	if err != nil {
		return nil, 0, domain.ErrInternalError
	}

	var resp []*dto.DeviceResponse
	for _, d := range devices {
		hostname := ""
		if d.Hostname != nil {
			hostname = *d.Hostname
		}
		resp = append(resp, &dto.DeviceResponse{
			ID:                         d.ID,
			TenantID:                   d.TenantID,
			AgentProfileID:             d.AgentProfileID,
			DeviceName:                 d.DeviceName,
			Hostname:                   hostname,
			Platform:                   d.Platform,
			Arch:                       d.Arch,
			RuntimeMetadata:            rawJSONPtr(d.RuntimeMetadata),
			EnvironmentType:            d.EnvironmentType,
			AgentVersion:               d.AgentVersion,
			DistributionPackageVersion: d.DistributionPackageVersion,
			Status:                     string(d.Status),
			PolicyVersion:              d.PolicyVersion,
			LastSeenAt:                 d.LastSeenAt,
			CreatedAt:                  d.CreatedAt,
			UpdatedAt:                  d.UpdatedAt,
		})
	}
	return resp, total, nil
}

func (s *Service) UpdateDevice(ctx context.Context, tenantID, id string, req dto.UpdateDeviceRequest) (*dto.DeviceResponse, error) {
	device, err := s.deviceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if device == nil || device.TenantID != tenantID {
		return nil, domain.ErrDeviceNotFound
	}

	if req.DeviceName != "" {
		device.DeviceName = req.DeviceName
	}
	if req.Status != "" {
		device.Status = domain.DeviceStatus(req.Status)
	}

	if err := s.deviceRepo.Update(ctx, device); err != nil {
		return nil, domain.ErrInternalError
	}

	hostname := ""
	if device.Hostname != nil {
		hostname = *device.Hostname
	}
	return &dto.DeviceResponse{
		ID:                         device.ID,
		TenantID:                   device.TenantID,
		AgentProfileID:             device.AgentProfileID,
		DeviceName:                 device.DeviceName,
		Hostname:                   hostname,
		Platform:                   device.Platform,
		Arch:                       device.Arch,
		RuntimeMetadata:            rawJSONPtr(device.RuntimeMetadata),
		EnvironmentType:            device.EnvironmentType,
		AgentVersion:               device.AgentVersion,
		DistributionPackageVersion: device.DistributionPackageVersion,
		Status:                     string(device.Status),
		PolicyVersion:              device.PolicyVersion,
		LastSeenAt:                 device.LastSeenAt,
		CreatedAt:                  device.CreatedAt,
		UpdatedAt:                  device.UpdatedAt,
	}, nil
}

func (s *Service) DeleteDevice(ctx context.Context, tenantID, id string) error {
	return s.deviceRepo.Delete(ctx, id, tenantID)
}

// Heartbeat records a device heartbeat and returns updated state.
func (s *Service) Heartbeat(ctx context.Context, deviceID, agentVersion, distPkgVersion string, policyVersion int, env *dto.AgentRuntimeEnvironment, status string) (*domain.Device, error) {
	device, err := s.deviceRepo.GetByID(ctx, deviceID)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if device == nil {
		return nil, domain.ErrDeviceNotFound
	}
	if device.IsRevoked() {
		return nil, domain.ErrDeviceRevoked
	}

	now := time.Now()
	device.AgentVersion = agentVersion
	if distPkgVersion != "" {
		device.DistributionPackageVersion = strings.TrimPrefix(strings.TrimPrefix(distPkgVersion, "v"), "V")
	}
	device.PolicyVersion = policyVersion
	device.UpdatedAt = now

	// Update last seen time only if status is not offline
	if status != "offline" {
		device.LastSeenAt = &now
	}

	// Update status if provided
	if status != "" && status != "offline" {
		device.Status = domain.DeviceStatus(status)
	} else if device.Status == domain.DeviceStatusPendingActivation {
		device.Status = domain.DeviceStatusActive
	}

	if env != nil {
		if b, err := json.Marshal(env); err == nil {
			meta := string(b)
			device.RuntimeMetadata = &meta
		}
	}
	if err := s.deviceRepo.Update(ctx, device); err != nil {
		return nil, domain.ErrInternalError
	}
	return device, nil
}

func rawJSONPtr(s *string) json.RawMessage {
	if s == nil || *s == "" {
		return nil
	}
	return json.RawMessage(*s)
}

// GetConfig returns the full config payload for a device (agent profile + model + policy).
func (s *Service) GetConfig(ctx context.Context, deviceID string) (*domain.Device, error) {
	device, err := s.deviceRepo.GetByID(ctx, deviceID)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if device == nil {
		return nil, domain.ErrDeviceNotFound
	}
	return device, nil
}

// RotateDeviceToken revokes the current device token and issues a new one.
func (s *Service) RotateDeviceToken(ctx context.Context, tenantID, deviceID string) (string, error) {
	dev, err := s.deviceRepo.GetByID(ctx, deviceID)
	if err != nil {
		return "", domain.ErrInternalError
	}
	if dev == nil || dev.TenantID != tenantID {
		return "", domain.ErrDeviceNotFound
	}
	if dev.IsRevoked() {
		return "", domain.ErrDeviceRevoked
	}

	if s.authService == nil {
		return "", domain.ErrInternalError
	}

	newToken, err := s.authService.IssueDeviceToken(deviceID, tenantID)
	if err != nil {
		return "", domain.ErrInternalError
	}

	return newToken, nil
}
