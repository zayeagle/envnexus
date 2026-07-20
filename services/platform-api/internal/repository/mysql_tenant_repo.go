package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type MySQLTenantRepository struct {
	db *gorm.DB
}

func NewMySQLTenantRepository(db *gorm.DB) *MySQLTenantRepository {
	return &MySQLTenantRepository{db: db}
}

func (r *MySQLTenantRepository) Create(ctx context.Context, tenant *domain.Tenant) error {
	return r.db.WithContext(ctx).Create(tenant).Error
}

func (r *MySQLTenantRepository) GetByID(ctx context.Context, id string) (*domain.Tenant, error) {
	var tenant domain.Tenant
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&tenant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &tenant, nil
}

func (r *MySQLTenantRepository) GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	var tenant domain.Tenant
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&tenant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &tenant, nil
}

func (r *MySQLTenantRepository) List(ctx context.Context) ([]*domain.Tenant, error) {
	var tenants []*domain.Tenant
	err := r.db.WithContext(ctx).Find(&tenants).Error
	if err != nil {
		return nil, err
	}
	return tenants, nil
}

func (r *MySQLTenantRepository) ListWithPagination(ctx context.Context, page, pageSize int) ([]*domain.Tenant, int64, error) {
	var tenants []*domain.Tenant
	var total int64

	// Calculate offset
	offset := (page - 1) * pageSize

	// Get total count
	err := r.db.WithContext(ctx).Model(&domain.Tenant{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	err = r.db.WithContext(ctx).Offset(offset).Limit(pageSize).Find(&tenants).Error
	if err != nil {
		return nil, 0, err
	}

	return tenants, total, nil
}

func (r *MySQLTenantRepository) Update(ctx context.Context, tenant *domain.Tenant) error {
	return r.db.WithContext(ctx).Save(tenant).Error
}

func (r *MySQLTenantRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.Tenant{}, "id = ?", id).Error
}
