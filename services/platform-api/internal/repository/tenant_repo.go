package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type TenantRepository interface {
	Create(ctx context.Context, tenant *domain.Tenant) error
	GetByID(ctx context.Context, id string) (*domain.Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error)
	List(ctx context.Context) ([]*domain.Tenant, error)
	ListWithPagination(ctx context.Context, page, pageSize int) ([]*domain.Tenant, int64, error)
	Update(ctx context.Context, tenant *domain.Tenant) error
	Delete(ctx context.Context, id string) error
}

// MemoryTenantRepository is a simple in-memory implementation for MVP
type MemoryTenantRepository struct {
	tenants map[string]*domain.Tenant
}

func NewMemoryTenantRepository() *MemoryTenantRepository {
	return &MemoryTenantRepository{
		tenants: make(map[string]*domain.Tenant),
	}
}

func (r *MemoryTenantRepository) Create(ctx context.Context, tenant *domain.Tenant) error {
	r.tenants[tenant.ID] = tenant
	return nil
}

func (r *MemoryTenantRepository) GetByID(ctx context.Context, id string) (*domain.Tenant, error) {
	if t, ok := r.tenants[id]; ok {
		return t, nil
	}
	return nil, nil // Should return a proper error like ErrNotFound
}

func (r *MemoryTenantRepository) GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	for _, t := range r.tenants {
		if t.Slug == slug {
			return t, nil
		}
	}
	return nil, nil
}

func (r *MemoryTenantRepository) List(ctx context.Context) ([]*domain.Tenant, error) {
	var list []*domain.Tenant
	for _, t := range r.tenants {
		list = append(list, t)
	}
	return list, nil
}

func (r *MemoryTenantRepository) ListWithPagination(ctx context.Context, page, pageSize int) ([]*domain.Tenant, int64, error) {
	var list []*domain.Tenant
	for _, t := range r.tenants {
		list = append(list, t)
	}

	total := int64(len(list))

	// Calculate offset and limit
	offset := (page - 1) * pageSize
	end := offset + pageSize

	// Apply pagination
	if offset >= len(list) {
		return []*domain.Tenant{}, total, nil
	}
	if end > len(list) {
		end = len(list)
	}

	return list[offset:end], total, nil
}

func (r *MemoryTenantRepository) Update(ctx context.Context, tenant *domain.Tenant) error {
	if _, exists := r.tenants[tenant.ID]; !exists {
		return errors.New("tenant not found")
	}
	r.tenants[tenant.ID] = tenant
	return nil
}

func (r *MemoryTenantRepository) Delete(ctx context.Context, id string) error {
	delete(r.tenants, id)
	return nil
}
