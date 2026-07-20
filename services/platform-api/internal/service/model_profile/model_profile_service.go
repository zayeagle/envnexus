package model_profile

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
	repo repository.ModelProfileRepository
}

func NewService(repo repository.ModelProfileRepository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) ListProfiles(ctx context.Context, tenantID string) ([]*dto.ModelProfileResponse, error) {
	profiles, err := s.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var resp []*dto.ModelProfileResponse
	for _, p := range profiles {
		resp = append(resp, &dto.ModelProfileResponse{
			ID:         p.ID,
			TenantID:   p.TenantID,
			Name:       p.Name,
			Provider:   p.Provider,
			BaseURL:    p.BaseURL,
			ModelName:  p.ModelName,
			APIKey:     p.APIKey,
			ParamsJSON: p.ParamsJSON,
			SecretMode: p.SecretMode,
			Status:     p.Status,
			Version:    p.Version,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *Service) CreateProfile(ctx context.Context, tenantID string, req dto.CreateModelProfileRequest) (*dto.ModelProfileResponse, error) {
	id := ulid.Make().String()
	profile := &domain.ModelProfile{
		ID:         id,
		TenantID:   tenantID,
		Name:       req.Name,
		Provider:   req.Provider,
		BaseURL:    req.BaseURL,
		ModelName:  req.ModelName,
		APIKey:     req.APIKey,
		ParamsJSON: req.ParamsJSON,
		SecretMode: req.SecretMode,
		Status:     "active",
		Version:    1,
	}

	if err := s.repo.Create(ctx, profile); err != nil {
		if isDuplicateEntry(err) {
			return nil, domain.ErrDuplicateName
		}
		return nil, err
	}

	return &dto.ModelProfileResponse{
		ID:         profile.ID,
		TenantID:   profile.TenantID,
		Name:       profile.Name,
		Provider:   profile.Provider,
		BaseURL:    profile.BaseURL,
		ModelName:  profile.ModelName,
		APIKey:     profile.APIKey,
		ParamsJSON: profile.ParamsJSON,
		SecretMode: profile.SecretMode,
		Status:     profile.Status,
		Version:    profile.Version,
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, tenantID, id string, req dto.UpdateModelProfileRequest) (*dto.ModelProfileResponse, error) {
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
	if req.Provider != "" {
		profile.Provider = req.Provider
	}
	if req.BaseURL != "" {
		profile.BaseURL = req.BaseURL
	}
	if req.ModelName != "" {
		profile.ModelName = req.ModelName
	}
	if req.APIKey != "" {
		profile.APIKey = req.APIKey
	}
	if req.ParamsJSON != "" {
		profile.ParamsJSON = req.ParamsJSON
	}
	if req.SecretMode != "" {
		profile.SecretMode = req.SecretMode
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

	return &dto.ModelProfileResponse{
		ID:         profile.ID,
		TenantID:   profile.TenantID,
		Name:       profile.Name,
		Provider:   profile.Provider,
		BaseURL:    profile.BaseURL,
		ModelName:  profile.ModelName,
		APIKey:     profile.APIKey,
		ParamsJSON: profile.ParamsJSON,
		SecretMode: profile.SecretMode,
		Status:     profile.Status,
		Version:    profile.Version,
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}, nil
}

func (s *Service) DeleteProfile(ctx context.Context, tenantID, id string) error {
	return s.repo.Delete(ctx, id, tenantID)
}
