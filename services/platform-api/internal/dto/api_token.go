package dto

import "time"

type CreateApiTokenRequest struct {
	Name         string   `json:"name" binding:"required,min=1,max=255"`
	Scopes       []string `json:"scopes"`
	IsSuperAdmin bool     `json:"is_super_admin"`
	ExpiresIn    *int     `json:"expires_in"` // seconds; nil or 0 = never expires
}

type CreateApiTokenResponse struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Token        string     `json:"token"` // plaintext, shown only once
	TokenPrefix  string     `json:"token_prefix"`
	Scopes       []string   `json:"scopes"`
	IsSuperAdmin bool       `json:"is_super_admin"`
	ExpiresAt    *time.Time `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type ApiTokenListItem struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	TokenPrefix  string     `json:"token_prefix"`
	Scopes       []string   `json:"scopes"`
	IsSuperAdmin bool       `json:"is_super_admin"`
	ExpiresAt    *time.Time `json:"expires_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    time.Time  `json:"created_at"`
}
