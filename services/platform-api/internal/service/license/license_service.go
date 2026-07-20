package license

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

// ValidateLicenseResult is the decoded, validated state of a license.
type ValidateLicenseResult struct {
	Valid      bool
	TenantID   string
	PlanCode   string
	MaxDevices int
	ExpiresAt  time.Time
	Features   []string
}

type Service struct {
	repo repository.LicenseRepository
}

func NewService(repo repository.LicenseRepository) *Service {
	return &Service{repo: repo}
}

// Activate activates a license key for a tenant.
func (s *Service) Activate(ctx context.Context, tenantID, licenseKey string) (*repository.LicenseRow, error) {
	if err := s.verifyKeyFormat(licenseKey); err != nil {
		return nil, fmt.Errorf("invalid license key format: %w", err)
	}

	plan, maxDevices, expiresAt, err := parseKey(licenseKey)
	if err != nil {
		return nil, fmt.Errorf("parse license key: %w", err)
	}

	row := &repository.LicenseRow{
		ID:         ulid.Make().String(),
		TenantID:   tenantID,
		LicenseKey: licenseKey,
		PlanCode:   plan,
		MaxDevices: maxDevices,
		IssuedAt:   time.Now(),
		ExpiresAt:  expiresAt,
		Status:     "active",
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, err
	}
	slog.Info("License activated", "tenant_id", tenantID, "plan", plan, "max_devices", maxDevices, "expires_at", expiresAt)
	return row, nil
}

// Validate checks if a tenant has a valid active license, returning a trial fallback if none.
func (s *Service) Validate(ctx context.Context, tenantID string) (*ValidateLicenseResult, error) {
	row, err := s.repo.GetActiveByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &ValidateLicenseResult{
			Valid:      true,
			TenantID:   tenantID,
			PlanCode:   "trial",
			MaxDevices: 5,
			ExpiresAt:  time.Now().AddDate(0, 1, 0),
		}, nil
	}

	return &ValidateLicenseResult{
		Valid:      true,
		TenantID:   tenantID,
		PlanCode:   row.PlanCode,
		MaxDevices: row.MaxDevices,
		ExpiresAt:  row.ExpiresAt,
	}, nil
}

// GetForTenant returns the most recent license for a tenant (any status).
func (s *Service) GetForTenant(ctx context.Context, tenantID string) (*repository.LicenseRow, error) {
	return s.repo.GetLatestByTenant(ctx, tenantID)
}

// Revoke marks a license as revoked.
func (s *Service) Revoke(ctx context.Context, licenseID string) error {
	return s.repo.Revoke(ctx, licenseID)
}

func (s *Service) verifyKeyFormat(key string) error {
	parts := strings.Split(key, "-")
	if len(parts) < 5 || parts[0] != "ENX" {
		return fmt.Errorf("key must be in format ENX-{plan}-{max_devices}-{YYYYMM}-{checksum}")
	}
	return nil
}

// parseKey extracts plan, max devices, and expiry from the key.
// Format: ENX-{PLAN}-{MaxDevices}-{YYYYMM}-{checksum}
func parseKey(key string) (plan string, maxDevices int, expiresAt time.Time, err error) {
	parts := strings.Split(key, "-")
	if len(parts) < 5 {
		return "", 0, time.Time{}, fmt.Errorf("invalid key format")
	}

	plan = strings.ToLower(parts[1])

	if _, err = fmt.Sscanf(parts[2], "%d", &maxDevices); err != nil {
		return "", 0, time.Time{}, fmt.Errorf("invalid max_devices in key")
	}

	expiresAt, err = time.Parse("200601", parts[3])
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("invalid expiry date in key (expected YYYYMM)")
	}
	expiresAt = expiresAt.AddDate(0, 1, 0).Add(-time.Second)

	base := strings.Join(parts[:4], "-")
	expectedChecksum := computeChecksum(base)
	if !strings.HasPrefix(expectedChecksum, parts[4]) {
		return "", 0, time.Time{}, fmt.Errorf("license key checksum mismatch")
	}

	return plan, maxDevices, expiresAt, nil
}

func computeChecksum(s string) string {
	h := sha256.Sum256([]byte(s))
	return strings.ToUpper(hex.EncodeToString(h[:4]))
}

// GenerateKey generates a valid license key for operator tooling.
// Format: ENX-{PLAN}-{MaxDevices}-{YYYYMM}-{checksum}
func GenerateKey(plan string, maxDevices int, expiryYYYYMM string) string {
	plan = strings.ToUpper(plan)
	base := fmt.Sprintf("ENX-%s-%d-%s", plan, maxDevices, expiryYYYYMM)
	return base + "-" + computeChecksum(base)
}
