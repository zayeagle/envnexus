package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type GovernanceRuleRepository interface {
	Create(ctx context.Context, rule *domain.GovernanceRule) error
	GetByID(ctx context.Context, id string) (*domain.GovernanceRule, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.GovernanceRule, error)
	Update(ctx context.Context, rule *domain.GovernanceRule) error
	Delete(ctx context.Context, id string) error
}

type MySQLGovernanceRuleRepository struct {
	db *gorm.DB
}

func NewMySQLGovernanceRuleRepository(db *gorm.DB) *MySQLGovernanceRuleRepository {
	return &MySQLGovernanceRuleRepository{db: db}
}

func (r *MySQLGovernanceRuleRepository) Create(ctx context.Context, rule *domain.GovernanceRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *MySQLGovernanceRuleRepository) GetByID(ctx context.Context, id string) (*domain.GovernanceRule, error) {
	var rule domain.GovernanceRule
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *MySQLGovernanceRuleRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.GovernanceRule, error) {
	var rules []*domain.GovernanceRule
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *MySQLGovernanceRuleRepository) Update(ctx context.Context, rule *domain.GovernanceRule) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

func (r *MySQLGovernanceRuleRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.GovernanceRule{}).Error
}

type ToolPermissionRepository interface {
	Create(ctx context.Context, tp *domain.ToolPermission) error
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.ToolPermission, error)
	Delete(ctx context.Context, id string) error
	GetByToolAndRole(ctx context.Context, tenantID, toolName string, roleID *string) (*domain.ToolPermission, error)
	Update(ctx context.Context, tp *domain.ToolPermission) error
	GetByID(ctx context.Context, id string) (*domain.ToolPermission, error)
}

type MySQLToolPermissionRepository struct {
	db *gorm.DB
}

func NewMySQLToolPermissionRepository(db *gorm.DB) *MySQLToolPermissionRepository {
	return &MySQLToolPermissionRepository{db: db}
}

func (r *MySQLToolPermissionRepository) Create(ctx context.Context, tp *domain.ToolPermission) error {
	return r.db.WithContext(ctx).Create(tp).Error
}

func (r *MySQLToolPermissionRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.ToolPermission, error) {
	var perms []*domain.ToolPermission
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&perms).Error; err != nil {
		return nil, err
	}
	return perms, nil
}

func (r *MySQLToolPermissionRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.ToolPermission{}).Error
}

func (r *MySQLToolPermissionRepository) GetByToolAndRole(ctx context.Context, tenantID, toolName string, roleID *string) (*domain.ToolPermission, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND tool_name = ?", tenantID, toolName)
	if roleID != nil {
		q = q.Where("role_id = ?", *roleID)
	} else {
		q = q.Where("role_id IS NULL")
	}
	var tp domain.ToolPermission
	if err := q.First(&tp).Error; err != nil {
		return nil, err
	}
	return &tp, nil
}

func (r *MySQLToolPermissionRepository) Update(ctx context.Context, tp *domain.ToolPermission) error {
	return r.db.WithContext(ctx).Save(tp).Error
}

func (r *MySQLToolPermissionRepository) GetByID(ctx context.Context, id string) (*domain.ToolPermission, error) {
	var tp domain.ToolPermission
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tp).Error; err != nil {
		return nil, err
	}
	return &tp, nil
}
