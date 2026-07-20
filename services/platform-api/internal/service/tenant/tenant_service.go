package tenant

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Service struct {
	repo repository.TenantRepository
}

func NewService(repo repository.TenantRepository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error) {
	// Check if slug exists
	existing, err := s.repo.GetBySlug(ctx, req.Slug)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.ErrDuplicateSlug
	}

	id := ulid.Make().String()
	tenant := domain.NewTenant(id, req.Name, req.Slug)

	if err := s.repo.Create(ctx, tenant); err != nil {
		return nil, err
	}

	return &dto.TenantResponse{
		ID:        tenant.ID,
		Name:      tenant.Name,
		Slug:      tenant.Slug,
		Status:    string(tenant.Status),
		CreatedAt: tenant.CreatedAt,
		UpdatedAt: tenant.UpdatedAt,
	}, nil
}

func (s *Service) GetTenant(ctx context.Context, id string) (*dto.TenantResponse, error) {
	tenant, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return nil, domain.ErrTenantNotFound
	}

	return &dto.TenantResponse{
		ID:        tenant.ID,
		Name:      tenant.Name,
		Slug:      tenant.Slug,
		Status:    string(tenant.Status),
		CreatedAt: tenant.CreatedAt,
		UpdatedAt: tenant.UpdatedAt,
	}, nil
}

func (s *Service) ListTenants(ctx context.Context) ([]*dto.TenantResponse, error) {
	tenants, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	var resp []*dto.TenantResponse
	for _, t := range tenants {
		resp = append(resp, &dto.TenantResponse{
			ID:        t.ID,
			Name:      t.Name,
			Slug:      t.Slug,
			Status:    string(t.Status),
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *Service) ListTenantsWithPagination(ctx context.Context, page, pageSize int) ([]*dto.TenantResponse, int64, error) {
	tenants, total, err := s.repo.ListWithPagination(ctx, page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	var resp []*dto.TenantResponse
	for _, t := range tenants {
		resp = append(resp, &dto.TenantResponse{
			ID:        t.ID,
			Name:      t.Name,
			Slug:      t.Slug,
			Status:    string(t.Status),
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
		})
	}
	return resp, total, nil
}

// ListTenantsForActor returns all tenants for platform super admins, or only the caller's home tenant otherwise.
func (s *Service) ListTenantsForActor(ctx context.Context, platformSuperAdmin bool, userTenantID string) ([]*dto.TenantResponse, error) {
	if platformSuperAdmin {
		return s.ListTenants(ctx)
	}
	t, err := s.repo.GetByID(ctx, userTenantID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return []*dto.TenantResponse{}, nil
	}
	return []*dto.TenantResponse{{
		ID:        t.ID,
		Name:      t.Name,
		Slug:      t.Slug,
		Status:    string(t.Status),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}}, nil
}

func (s *Service) UpdateTenant(ctx context.Context, id string, req dto.UpdateTenantRequest) (*dto.TenantResponse, error) {
	tenant, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return nil, domain.ErrTenantNotFound
	}

	if req.Name != "" {
		tenant.Name = req.Name
	}
	if req.Status != "" {
		tenant.Status = domain.TenantStatus(req.Status)
	}

	if err := s.repo.Update(ctx, tenant); err != nil {
		return nil, err
	}

	return &dto.TenantResponse{
		ID:        tenant.ID,
		Name:      tenant.Name,
		Slug:      tenant.Slug,
		Status:    string(tenant.Status),
		CreatedAt: tenant.CreatedAt,
		UpdatedAt: tenant.UpdatedAt,
	}, nil
}

func (s *Service) DeleteTenant(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
