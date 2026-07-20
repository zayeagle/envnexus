package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type PolicyProfileRepository interface {
	GetByID(ctx context.Context, id string, tenantID string) (*domain.PolicyProfile, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.PolicyProfile, error)
	Create(ctx context.Context, profile *domain.PolicyProfile) error
	Update(ctx context.Context, profile *domain.PolicyProfile) error
	Delete(ctx context.Context, id string, tenantID string) error
}

type MySQLPolicyProfileRepository struct {
	db *gorm.DB
}

func NewMySQLPolicyProfileRepository(db *gorm.DB) *MySQLPolicyProfileRepository {
	return &MySQLPolicyProfileRepository{db: db}
}

func (r *MySQLPolicyProfileRepository) GetByID(ctx context.Context, id string, tenantID string) (*domain.PolicyProfile, error) {
	var profile domain.PolicyProfile
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", id, tenantID).First(&profile).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &profile, nil
}

func (r *MySQLPolicyProfileRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.PolicyProfile, error) {
	var profiles []*domain.PolicyProfile
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID).Find(&profiles).Error
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

func (r *MySQLPolicyProfileRepository) Create(ctx context.Context, profile *domain.PolicyProfile) error {
	return r.db.WithContext(ctx).Create(profile).Error
}

func (r *MySQLPolicyProfileRepository) Update(ctx context.Context, profile *domain.PolicyProfile) error {
	return r.db.WithContext(ctx).Save(profile).Error
}

func (r *MySQLPolicyProfileRepository) Delete(ctx context.Context, id string, tenantID string) error {
	return r.db.WithContext(ctx).Model(&domain.PolicyProfile{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("deleted_at", gorm.Expr("NOW()")).Error
}
