package user

import (
	"context"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/auth"
)

type Service struct {
	userRepo repository.UserRepository
}

func NewService(userRepo repository.UserRepository) *Service {
	return &Service{userRepo: userRepo}
}

func (s *Service) List(ctx context.Context, tenantID, q string, limit int) ([]dto.UserResponse, error) {
	var users []*domain.User
	var err error
	if strings.TrimSpace(q) != "" || limit > 0 {
		users, err = s.userRepo.SearchByTenant(ctx, tenantID, q, limit)
	} else {
		users, err = s.userRepo.ListByTenant(ctx, tenantID)
	}
	if err != nil {
		return nil, err
	}
	out := make([]dto.UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toResponse(u))
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, tenantID string, req dto.CreateUserRequest) (*dto.UserResponse, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if existing, err := s.userRepo.GetByEmail(ctx, email); err == nil && existing != nil && existing.TenantID == tenantID {
		return nil, domain.ErrDuplicateName
	}

	passwordHash := ""
	if strings.TrimSpace(req.Password) != "" {
		h, err := auth.HashPassword(req.Password)
		if err != nil {
			return nil, domain.ErrInternalError
		}
		passwordHash = h
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}

	u := &domain.User{
		ID:           ulid.Make().String(),
		TenantID:     tenantID,
		Email:        email,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		PasswordHash: passwordHash,
		Status:       status,
	}
	if err := s.userRepo.Create(ctx, u); err != nil {
		return nil, err
	}
	resp := toResponse(u)
	return &resp, nil
}

func (s *Service) Update(ctx context.Context, tenantID, userID string, req dto.UpdateUserRequest) (*dto.UserResponse, error) {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}

	if req.Email != nil {
		email := strings.TrimSpace(strings.ToLower(*req.Email))
		if email != "" && email != u.Email {
			if existing, err := s.userRepo.GetByEmail(ctx, email); err == nil && existing != nil && existing.ID != u.ID && existing.TenantID == tenantID {
				return nil, domain.ErrDuplicateName
			}
			u.Email = email
		}
	}
	if req.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Status != nil {
		st := strings.TrimSpace(*req.Status)
		if st != "" {
			u.Status = st
		}
	}
	if req.Password != nil {
		if strings.TrimSpace(*req.Password) == "" {
			u.PasswordHash = ""
		} else {
			h, err := auth.HashPassword(*req.Password)
			if err != nil {
				return nil, domain.ErrInternalError
			}
			u.PasswordHash = h
		}
	}

	if err := s.userRepo.Update(ctx, u); err != nil {
		return nil, err
	}
	resp := toResponse(u)
	return &resp, nil
}

func (s *Service) Delete(ctx context.Context, tenantID, userID string) error {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil || u.TenantID != tenantID {
		return domain.ErrNotFound
	}
	return s.userRepo.Delete(ctx, userID)
}

func toResponse(u *domain.User) dto.UserResponse {
	return dto.UserResponse{
		ID:          u.ID,
		TenantID:    u.TenantID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Status:      u.Status,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

