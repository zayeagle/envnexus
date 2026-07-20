package agent_profile

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
	repo repository.AgentProfileRepository
}

func NewService(repo repository.AgentProfileRepository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) ListProfiles(ctx context.Context, tenantID string) ([]*dto.AgentProfileResponse, error) {
	profiles, err := s.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var resp []*dto.AgentProfileResponse
	for _, p := range profiles {
		resp = append(resp, &dto.AgentProfileResponse{
			ID:               p.ID,
			TenantID:         p.TenantID,
			Name:             p.Name,
			ModelProfileID:   p.ModelProfileID,
			PolicyProfileID:  p.PolicyProfileID,
			CapabilitiesJSON: p.CapabilitiesJSON,
			UpdateChannel:    p.UpdateChannel,
			Status:           p.Status,
			Version:          p.Version,
			CreatedAt:        p.CreatedAt,
			UpdatedAt:        p.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *Service) CreateProfile(ctx context.Context, tenantID string, req dto.CreateAgentProfileRequest) (*dto.AgentProfileResponse, error) {
	id := ulid.Make().String()
	profile := &domain.AgentProfile{
		ID:               id,
		TenantID:         tenantID,
		Name:             req.Name,
		ModelProfileID:   req.ModelProfileID,
		PolicyProfileID:  req.PolicyProfileID,
		CapabilitiesJSON: req.CapabilitiesJSON,
		UpdateChannel:    req.UpdateChannel,
		Status:           "active",
		Version:          1,
	}

	if err := s.repo.Create(ctx, profile); err != nil {
		if isDuplicateEntry(err) {
			return nil, domain.ErrDuplicateName
		}
		return nil, err
	}

	return &dto.AgentProfileResponse{
		ID:               profile.ID,
		TenantID:         profile.TenantID,
		Name:             profile.Name,
		ModelProfileID:   profile.ModelProfileID,
		PolicyProfileID:  profile.PolicyProfileID,
		CapabilitiesJSON: profile.CapabilitiesJSON,
		UpdateChannel:    profile.UpdateChannel,
		Status:           profile.Status,
		Version:          profile.Version,
		CreatedAt:        profile.CreatedAt,
		UpdatedAt:        profile.UpdatedAt,
	}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, tenantID, id string, req dto.UpdateAgentProfileRequest) (*dto.AgentProfileResponse, error) {
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
	if req.ModelProfileID != "" {
		profile.ModelProfileID = req.ModelProfileID
	}
	if req.PolicyProfileID != "" {
		profile.PolicyProfileID = req.PolicyProfileID
	}
	if req.CapabilitiesJSON != "" {
		profile.CapabilitiesJSON = req.CapabilitiesJSON
	}
	if req.UpdateChannel != "" {
		profile.UpdateChannel = req.UpdateChannel
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

	return &dto.AgentProfileResponse{
		ID:               profile.ID,
		TenantID:         profile.TenantID,
		Name:             profile.Name,
		ModelProfileID:   profile.ModelProfileID,
		PolicyProfileID:  profile.PolicyProfileID,
		CapabilitiesJSON: profile.CapabilitiesJSON,
		UpdateChannel:    profile.UpdateChannel,
		Status:           profile.Status,
		Version:          profile.Version,
		CreatedAt:        profile.CreatedAt,
		UpdatedAt:        profile.UpdatedAt,
	}, nil
}

func (s *Service) DeleteProfile(ctx context.Context, tenantID, id string) error {
	return s.repo.Delete(ctx, id, tenantID)
}
