package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
)

type mockUserRepo struct {
	users map[string]*domain.User
}

func (m *mockUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, nil
}
func (m *mockUserRepo) GetByID(_ context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, nil
	}
	return u, nil
}
func (m *mockUserRepo) Create(_ context.Context, u *domain.User) error { m.users[u.ID] = u; return nil }
func (m *mockUserRepo) Update(_ context.Context, u *domain.User) error { m.users[u.ID] = u; return nil }
func (m *mockUserRepo) Delete(_ context.Context, id string) error      { delete(m.users, id); return nil }
func (m *mockUserRepo) ListByTenant(_ context.Context, tenantID string) ([]*domain.User, error) {
	var result []*domain.User
	for _, u := range m.users {
		if u.TenantID == tenantID {
			result = append(result, u)
		}
	}
	return result, nil
}
func (m *mockUserRepo) SearchByTenant(_ context.Context, tenantID, query string, limit int) ([]*domain.User, error) {
	_ = query
	if limit <= 0 {
		limit = 20
	}
	users, err := m.ListByTenant(context.Background(), tenantID)
	if err != nil {
		return nil, err
	}
	if len(users) <= limit {
		return users, nil
	}
	return users[:limit], nil
}

func newTestService() *Service {
	hash, _ := HashPassword("test-password")
	repo := &mockUserRepo{
		users: map[string]*domain.User{
			"user-1": {
				ID:           "user-1",
				TenantID:     "tenant-1",
				Email:        "admin@test.com",
				DisplayName:  "Admin",
				PasswordHash: hash,
				Status:       "active",
			},
		},
	}
	return NewService(repo, "test-jwt-secret", "test-device-secret", "test-session-secret")
}

func TestService_IssueDeviceToken(t *testing.T) {
	svc := newTestService()
	tokenStr, err := svc.IssueDeviceToken("device-1", "tenant-1")
	if err != nil {
		t.Fatalf("IssueDeviceToken failed: %v", err)
	}

	parsed, err := jwt.ParseWithClaims(tokenStr, &middleware.DeviceClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-device-secret"), nil
	})
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}

	claims, ok := parsed.Claims.(*middleware.DeviceClaims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	if claims.DeviceID != "device-1" {
		t.Errorf("DeviceID = %s, want device-1", claims.DeviceID)
	}
	if claims.TenantID != "tenant-1" {
		t.Errorf("TenantID = %s, want tenant-1", claims.TenantID)
	}

	exp, _ := claims.GetExpirationTime()
	if exp == nil || exp.Time.Before(time.Now().Add(364*24*time.Hour)) {
		t.Error("expected device token to expire ~365 days from now")
	}
}

func TestService_IssueSessionToken(t *testing.T) {
	svc := newTestService()
	tokenStr, err := svc.IssueSessionToken("device-1", "tenant-1", "session-1")
	if err != nil {
		t.Fatalf("IssueSessionToken failed: %v", err)
	}

	parsed, err := jwt.ParseWithClaims(tokenStr, &middleware.SessionTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-session-secret"), nil
	})
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}

	claims, ok := parsed.Claims.(*middleware.SessionTokenClaims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	if claims.DeviceID != "device-1" {
		t.Errorf("DeviceID = %s, want device-1", claims.DeviceID)
	}
	if claims.SessionID != "session-1" {
		t.Errorf("SessionID = %s, want session-1", claims.SessionID)
	}

	exp, _ := claims.GetExpirationTime()
	if exp == nil || exp.Time.Before(time.Now().Add(29*time.Minute)) {
		t.Error("expected session token to expire ~30 min from now")
	}
}

func TestService_Login_Success(t *testing.T) {
	svc := newTestService()
	resp, err := svc.Login(context.Background(), dto.LoginRequest{
		Email:    "admin@test.com",
		Password: "test-password",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.User.Email != "admin@test.com" {
		t.Errorf("User.Email = %s, want admin@test.com", resp.User.Email)
	}
}

func TestService_Login_WrongPassword(t *testing.T) {
	svc := newTestService()
	_, err := svc.Login(context.Background(), dto.LoginRequest{
		Email:    "admin@test.com",
		Password: "wrong-password",
	})
	if err != domain.ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestService_Login_UserNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.Login(context.Background(), dto.LoginRequest{
		Email:    "nobody@test.com",
		Password: "any",
	})
	if err != domain.ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("my-secret")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" || hash == "my-secret" {
		t.Error("expected non-empty hash different from plain password")
	}
}
