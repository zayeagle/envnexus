package policy_profile

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

type Service struct {
	repo repository.PolicyProfileRepository
}

func NewService(repo repository.PolicyProfileRepository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) ListProfiles(ctx context.Context, tenantID string) ([]*dto.PolicyProfileResponse, error) {
	profiles, err := s.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var resp []*dto.PolicyProfileResponse
	for _, p := range profiles {
		resp = append(resp, &dto.PolicyProfileResponse{
			ID:         p.ID,
			TenantID:   p.TenantID,
			Name:       p.Name,
			PolicyJSON: p.PolicyJSON,
			Status:     p.Status,
			Version:    p.Version,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *Service) CreateProfile(ctx context.Context, tenantID string, req dto.CreatePolicyProfileRequest) (*dto.PolicyProfileResponse, error) {
	id := ulid.Make().String()
	profile := &domain.PolicyProfile{
		ID:         id,
		TenantID:   tenantID,
		Name:       req.Name,
		PolicyJSON: req.PolicyJSON,
		Status:     "active",
		Version:    1,
	}

	if err := s.repo.Create(ctx, profile); err != nil {
		if isDuplicateEntry(err) {
			return nil, domain.ErrDuplicateName
		}
		return nil, err
	}

	return &dto.PolicyProfileResponse{
		ID:         profile.ID,
		TenantID:   profile.TenantID,
		Name:       profile.Name,
		PolicyJSON: profile.PolicyJSON,
		Status:     profile.Status,
		Version:    profile.Version,
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, tenantID, id string, req dto.UpdatePolicyProfileRequest) (*dto.PolicyProfileResponse, error) {
	profile, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, domain.ErrProfileNotFound
	}

	if req.Name != "" {
		profile.Name = req.Name
	}
	if req.PolicyJSON != "" {
		profile.PolicyJSON = req.PolicyJSON
	}
	if req.Status != "" {
		profile.Status = req.Status
	}
	profile.Version++

	if err := s.repo.Update(ctx, profile); err != nil {
		if isDuplicateEntry(err) {
			return nil, domain.ErrDuplicateName
		}
		return nil, err
	}

	return &dto.PolicyProfileResponse{
		ID:         profile.ID,
		TenantID:   profile.TenantID,
		Name:       profile.Name,
		PolicyJSON: profile.PolicyJSON,
		Status:     profile.Status,
		Version:    profile.Version,
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}, nil
}

func (s *Service) DeleteProfile(ctx context.Context, tenantID, id string) error {
	return s.repo.Delete(ctx, id, tenantID)
}
