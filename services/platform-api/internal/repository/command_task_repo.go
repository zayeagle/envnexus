package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type CommandTaskFilters struct {
	Status     string
	CreatedBy  string
	ApproverID string
	RiskLevel  string
	IncludeArchived bool
}

type CommandTaskRepository interface {
	Create(ctx context.Context, task *domain.CommandTask) error
	GetByID(ctx context.Context, id string) (*domain.CommandTask, error)
	Update(ctx context.Context, task *domain.CommandTask) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenantID string, filters CommandTaskFilters, limit, offset int) ([]*domain.CommandTask, int64, error)
	ListPendingByApprover(ctx context.Context, tenantID, approverUserID string) ([]*domain.CommandTask, error)
	ListPendingByApproverRole(ctx context.Context, tenantID, roleID string) ([]*domain.CommandTask, error)
	ListPendingInTenant(ctx context.Context, tenantID string) ([]*domain.CommandTask, error)
	CountPendingInTenant(ctx context.Context, tenantID string) (int64, error)
	CountPendingByApprover(ctx context.Context, tenantID, approverUserID string) (int64, error)
	ListExpired(ctx context.Context) ([]*domain.CommandTask, error)
}
