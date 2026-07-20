package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type UserRepository interface {
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id string) (*domain.User, error)
	Create(ctx context.Context, user *domain.User) error
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.User, error)
	SearchByTenant(ctx context.Context, tenantID, query string, limit int) ([]*domain.User, error)
}

type MySQLUserRepository struct {
	db *gorm.DB
}

func NewMySQLUserRepository(db *gorm.DB) *MySQLUserRepository {
	return &MySQLUserRepository{db: db}
}

func (r *MySQLUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Where("email = ? AND deleted_at IS NULL", email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *MySQLUserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *MySQLUserRepository) Create(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *MySQLUserRepository) Update(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *MySQLUserRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", gorm.Expr("NOW(3)")).Error
}

func (r *MySQLUserRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.User, error) {
	var users []*domain.User
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID).Find(&users).Error
	return users, err
}

func (r *MySQLUserRepository) SearchByTenant(ctx context.Context, tenantID, query string, limit int) ([]*domain.User, error) {
	if limit <= 0 {
		limit = 20
	}
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if strings.TrimSpace(query) != "" {
		like := "%" + strings.TrimSpace(query) + "%"
		q = q.Where("email LIKE ? OR display_name LIKE ?", like, like)
	}
	var users []*domain.User
	err := q.Order("created_at DESC").Limit(limit).Find(&users).Error
	return users, err
}
