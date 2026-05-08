package domain

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// StringList is a JSON-backed []string for GORM columns of type json.
type StringList []string

func (s StringList) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal(s)
	return string(b), err
}

func (s *StringList) Scan(src interface{}) error {
	if src == nil {
		*s = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("unsupported type %T for StringList", src)
	}
	return json.Unmarshal(raw, s)
}

// ApiToken is a long-lived, opaque bearer token for programmatic / third-party
// access (e.g. MCP servers, CI pipelines). The plaintext is only revealed once
// at creation; only the SHA-256 hash is persisted.
type ApiToken struct {
	ID           string     `json:"id"             gorm:"primaryKey;size:26"`
	UserID       string     `json:"user_id"        gorm:"size:26;not null;index"`
	TenantID     string     `json:"tenant_id"      gorm:"size:26;not null;index"`
	Name         string     `json:"name"           gorm:"size:255;not null"`
	TokenHash    string     `json:"-"              gorm:"size:64;not null;uniqueIndex"`
	TokenPrefix  string     `json:"token_prefix"   gorm:"size:12;not null;index"`
	Scopes       StringList `json:"scopes"         gorm:"type:json"`
	IsSuperAdmin bool       `json:"is_super_admin" gorm:"not null;default:false"`
	ExpiresAt    *time.Time `json:"expires_at"     gorm:"index"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (ApiToken) TableName() string { return "api_tokens" }

// ApiTokenRepository persists API tokens.
type ApiTokenRepository interface {
	CreateApiToken(ctx context.Context, t *ApiToken) error
	GetApiTokenByID(ctx context.Context, id string) (*ApiToken, error)
	GetApiTokenByHash(ctx context.Context, tokenHash string) (*ApiToken, error)
	ListApiTokensByTenantID(ctx context.Context, tenantID string, page, pageSize int) ([]*ApiToken, int64, error)
	ListApiTokensByUserID(ctx context.Context, userID string, page, pageSize int) ([]*ApiToken, int64, error)
	UpdateApiToken(ctx context.Context, t *ApiToken) error
	DeleteApiToken(ctx context.Context, id string) error
}
