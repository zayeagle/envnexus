package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type GovernanceRepository interface {
	CreateBaseline(ctx context.Context, b *domain.GovernanceBaseline) error
	ListBaselinesByTenant(ctx context.Context, tenantID string, deviceID string) ([]*domain.GovernanceBaseline, error)
	GetLatestBaseline(ctx context.Context, deviceID string) (*domain.GovernanceBaseline, error)

	CreateDrift(ctx context.Context, d *domain.GovernanceDrift) error
	CreateDriftBatch(ctx context.Context, drifts []*domain.GovernanceDrift) error
	ListDriftsByTenant(ctx context.Context, tenantID string, filters DriftFilters) ([]*domain.GovernanceDrift, error)
	ResolveDrift(ctx context.Context, id string) error

	CountBaselinesByTenant(ctx context.Context, tenantID string) (int64, error)
	CountDriftsByTenant(ctx context.Context, tenantID string, unresolvedOnly bool) (int64, error)
}

type DriftFilters struct {
	DeviceID       string
	Severity       string
	UnresolvedOnly bool
}

type MySQLGovernanceRepository struct {
	db *gorm.DB
}

func NewMySQLGovernanceRepository(db *gorm.DB) *MySQLGovernanceRepository {
	return &MySQLGovernanceRepository{db: db}
}

func (r *MySQLGovernanceRepository) CreateBaseline(ctx context.Context, b *domain.GovernanceBaseline) error {
	return r.db.WithContext(ctx).Create(b).Error
}

func (r *MySQLGovernanceRepository) ListBaselinesByTenant(ctx context.Context, tenantID string, deviceID string) ([]*domain.GovernanceBaseline, error) {
	query := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}
	var baselines []*domain.GovernanceBaseline
	err := query.Order("captured_at DESC").Limit(100).Find(&baselines).Error
	return baselines, err
}

func (r *MySQLGovernanceRepository) GetLatestBaseline(ctx context.Context, deviceID string) (*domain.GovernanceBaseline, error) {
	var b domain.GovernanceBaseline
	err := r.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("captured_at DESC").First(&b).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (r *MySQLGovernanceRepository) CreateDrift(ctx context.Context, d *domain.GovernanceDrift) error {
	return r.db.WithContext(ctx).Create(d).Error
}

func (r *MySQLGovernanceRepository) CreateDriftBatch(ctx context.Context, drifts []*domain.GovernanceDrift) error {
	if len(drifts) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&drifts).Error
}

func (r *MySQLGovernanceRepository) ListDriftsByTenant(ctx context.Context, tenantID string, filters DriftFilters) ([]*domain.GovernanceDrift, error) {
	query := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if filters.DeviceID != "" {
		query = query.Where("device_id = ?", filters.DeviceID)
	}
	if filters.Severity != "" {
		query = query.Where("severity = ?", filters.Severity)
	}
	if filters.UnresolvedOnly {
		query = query.Where("resolved_at IS NULL")
	}
	var drifts []*domain.GovernanceDrift
	err := query.Order("detected_at DESC").Limit(200).Find(&drifts).Error
	return drifts, err
}

func (r *MySQLGovernanceRepository) ResolveDrift(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&domain.GovernanceDrift{}).Where("id = ?", id).
		Update("resolved_at", gorm.Expr("NOW(3)")).Error
}

func (r *MySQLGovernanceRepository) CountBaselinesByTenant(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.GovernanceBaseline{}).Where("tenant_id = ?", tenantID).Count(&count).Error
	return count, err
}

func (r *MySQLGovernanceRepository) CountDriftsByTenant(ctx context.Context, tenantID string, unresolvedOnly bool) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&domain.GovernanceDrift{}).Where("tenant_id = ?", tenantID)
	if unresolvedOnly {
		query = query.Where("resolved_at IS NULL")
	}
	err := query.Count(&count).Error
	return count, err
}
