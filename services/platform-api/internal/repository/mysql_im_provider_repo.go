package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type MySQLIMProviderRepository struct {
	db *gorm.DB
}

func NewMySQLIMProviderRepository(db *gorm.DB) *MySQLIMProviderRepository {
	return &MySQLIMProviderRepository{db: db}
}

func (r *MySQLIMProviderRepository) Create(ctx context.Context, provider *domain.IMProvider) error {
	return r.db.WithContext(ctx).Create(provider).Error
}

func (r *MySQLIMProviderRepository) GetByID(ctx context.Context, id string) (*domain.IMProvider, error) {
	var provider domain.IMProvider
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&provider).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &provider, nil
}

func (r *MySQLIMProviderRepository) Update(ctx context.Context, provider *domain.IMProvider) error {
	return r.db.WithContext(ctx).Save(provider).Error
}

func (r *MySQLIMProviderRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.IMProvider{}, "id = ?", id).Error
}

func (r *MySQLIMProviderRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.IMProvider, error) {
	var providers []*domain.IMProvider
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("created_at ASC").Find(&providers).Error
	return providers, err
}
