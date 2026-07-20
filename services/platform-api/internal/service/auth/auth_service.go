package auth

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Service struct {
	userRepo           repository.UserRepository
	jwtSecret          string
	deviceSecret       string
	sessionSecret      string
	tokenExpiration    time.Duration
	refreshExpiration  time.Duration
}

func NewService(userRepo repository.UserRepository, jwtSecret, deviceSecret, sessionSecret string) *Service {
	return &Service{
		userRepo:          userRepo,
		jwtSecret:         jwtSecret,
		deviceSecret:      deviceSecret,
		sessionSecret:     sessionSecret,
		tokenExpiration:   1 * time.Hour,
		refreshExpiration: 7 * 24 * time.Hour,
	}
}

func (s *Service) Login(ctx context.Context, req dto.LoginRequest) (*dto.LoginResponse, error) {
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if user == nil {
		return nil, domain.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	now := time.Now()
	claims := &middleware.Claims{
		UserID:             user.ID,
		TenantID:           user.TenantID,
		Email:              user.Email,
		DisplayName:        user.DisplayName,
		PlatformSuperAdmin: user.PlatformSuperAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        ulid.Make().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return nil, domain.ErrInternalError
	}

	refreshTokenStr, err := s.issueRefreshToken(user.ID, user.TenantID)
	if err != nil {
		return nil, domain.ErrInternalError
	}

	user.LastLoginAt = &now
	_ = s.userRepo.Update(ctx, user)

	resp := &dto.LoginResponse{
		AccessToken:  tokenStr,
		RefreshToken: refreshTokenStr,
		ExpiresIn:    int(s.tokenExpiration.Seconds()),
	}
	resp.User.ID = user.ID
	resp.User.TenantID = user.TenantID
	resp.User.Email = user.Email
	resp.User.DisplayName = user.DisplayName
	resp.User.PlatformSuperAdmin = user.PlatformSuperAdmin

	return resp, nil
}

func (s *Service) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	return s.userRepo.GetByID(ctx, userID)
}

func (s *Service) IssueDeviceToken(deviceID, tenantID string) (string, error) {
	now := time.Now()
	claims := &middleware.DeviceClaims{
		DeviceID: deviceID,
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(365 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        ulid.Make().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.deviceSecret))
}

func (s *Service) IssueSessionToken(deviceID, tenantID, sessionID string) (string, error) {
	now := time.Now()
	claims := &middleware.SessionTokenClaims{
		DeviceID:  deviceID,
		TenantID:  tenantID,
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        ulid.Make().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.sessionSecret))
}

func (s *Service) GetSessionSecret() string {
	return s.sessionSecret
}

type RefreshClaims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	jwt.RegisteredClaims
}

func (s *Service) issueRefreshToken(userID, tenantID string) (string, error) {
	now := time.Now()
	claims := &RefreshClaims{
		UserID:   userID,
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshExpiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        ulid.Make().String(),
			Subject:   "refresh",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

func (s *Service) RefreshAccessToken(ctx context.Context, refreshTokenStr string) (*dto.RefreshTokenResponse, error) {
	claims := &RefreshClaims{}
	token, err := jwt.ParseWithClaims(refreshTokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, domain.ErrInvalidCredentials
	}
	if claims.Subject != "refresh" {
		return nil, domain.ErrInvalidCredentials
	}

	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil || user == nil {
		return nil, domain.ErrInvalidCredentials
	}

	now := time.Now()
	accessClaims := &middleware.Claims{
		UserID:             user.ID,
		TenantID:           user.TenantID,
		Email:              user.Email,
		DisplayName:        user.DisplayName,
		PlatformSuperAdmin: user.PlatformSuperAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        ulid.Make().String(),
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenStr, err := accessToken.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return nil, domain.ErrInternalError
	}

	newRefreshTokenStr, err := s.issueRefreshToken(user.ID, user.TenantID)
	if err != nil {
		return nil, domain.ErrInternalError
	}

	return &dto.RefreshTokenResponse{
		AccessToken:  accessTokenStr,
		RefreshToken: newRefreshTokenStr,
		ExpiresIn:    int(s.tokenExpiration.Seconds()),
	}, nil
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return domain.ErrUserNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return domain.ErrIncorrectPassword
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return domain.ErrInternalError
	}

	user.PasswordHash = string(hashed)
	if err := s.userRepo.Update(ctx, user); err != nil {
		return domain.ErrInternalError
	}
	return nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}
