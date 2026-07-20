package api_token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
)

const tokenRawBytes = 32 // 256-bit random token
const tokenPrefixTag = "enx_"

type ApiTokenPrincipal struct {
	TokenID      string
	UserID       string
	TenantID     string
	Scopes       []string
	IsSuperAdmin bool
}

type Service struct {
	repo domain.ApiTokenRepository
}

func NewService(repo domain.ApiTokenRepository) *Service {
	return &Service{repo: repo}
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func generateToken() (plain, prefix string, err error) {
	b := make([]byte, tokenRawBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	plain = tokenPrefixTag + raw
	prefix = plain[:len(tokenPrefixTag)+8]
	return plain, prefix, nil
}

// Create generates a new API token. The plaintext is returned only in this response.
func (s *Service) Create(ctx context.Context, userID, tenantID string, isSuperAdmin bool, req dto.CreateApiTokenRequest) (*dto.CreateApiTokenResponse, error) {
	plain, prefix, err := generateToken()
	if err != nil {
		return nil, domain.ErrInternalError
	}

	scopes := normalizeScopes(req.Scopes)

	now := time.Now()
	tok := &domain.ApiToken{
		ID:           ulid.Make().String(),
		UserID:       userID,
		TenantID:     tenantID,
		Name:         req.Name,
		TokenHash:    hashToken(plain),
		TokenPrefix:  prefix,
		Scopes:       domain.StringList(scopes),
		IsSuperAdmin: isSuperAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		exp := now.Add(time.Duration(*req.ExpiresIn) * time.Second)
		tok.ExpiresAt = &exp
	}

	if err := s.repo.CreateApiToken(ctx, tok); err != nil {
		return nil, domain.ErrInternalError
	}
	return &dto.CreateApiTokenResponse{
		ID:           tok.ID,
		Name:         tok.Name,
		Token:        plain,
		TokenPrefix:  tok.TokenPrefix,
		Scopes:       scopes,
		IsSuperAdmin: tok.IsSuperAdmin,
		ExpiresAt:    tok.ExpiresAt,
		CreatedAt:    tok.CreatedAt,
	}, nil
}

// normalizeScopes ensures scopes contain exactly one of "read-only" or "read-write".
func normalizeScopes(scopes []string) []string {
	for _, s := range scopes {
		if s == "read-write" {
			return []string{"read-write"}
		}
	}
	return []string{"read-only"}
}

// Validate checks a raw bearer token against stored hashes, expiry, and revocation.
func (s *Service) Validate(ctx context.Context, rawToken string) (*ApiTokenPrincipal, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, domain.ErrApiTokenNotFound
	}

	h := hashToken(rawToken)
	tok, err := s.repo.GetApiTokenByHash(ctx, h)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrApiTokenNotFound
		}
		return nil, err
	}

	if tok.RevokedAt != nil {
		return nil, domain.ErrApiTokenRevoked
	}

	now := time.Now()
	if tok.ExpiresAt != nil && now.After(*tok.ExpiresAt) {
		return nil, domain.ErrApiTokenExpired
	}

	tok.LastUsedAt = &now
	tok.UpdatedAt = now
	_ = s.repo.UpdateApiToken(ctx, tok)

	return &ApiTokenPrincipal{
		TokenID:      tok.ID,
		UserID:       tok.UserID,
		TenantID:     tok.TenantID,
		Scopes:       []string(tok.Scopes),
		IsSuperAdmin: tok.IsSuperAdmin,
	}, nil
}

// List returns paginated API tokens for a tenant (hashes are never exposed).
func (s *Service) List(ctx context.Context, tenantID string, page, pageSize int) ([]*dto.ApiTokenListItem, int64, error) {
	rows, total, err := s.repo.ListApiTokensByTenantID(ctx, tenantID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*dto.ApiTokenListItem, 0, len(rows))
	for _, t := range rows {
		out = append(out, &dto.ApiTokenListItem{
			ID:           t.ID,
			Name:         t.Name,
			TokenPrefix:  t.TokenPrefix,
			Scopes:       []string(t.Scopes),
			IsSuperAdmin: t.IsSuperAdmin,
			ExpiresAt:    t.ExpiresAt,
			LastUsedAt:   t.LastUsedAt,
			RevokedAt:    t.RevokedAt,
			CreatedAt:    t.CreatedAt,
		})
	}
	return out, total, nil
}

// Revoke soft-deletes a token (sets revoked_at) if it belongs to the given tenant.
func (s *Service) Revoke(ctx context.Context, tenantID, tokenID string) error {
	tok, err := s.repo.GetApiTokenByID(ctx, tokenID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	if tok.TenantID != tenantID {
		return domain.ErrForbidden
	}
	now := time.Now()
	tok.RevokedAt = &now
	tok.UpdatedAt = now
	return s.repo.UpdateApiToken(ctx, tok)
}
