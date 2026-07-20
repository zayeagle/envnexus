package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

// ── RoleRepository ────────────────────────────────────────────────────────────

type RoleRepository interface {
	Create(ctx context.Context, role *domain.Role) error
	GetByID(ctx context.Context, id string) (*domain.Role, error)
	GetByName(ctx context.Context, tenantID, name string) (*domain.Role, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.Role, error)
	SearchByTenant(ctx context.Context, tenantID, query string, limit int) ([]*domain.Role, error)
	Update(ctx context.Context, role *domain.Role) error
	Delete(ctx context.Context, id string) error
}

type MySQLRoleRepository struct {
	db *gorm.DB
}

func NewMySQLRoleRepository(db *gorm.DB) *MySQLRoleRepository {
	return &MySQLRoleRepository{db: db}
}

func (r *MySQLRoleRepository) Create(ctx context.Context, role *domain.Role) error {
	return r.db.WithContext(ctx).Create(role).Error
}

func (r *MySQLRoleRepository) GetByID(ctx context.Context, id string) (*domain.Role, error) {
	var role domain.Role
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&role).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &role, nil
}

func (r *MySQLRoleRepository) GetByName(ctx context.Context, tenantID, name string) (*domain.Role, error) {
	var role domain.Role
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND name = ?", tenantID, name).First(&role).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &role, nil
}

func (r *MySQLRoleRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.Role, error) {
	var roles []*domain.Role
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("name").Find(&roles).Error
	return roles, err
}

func (r *MySQLRoleRepository) SearchByTenant(ctx context.Context, tenantID, query string, limit int) ([]*domain.Role, error) {
	if limit <= 0 {
		limit = 20
	}
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if strings.TrimSpace(query) != "" {
		like := "%" + strings.TrimSpace(query) + "%"
		q = q.Where("name LIKE ?", like)
	}
	var roles []*domain.Role
	err := q.Order("name").Limit(limit).Find(&roles).Error
	return roles, err
}

func (r *MySQLRoleRepository) Update(ctx context.Context, role *domain.Role) error {
	return r.db.WithContext(ctx).Save(role).Error
}

func (r *MySQLRoleRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.Role{}, "id = ?", id).Error
}

// ── RoleBindingRepository ─────────────────────────────────────────────────────

type RoleBindingRepository interface {
	Create(ctx context.Context, rb *domain.RoleBinding) error
	GetByUserID(ctx context.Context, userID string) ([]*domain.RoleBinding, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.RoleBinding, error)
	Delete(ctx context.Context, id string) error
	DeleteByUserAndRole(ctx context.Context, userID, roleID string) error
}

type MySQLRoleBindingRepository struct {
	db *gorm.DB
}

func NewMySQLRoleBindingRepository(db *gorm.DB) *MySQLRoleBindingRepository {
	return &MySQLRoleBindingRepository{db: db}
}

func (r *MySQLRoleBindingRepository) Create(ctx context.Context, rb *domain.RoleBinding) error {
	return r.db.WithContext(ctx).Create(rb).Error
}

func (r *MySQLRoleBindingRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.RoleBinding, error) {
	var bindings []*domain.RoleBinding
	err := r.db.WithContext(ctx).
		Preload("Role").
		Where("user_id = ?", userID).
		Find(&bindings).Error
	return bindings, err
}

func (r *MySQLRoleBindingRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.RoleBinding, error) {
	var bindings []*domain.RoleBinding
	err := r.db.WithContext(ctx).
		Preload("Role").
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&bindings).Error
	return bindings, err
}

func (r *MySQLRoleBindingRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.RoleBinding{}, "id = ?", id).Error
}

func (r *MySQLRoleBindingRepository) DeleteByUserAndRole(ctx context.Context, userID, roleID string) error {
	return r.db.WithContext(ctx).Delete(&domain.RoleBinding{}, "user_id = ? AND role_id = ?", userID, roleID).Error
}
