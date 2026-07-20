package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type MySQLApiTokenRepository struct {
	db *gorm.DB
}

func NewMySQLApiTokenRepository(db *gorm.DB) *MySQLApiTokenRepository {
	return &MySQLApiTokenRepository{db: db}
}

func (r *MySQLApiTokenRepository) CreateApiToken(ctx context.Context, t *domain.ApiToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *MySQLApiTokenRepository) GetApiTokenByID(ctx context.Context, id string) (*domain.ApiToken, error) {
	var t domain.ApiToken
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MySQLApiTokenRepository) GetApiTokenByHash(ctx context.Context, tokenHash string) (*domain.ApiToken, error) {
	var t domain.ApiToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MySQLApiTokenRepository) ListApiTokensByTenantID(ctx context.Context, tenantID string, page, pageSize int) ([]*domain.ApiToken, int64, error) {
	var tokens []*domain.ApiToken
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.ApiToken{}).Where("tenant_id = ?", tenantID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		q = q.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, 0, err
	}
	return tokens, total, nil
}

func (r *MySQLApiTokenRepository) ListApiTokensByUserID(ctx context.Context, userID string, page, pageSize int) ([]*domain.ApiToken, int64, error) {
	var tokens []*domain.ApiToken
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.ApiToken{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		q = q.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, 0, err
	}
	return tokens, total, nil
}

func (r *MySQLApiTokenRepository) UpdateApiToken(ctx context.Context, t *domain.ApiToken) error {
	return r.db.WithContext(ctx).Save(t).Error
}

func (r *MySQLApiTokenRepository) DeleteApiToken(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.ApiToken{}).Error
}

var _ domain.ApiTokenRepository = (*MySQLApiTokenRepository)(nil)
