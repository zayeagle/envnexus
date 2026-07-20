package device_auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
)

// RFC 8628 style timings.
const (
	defaultUserCodeLen         = 8
	deviceCodeMinIntervalSec   = 5
	defaultDeviceCodeExpires   = 15 * time.Minute
	accessTokenLifetime        = 1 * time.Hour
	refreshTokenLifetime       = 30 * 24 * time.Hour
	maxUserCodeGenerationTries = 5
	ideClientTokenName         = "IDE Client"
)

// user code charset: visually unambiguous alphanumerics.
const userCodeChars = "BCDFGHJKLMNPQRSTVWXZ23456789"

// IdeAccessPrincipal is the identity resolved from a valid IDE access token.
type IdeAccessPrincipal struct {
	TokenID  string
	UserID   string
	TenantID string
}

type Service struct {
	repo domain.DeviceAuthRepository
}

type deviceInfoPayload struct {
	Name     string `json:"name"`
	OS       string `json:"os"`
	Hostname string `json:"hostname"`
	Client   string `json:"client"`
}

func NewService(repo domain.DeviceAuthRepository) *Service {
	return &Service{repo: repo}
}

func tokenHash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func randomURLToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomUserCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	ch := make([]byte, 0, 9)
	for i := 0; i < 4; i++ {
		ch = append(ch, userCodeChars[int(buf[i])%len(userCodeChars)])
	}
	ch = append(ch, '-')
	for i := 0; i < 4; i++ {
		ch = append(ch, userCodeChars[int(buf[4+i])%len(userCodeChars)])
	}
	return string(ch), nil
}

// NormalizeUserCode uppercases, trims, and formats XXXXXXXX as XXXX-XXXX if the hyphen is omitted.
func NormalizeUserCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	if len(s) == defaultUserCodeLen {
		return s[:4] + "-" + s[4:]
	}
	return s
}

func buildIdeClientTokenName(rawDeviceInfo string) string {
	base := ideClientTokenName
	info := strings.TrimSpace(rawDeviceInfo)
	if info == "" {
		return base
	}
	var p deviceInfoPayload
	if err := json.Unmarshal([]byte(info), &p); err != nil {
		return base
	}

	clientName := strings.TrimSpace(p.Name)
	if clientName == "" {
		clientName = strings.TrimSpace(p.Client)
	}
	if clientName == "" {
		clientName = base
	}

	platform := strings.TrimSpace(p.OS)
	if platform == "" {
		platform = strings.TrimSpace(p.Hostname)
	}
	if platform == "" {
		return clientName
	}
	return fmt.Sprintf("%s (%s)", clientName, platform)
}

// InitDeviceAuthorization starts a device authorization request and returns the codes the device and user must use.
func (s *Service) InitDeviceAuthorization(ctx context.Context, deviceInfo string) (*dto.DeviceAuthInitResponse, error) {
	if deviceInfo == "" {
		deviceInfo = "{}"
	}
	now := time.Now()
	exp := now.Add(defaultDeviceCodeExpires)

	var userCode, deviceCode string
	for t := 0; t < maxUserCodeGenerationTries; t++ {
		uc, err := randomUserCode()
		if err != nil {
			return nil, err
		}
		_, err = s.repo.GetDeviceAuthCodeByUserCode(ctx, uc)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				userCode = uc
				break
			}
			return nil, err
		}
	}
	if userCode == "" {
		return nil, domain.ErrInternalError
	}

	dc, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	deviceCode = dc

	sess := &domain.DeviceAuthCode{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		Status:     domain.DeviceAuthCodeStatusPending,
		ExpiresAt:  exp,
		DeviceInfo: deviceInfo,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.CreateDeviceAuthCode(ctx, sess); err != nil {
		return nil, err
	}
	return &dto.DeviceAuthInitResponse{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ExpiresIn:  int(time.Until(exp).Seconds()),
		Interval:   deviceCodeMinIntervalSec,
	}, nil
}

// PollForTokens checks a pending device request; on success returns tokens once, then the session is removed.
func (s *Service) PollForTokens(ctx context.Context, deviceCode string) (*dto.DeviceAuthPollResponse, error) {
	code, err := s.repo.GetDeviceAuthCodeByDeviceCode(ctx, deviceCode)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrDeviceAuthNotFound
		}
		return nil, err
	}
	now := time.Now()
	if now.After(code.ExpiresAt) {
		_ = s.repo.DeleteDeviceAuthCode(ctx, deviceCode)
		return &dto.DeviceAuthPollResponse{
			Error:            "expired_token",
			ErrorDescription: "the device code has expired",
		}, nil
	}
	switch code.Status {
	case domain.DeviceAuthCodeStatusPending:
		return &dto.DeviceAuthPollResponse{
			Error:            "authorization_pending",
			ErrorDescription: "the user has not yet completed authorization",
		}, nil
	case domain.DeviceAuthCodeStatusDenied:
		_ = s.repo.DeleteDeviceAuthCode(ctx, deviceCode)
		return &dto.DeviceAuthPollResponse{
			Error:            "access_denied",
			ErrorDescription: "the user denied the authorization request",
		}, nil
	case domain.DeviceAuthCodeStatusExpired:
		_ = s.repo.DeleteDeviceAuthCode(ctx, deviceCode)
		return &dto.DeviceAuthPollResponse{
			Error:            "expired_token",
			ErrorDescription: "the device code has expired",
		}, nil
	case domain.DeviceAuthCodeStatusAuthorized:
		if code.UserID == nil || code.TenantID == nil {
			_ = s.repo.DeleteDeviceAuthCode(ctx, deviceCode)
			return nil, domain.ErrDeviceAuthInvalidState
		}
		accessRaw, err := randomURLToken(32)
		if err != nil {
			return nil, err
		}
		refreshRaw, err := randomURLToken(32)
		if err != nil {
			return nil, err
		}
		accessExp := now.Add(accessTokenLifetime)
		refreshExp := now.Add(refreshTokenLifetime)
		ide := &domain.IdeClientToken{
			ID:               ulid.Make().String(),
			UserID:           *code.UserID,
			TenantID:         *code.TenantID,
			Name:             buildIdeClientTokenName(code.DeviceInfo),
			AccessTokenHash:  tokenHash(accessRaw),
			RefreshTokenHash: tokenHash(refreshRaw),
			AccessExpiresAt:  accessExp,
			RefreshExpiresAt: refreshExp,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := s.repo.CreateIdeClientToken(ctx, ide); err != nil {
			return nil, err
		}
		if err := s.repo.DeleteDeviceAuthCode(ctx, deviceCode); err != nil {
			return nil, err
		}
		return &dto.DeviceAuthPollResponse{
			AccessToken:      accessRaw,
			RefreshToken:     refreshRaw,
			ExpiresIn:        int(time.Until(accessExp).Seconds()),
			TokenType:        "Bearer",
			IdeClientTokenID: ide.ID,
		}, nil
	default:
		return nil, domain.ErrDeviceAuthInvalidState
	}
}

// ConfirmUserCode records the end user's decision for the given user (verification) code.
func (s *Service) ConfirmUserCode(ctx context.Context, userCode string, userID, tenantID string, approve bool) error {
	uc := NormalizeUserCode(userCode)
	if len(uc) != 9 { // 4 + '-' + 4
		return domain.ErrDeviceAuthNotFound
	}
	code, err := s.repo.GetDeviceAuthCodeByUserCode(ctx, uc)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrDeviceAuthNotFound
		}
		return err
	}
	now := time.Now()
	if now.After(code.ExpiresAt) {
		code.Status = domain.DeviceAuthCodeStatusExpired
		code.UpdatedAt = now
		_ = s.repo.UpdateDeviceAuthCode(ctx, code)
		_ = s.repo.DeleteDeviceAuthCode(ctx, code.DeviceCode)
		return domain.ErrDeviceAuthExpired
	}
	if code.Status != domain.DeviceAuthCodeStatusPending {
		return domain.ErrDeviceAuthInvalidState
	}
	if !approve {
		code.Status = domain.DeviceAuthCodeStatusDenied
		code.UpdatedAt = now
		return s.repo.UpdateDeviceAuthCode(ctx, code)
	}
	uid, tid := userID, tenantID
	code.Status = domain.DeviceAuthCodeStatusAuthorized
	code.UserID = &uid
	code.TenantID = &tid
	code.UpdatedAt = now
	return s.repo.UpdateDeviceAuthCode(ctx, code)
}

// RefreshTokens validates the refresh token and issues a new access/refresh pair, rotating stored hashes.
func (s *Service) RefreshTokens(ctx context.Context, refreshToken string) (*dto.IdeClientTokenResponse, error) {
	h := tokenHash(refreshToken)
	tok, err := s.repo.GetIdeClientTokenByRefreshTokenHash(ctx, h)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrIdeClientTokenNotFound
		}
		return nil, err
	}
	now := time.Now()
	if now.After(tok.RefreshExpiresAt) {
		_ = s.repo.DeleteIdeClientToken(ctx, tok.ID)
		return nil, domain.ErrIdeClientTokenExpired
	}
	accessRaw, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	refreshRaw, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	accessExp := now.Add(accessTokenLifetime)
	refreshExp := now.Add(refreshTokenLifetime)
	tok.AccessTokenHash = tokenHash(accessRaw)
	tok.RefreshTokenHash = tokenHash(refreshRaw)
	tok.AccessExpiresAt = accessExp
	tok.RefreshExpiresAt = refreshExp
	tok.UpdatedAt = now
	if err := s.repo.UpdateIdeClientToken(ctx, tok); err != nil {
		return nil, err
	}
	return &dto.IdeClientTokenResponse{
		AccessToken:  accessRaw,
		RefreshToken: refreshRaw,
		ExpiresIn:    int(time.Until(accessExp).Seconds()),
		TokenType:    "Bearer",
		TokenID:      tok.ID,
	}, nil
}

// ValidateIdeAccessToken checks the SHA-256 hash of the bearer token against ide_client_tokens and access expiry.
func (s *Service) ValidateIdeAccessToken(ctx context.Context, accessToken string) (*IdeAccessPrincipal, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, domain.ErrIdeClientTokenNotFound
	}
	h := tokenHash(accessToken)
	tok, err := s.repo.GetIdeClientTokenByAccessTokenHash(ctx, h)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrIdeClientTokenNotFound
		}
		return nil, err
	}
	now := time.Now()
	if now.After(tok.AccessExpiresAt) {
		return nil, domain.ErrIdeClientTokenNotFound
	}
	tok.LastUsedAt = &now
	tok.UpdatedAt = now
	_ = s.repo.UpdateIdeClientToken(ctx, tok)
	return &IdeAccessPrincipal{
		TokenID:  tok.ID,
		UserID:   tok.UserID,
		TenantID: tok.TenantID,
	}, nil
}

// ListIdeTokens returns paginated IDE client token records for a tenant (hashes are never exposed).
func (s *Service) ListIdeTokens(ctx context.Context, tenantID string, page, pageSize int) ([]*dto.IdeTokenListItem, int64, error) {
	rows, total, err := s.repo.ListIdeClientTokensByTenantID(ctx, tenantID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*dto.IdeTokenListItem, 0, len(rows))
	for _, t := range rows {
		out = append(out, &dto.IdeTokenListItem{
			ID:               t.ID,
			Name:             t.Name,
			UserID:           t.UserID,
			AccessExpiresAt:  t.AccessExpiresAt,
			RefreshExpiresAt: t.RefreshExpiresAt,
			LastUsedAt:       t.LastUsedAt,
			CreatedAt:        t.CreatedAt,
		})
	}
	return out, total, nil
}

// RevokeIdeToken deletes an IDE client token if it exists and belongs to the given tenant.
func (s *Service) RevokeIdeToken(ctx context.Context, tenantID, tokenID string) error {
	tok, err := s.repo.GetIdeClientTokenByID(ctx, tokenID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	if tok.TenantID != tenantID {
		return domain.ErrForbidden
	}
	return s.repo.DeleteIdeClientToken(ctx, tokenID)
}
