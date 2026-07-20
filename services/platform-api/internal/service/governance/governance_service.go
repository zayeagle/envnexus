package governance

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Service struct {
	repo repository.GovernanceRepository
}

func NewService(repo repository.GovernanceRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListBaselines(ctx context.Context, tenantID, deviceID string) ([]*domain.GovernanceBaseline, error) {
	return s.repo.ListBaselinesByTenant(ctx, tenantID, deviceID)
}

func (s *Service) ListDrifts(ctx context.Context, tenantID string, filters repository.DriftFilters) ([]*domain.GovernanceDrift, error) {
	return s.repo.ListDriftsByTenant(ctx, tenantID, filters)
}

func (s *Service) ResolveDrift(ctx context.Context, id string) error {
	return s.repo.ResolveDrift(ctx, id)
}

func (s *Service) GetSummary(ctx context.Context, tenantID string) (*Summary, error) {
	baselineCount, err := s.repo.CountBaselinesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	totalDrifts, err := s.repo.CountDriftsByTenant(ctx, tenantID, false)
	if err != nil {
		return nil, err
	}
	unresolvedDrifts, err := s.repo.CountDriftsByTenant(ctx, tenantID, true)
	if err != nil {
		return nil, err
	}
	return &Summary{
		TotalBaselines:   baselineCount,
		TotalDrifts:      totalDrifts,
		UnresolvedDrifts: unresolvedDrifts,
	}, nil
}

type Summary struct {
	TotalBaselines   int64 `json:"total_baselines"`
	TotalDrifts      int64 `json:"total_drifts"`
	UnresolvedDrifts int64 `json:"unresolved_drifts"`
}

func (s *Service) ReportBaseline(ctx context.Context, deviceID, tenantID, snapshotJSON string) (*domain.GovernanceBaseline, error) {
	b := &domain.GovernanceBaseline{
		ID:           ulid.Make().String(),
		DeviceID:     deviceID,
		TenantID:     tenantID,
		SnapshotJSON: snapshotJSON,
		CapturedAt:   time.Now(),
	}
	if err := s.repo.CreateBaseline(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) ReportDrifts(ctx context.Context, deviceID, tenantID string, drifts []DriftReport) (int, error) {
	latest, _ := s.repo.GetLatestBaseline(ctx, deviceID)
	var baselineID *string
	if latest != nil {
		baselineID = &latest.ID
	}

	now := time.Now()
	var records []*domain.GovernanceDrift
	for _, d := range drifts {
		records = append(records, &domain.GovernanceDrift{
			ID:            ulid.Make().String(),
			DeviceID:      deviceID,
			TenantID:      tenantID,
			BaselineID:    baselineID,
			DriftType:     d.DriftType,
			KeyName:       d.KeyName,
			ExpectedValue: d.ExpectedValue,
			ActualValue:   d.ActualValue,
			Severity:      d.Severity,
			DetectedAt:    now,
		})
	}
	if err := s.repo.CreateDriftBatch(ctx, records); err != nil {
		return 0, err
	}
	return len(records), nil
}

type DriftReport struct {
	DriftType     string  `json:"drift_type"`
	KeyName       string  `json:"key_name"`
	ExpectedValue *string `json:"expected_value"`
	ActualValue   *string `json:"actual_value"`
	Severity      string  `json:"severity"`
}
