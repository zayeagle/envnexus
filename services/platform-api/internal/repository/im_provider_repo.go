package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type IMProviderRepository interface {
	Create(ctx context.Context, provider *domain.IMProvider) error
	GetByID(ctx context.Context, id string) (*domain.IMProvider, error)
	Update(ctx context.Context, provider *domain.IMProvider) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.IMProvider, error)
}
