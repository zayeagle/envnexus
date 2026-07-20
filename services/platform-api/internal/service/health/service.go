package health

import (
	"context"
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type DeviceHealth struct {
	DeviceID     string     `json:"device_id"`
	TenantID     string     `json:"tenant_id"`
	Status       string     `json:"status"`
	LastSeen     *time.Time `json:"last_seen"`
	DriftCount   int        `json:"drift_count"`
	AgentVersion string     `json:"agent_version"`
}

type TenantHealthSummary struct {
	TenantID       string `json:"tenant_id"`
	TotalDevices   int    `json:"total_devices"`
	OnlineDevices  int    `json:"online_devices"`
	OfflineDevices int    `json:"offline_devices"`
	DegradedCount  int    `json:"degraded_count"`
	DriftCount     int    `json:"drift_count"`
}

type Service struct {
	deviceRepo repository.DeviceRepository
	govRepo    repository.GovernanceRepository
}

func NewService(deviceRepo repository.DeviceRepository, govRepo repository.GovernanceRepository) *Service {
	return &Service{deviceRepo: deviceRepo, govRepo: govRepo}
}

func (s *Service) GetTenantSummary(ctx context.Context, tenantID string) (*TenantHealthSummary, error) {
	devices, _, err := s.deviceRepo.ListByTenantID(ctx, tenantID, false, false, 0, 0)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-5 * time.Minute)
	summary := &TenantHealthSummary{TenantID: tenantID, TotalDevices: len(devices)}

	for _, d := range devices {
		if d.LastSeenAt != nil && d.LastSeenAt.After(cutoff) {
			summary.OnlineDevices++
		} else {
			summary.OfflineDevices++
		}
	}

	driftCount, _ := s.govRepo.CountDriftsByTenant(ctx, tenantID, true)
	summary.DriftCount = int(driftCount)

	return summary, nil
}

func (s *Service) ListDeviceHealth(ctx context.Context, tenantID string) ([]DeviceHealth, error) {
	devices, _, err := s.deviceRepo.ListByTenantID(ctx, tenantID, false, false, 0, 0)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-5 * time.Minute)
	var result []DeviceHealth
	for _, d := range devices {
		status := "offline"
		if d.LastSeenAt != nil && d.LastSeenAt.After(cutoff) {
			status = "online"
		}

		result = append(result, DeviceHealth{
			DeviceID:     d.ID,
			TenantID:     d.TenantID,
			Status:       status,
			LastSeen:     d.LastSeenAt,
			AgentVersion: d.AgentVersion,
		})
	}
	return result, nil
}
