package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type ApprovalPolicyRepository interface {
	Create(ctx context.Context, policy *domain.ApprovalPolicy) error
	GetByID(ctx context.Context, id string) (*domain.ApprovalPolicy, error)
	Update(ctx context.Context, policy *domain.ApprovalPolicy) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.ApprovalPolicy, error)
	FindByTenantAndRisk(ctx context.Context, tenantID, riskLevel string) (*domain.ApprovalPolicy, error)
}
