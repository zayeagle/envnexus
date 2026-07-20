package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type ModelProfileRepository interface {
	GetByID(ctx context.Context, id string, tenantID string) (*domain.ModelProfile, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.ModelProfile, error)
	Create(ctx context.Context, profile *domain.ModelProfile) error
	Update(ctx context.Context, profile *domain.ModelProfile) error
	Delete(ctx context.Context, id string, tenantID string) error
}

type MySQLModelProfileRepository struct {
	db *gorm.DB
}

func NewMySQLModelProfileRepository(db *gorm.DB) *MySQLModelProfileRepository {
	return &MySQLModelProfileRepository{db: db}
}

func (r *MySQLModelProfileRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.ModelProfile, error) {
	var profiles []*domain.ModelProfile
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID).Find(&profiles).Error
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

func (r *MySQLModelProfileRepository) Create(ctx context.Context, profile *domain.ModelProfile) error {
	return r.db.WithContext(ctx).Create(profile).Error
}

func (r *MySQLModelProfileRepository) GetByID(ctx context.Context, id string, tenantID string) (*domain.ModelProfile, error) {
	var profile domain.ModelProfile
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", id, tenantID).First(&profile).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &profile, nil
}

func (r *MySQLModelProfileRepository) Update(ctx context.Context, profile *domain.ModelProfile) error {
	return r.db.WithContext(ctx).Save(profile).Error
}

func (r *MySQLModelProfileRepository) Delete(ctx context.Context, id string, tenantID string) error {
	// Soft delete
	return r.db.WithContext(ctx).Model(&domain.ModelProfile{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("deleted_at", gorm.Expr("NOW()")).Error
}
