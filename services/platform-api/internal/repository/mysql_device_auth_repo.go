package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

// MySQLDeviceAuthRepository implements domain.DeviceAuthRepository with GORM.
type MySQLDeviceAuthRepository struct {
	db *gorm.DB
}

// NewMySQLDeviceAuthRepository creates a MySQL-backed device auth repository.
func NewMySQLDeviceAuthRepository(db *gorm.DB) *MySQLDeviceAuthRepository {
	return &MySQLDeviceAuthRepository{db: db}
}

func (r *MySQLDeviceAuthRepository) CreateDeviceAuthCode(ctx context.Context, code *domain.DeviceAuthCode) error {
	return r.db.WithContext(ctx).Create(code).Error
}

func (r *MySQLDeviceAuthRepository) GetDeviceAuthCodeByDeviceCode(ctx context.Context, deviceCode string) (*domain.DeviceAuthCode, error) {
	var code domain.DeviceAuthCode
	if err := r.db.WithContext(ctx).Where("device_code = ?", deviceCode).First(&code).Error; err != nil {
		return nil, err
	}
	return &code, nil
}

func (r *MySQLDeviceAuthRepository) GetDeviceAuthCodeByUserCode(ctx context.Context, userCode string) (*domain.DeviceAuthCode, error) {
	var code domain.DeviceAuthCode
	if err := r.db.WithContext(ctx).
		Where("user_code = ?", userCode).
		Order("created_at DESC").
		First(&code).Error; err != nil {
		return nil, err
	}
	return &code, nil
}

func (r *MySQLDeviceAuthRepository) ListDeviceAuthCodes(ctx context.Context, page, pageSize int) ([]*domain.DeviceAuthCode, int64, error) {
	var codes []*domain.DeviceAuthCode
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.DeviceAuthCode{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&codes).Error; err != nil {
		return nil, 0, err
	}
	return codes, total, nil
}

func (r *MySQLDeviceAuthRepository) UpdateDeviceAuthCode(ctx context.Context, code *domain.DeviceAuthCode) error {
	return r.db.WithContext(ctx).Save(code).Error
}

func (r *MySQLDeviceAuthRepository) DeleteDeviceAuthCode(ctx context.Context, deviceCode string) error {
	return r.db.WithContext(ctx).Where("device_code = ?", deviceCode).Delete(&domain.DeviceAuthCode{}).Error
}

func (r *MySQLDeviceAuthRepository) CreateIdeClientToken(ctx context.Context, t *domain.IdeClientToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *MySQLDeviceAuthRepository) GetIdeClientTokenByID(ctx context.Context, id string) (*domain.IdeClientToken, error) {
	var t domain.IdeClientToken
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MySQLDeviceAuthRepository) GetIdeClientTokenByAccessTokenHash(
	ctx context.Context, accessTokenHash string,
) (*domain.IdeClientToken, error) {
	var t domain.IdeClientToken
	if err := r.db.WithContext(ctx).Where("access_token_hash = ?", accessTokenHash).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MySQLDeviceAuthRepository) GetIdeClientTokenByRefreshTokenHash(
	ctx context.Context, refreshTokenHash string,
) (*domain.IdeClientToken, error) {
	var t domain.IdeClientToken
	if err := r.db.WithContext(ctx).Where("refresh_token_hash = ?", refreshTokenHash).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MySQLDeviceAuthRepository) ListIdeClientTokensByUserID(
	ctx context.Context, userID string, page, pageSize int,
) ([]*domain.IdeClientToken, int64, error) {
	var tokens []*domain.IdeClientToken
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.IdeClientToken{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, 0, err
	}
	return tokens, total, nil
}

func (r *MySQLDeviceAuthRepository) ListIdeClientTokensByTenantID(
	ctx context.Context, tenantID string, page, pageSize int,
) ([]*domain.IdeClientToken, int64, error) {
	var tokens []*domain.IdeClientToken
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.IdeClientToken{}).Where("tenant_id = ?", tenantID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, 0, err
	}
	return tokens, total, nil
}

func (r *MySQLDeviceAuthRepository) UpdateIdeClientToken(ctx context.Context, t *domain.IdeClientToken) error {
	return r.db.WithContext(ctx).Save(t).Error
}

func (r *MySQLDeviceAuthRepository) DeleteIdeClientToken(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.IdeClientToken{}).Error
}

// Compile-time check that the repository implements the domain interface.
var _ domain.DeviceAuthRepository = (*MySQLDeviceAuthRepository)(nil)
