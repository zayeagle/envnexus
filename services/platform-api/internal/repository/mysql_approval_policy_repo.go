package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type MySQLApprovalPolicyRepository struct {
	db *gorm.DB
}

func NewMySQLApprovalPolicyRepository(db *gorm.DB) *MySQLApprovalPolicyRepository {
	return &MySQLApprovalPolicyRepository{db: db}
}

func (r *MySQLApprovalPolicyRepository) Create(ctx context.Context, policy *domain.ApprovalPolicy) error {
	return r.db.WithContext(ctx).Create(policy).Error
}

func (r *MySQLApprovalPolicyRepository) GetByID(ctx context.Context, id string) (*domain.ApprovalPolicy, error) {
	var policy domain.ApprovalPolicy
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&policy).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &policy, nil
}

func (r *MySQLApprovalPolicyRepository) Update(ctx context.Context, policy *domain.ApprovalPolicy) error {
	return r.db.WithContext(ctx).Save(policy).Error
}

func (r *MySQLApprovalPolicyRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.ApprovalPolicy{}, "id = ?", id).Error
}

func (r *MySQLApprovalPolicyRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.ApprovalPolicy, error) {
	var policies []*domain.ApprovalPolicy
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, "active").
		Order("priority DESC, created_at ASC").
		Find(&policies).Error
	return policies, err
}

func (r *MySQLApprovalPolicyRepository) FindByTenantAndRisk(ctx context.Context, tenantID, riskLevel string) (*domain.ApprovalPolicy, error) {
	var policy domain.ApprovalPolicy
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND (risk_level = ? OR risk_level = ?)", tenantID, "active", riskLevel, "*").
		Order(gorm.Expr("CASE WHEN risk_level = ? THEN 0 ELSE 1 END, priority DESC", riskLevel)).
		First(&policy).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &policy, nil
}
