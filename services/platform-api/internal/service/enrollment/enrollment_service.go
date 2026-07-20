package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/auth"
)

type Service struct {
	enrollRepo  repository.EnrollmentRepository
	deviceRepo  repository.DeviceRepository
	authService *auth.Service
}

func NewService(enrollRepo repository.EnrollmentRepository, deviceRepo repository.DeviceRepository, authService *auth.Service) *Service {
	return &Service{
		enrollRepo:  enrollRepo,
		deviceRepo:  deviceRepo,
		authService: authService,
	}
}

func (s *Service) CreateToken(ctx context.Context, tenantID string, req dto.CreateTokenRequest) (*dto.TokenResponse, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, domain.ErrInternalError
	}
	tokenStr := "enx_tok_" + hex.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(tokenStr))
	tokenHash := hex.EncodeToString(hash[:])

	now := time.Now()
	token := &domain.EnrollmentToken{
		ID:             ulid.Make().String(),
		TenantID:       tenantID,
		AgentProfileID: req.AgentProfileID,
		TokenHash:      tokenHash,
		Channel:        "stable",
		MaxUses:        req.MaxUses,
		UsedCount:      0,
		ExpiresAt:      now.Add(time.Duration(req.ExpiresIn) * time.Hour),
		Status:         "active",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.enrollRepo.Create(ctx, token); err != nil {
		return nil, domain.ErrInternalError
	}

	return &dto.TokenResponse{
		ID:        token.ID,
		TenantID:  token.TenantID,
		Token:     tokenStr,
		MaxUses:   token.MaxUses,
		UsedCount: token.UsedCount,
		ExpiresAt: token.ExpiresAt,
		CreatedAt: token.CreatedAt,
	}, nil
}

func (s *Service) ListTokens(ctx context.Context, tenantID string) ([]*dto.TokenResponse, error) {
	tokens, err := s.enrollRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	var result []*dto.TokenResponse
	for _, t := range tokens {
		result = append(result, &dto.TokenResponse{
			ID:        t.ID,
			TenantID:  t.TenantID,
			Token:     "(hidden)",
			MaxUses:   t.MaxUses,
			UsedCount: t.UsedCount,
			ExpiresAt: t.ExpiresAt,
			CreatedAt: t.CreatedAt,
		})
	}
	return result, nil
}

func (s *Service) EnrollAgent(ctx context.Context, req dto.AgentEnrollRequest) (*dto.AgentEnrollResponse, error) {
	hash := sha256.Sum256([]byte(req.EnrollmentToken))
	tokenHash := hex.EncodeToString(hash[:])

	token, err := s.enrollRepo.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if token == nil {
		return nil, domain.ErrInvalidEnrollToken
	}
	if !token.IsValid() {
		return nil, domain.ErrInvalidEnrollToken
	}

	now := time.Now()
	hostname := req.Device.Hostname
	deviceID := ulid.Make().String()
	newDevice := &domain.Device{
		ID:              deviceID,
		TenantID:        token.TenantID,
		AgentProfileID:  token.AgentProfileID,
		DeviceName:      req.Device.DeviceName,
		Hostname:        &hostname,
		Platform:        req.Device.Platform,
		Arch:            req.Device.Arch,
		EnvironmentType: req.Device.EnvironmentType,
		AgentVersion:    req.Agent.Version,
		Status:          domain.DeviceStatusActive,
		PolicyVersion:   1,
		LastSeenAt:      &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if newDevice.Arch == "" {
		newDevice.Arch = "amd64"
	}
	if newDevice.EnvironmentType == "" {
		newDevice.EnvironmentType = "physical"
	}

	if err := s.deviceRepo.Create(ctx, newDevice); err != nil {
		return nil, domain.ErrInternalError
	}

	token.IncrementUsage()
	_ = s.enrollRepo.Update(ctx, token)

	deviceToken, err := s.authService.IssueDeviceToken(deviceID, token.TenantID)
	if err != nil {
		return nil, domain.ErrInternalError
	}

	return &dto.AgentEnrollResponse{
		DeviceID:      deviceID,
		TenantID:      token.TenantID,
		DeviceToken:   deviceToken,
		ConfigVersion: 1,
	}, nil
}
