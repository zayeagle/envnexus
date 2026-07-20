package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type EnrollmentRepository interface {
	Create(ctx context.Context, token *domain.EnrollmentToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.EnrollmentToken, error)
	Update(ctx context.Context, token *domain.EnrollmentToken) error
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.EnrollmentToken, error)
}

type MySQLEnrollmentRepository struct {
	db *gorm.DB
}

func NewMySQLEnrollmentRepository(db *gorm.DB) *MySQLEnrollmentRepository {
	return &MySQLEnrollmentRepository{db: db}
}

func (r *MySQLEnrollmentRepository) Create(ctx context.Context, token *domain.EnrollmentToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *MySQLEnrollmentRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.EnrollmentToken, error) {
	var et domain.EnrollmentToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&et).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &et, nil
}

func (r *MySQLEnrollmentRepository) Update(ctx context.Context, token *domain.EnrollmentToken) error {
	return r.db.WithContext(ctx).Save(token).Error
}

func (r *MySQLEnrollmentRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.EnrollmentToken, error) {
	var tokens []*domain.EnrollmentToken
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&tokens).Error
	return tokens, err
}

type DeviceRepository interface {
	Create(ctx context.Context, device *domain.Device) error
	GetByID(ctx context.Context, id string) (*domain.Device, error)
	Update(ctx context.Context, device *domain.Device) error
	// ListByTenantID returns devices with deleted_at IS NULL. When activeOnly is true, only status=active rows are returned.
	// When requirePlatformArch is true, only rows with non-empty platform and arch (after trim) are returned.
	ListByTenantID(ctx context.Context, tenantID string, activeOnly, requirePlatformArch bool, page, pageSize int) ([]*domain.Device, int64, error)
	Delete(ctx context.Context, id string, tenantID string) error
}

type MySQLDeviceRepository struct {
	db *gorm.DB
}

func NewMySQLDeviceRepository(db *gorm.DB) *MySQLDeviceRepository {
	return &MySQLDeviceRepository{db: db}
}

func (r *MySQLDeviceRepository) Create(ctx context.Context, device *domain.Device) error {
	return r.db.WithContext(ctx).Create(device).Error
}

func (r *MySQLDeviceRepository) GetByID(ctx context.Context, id string) (*domain.Device, error) {
	var device domain.Device
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

func (r *MySQLDeviceRepository) Update(ctx context.Context, device *domain.Device) error {
	return r.db.WithContext(ctx).Save(device).Error
}

func (r *MySQLDeviceRepository) ListByTenantID(ctx context.Context, tenantID string, activeOnly, requirePlatformArch bool, page, pageSize int) ([]*domain.Device, int64, error) {
	var devices []*domain.Device
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.Device{}).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if activeOnly {
		q = q.Where("status = ?", domain.DeviceStatusActive)
	}
	if requirePlatformArch {
		q = q.Where("TRIM(platform) <> ? AND TRIM(arch) <> ?", "", "")
	}
	// Count total records
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// Apply pagination
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	// Execute query
	err := q.Find(&devices).Error
	return devices, total, err
}

func (r *MySQLDeviceRepository) Delete(ctx context.Context, id string, tenantID string) error {
	return r.db.WithContext(ctx).Model(&domain.Device{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("deleted_at", gorm.Expr("NOW(3)")).Error
}
