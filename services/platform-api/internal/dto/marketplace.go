package dto

import (
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

// MarketplaceItemResponse is a published marketplace item for listing and subscription flows.
type MarketplaceItemResponse struct {
	ID          string                      `json:"id"`
	Type        domain.MarketplaceItemType  `json:"type"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Version     string                      `json:"version"`
	Author      string                      `json:"author"`
	Payload     string                      `json:"payload"`
	Status      domain.MarketplaceItemStatus `json:"status"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

// MarketplaceSubscribeRequest subscribes the tenant to a published item.
type MarketplaceSubscribeRequest struct {
	ItemID string `json:"item_id" binding:"required"`
}

// TenantSubscriptionResponse is a tenant's subscription to a marketplace item.
type TenantSubscriptionResponse struct {
	ID        string                        `json:"id"`
	TenantID  string                        `json:"tenant_id"`
	ItemID    string                        `json:"item_id"`
	Status    domain.TenantSubscriptionStatus `json:"status"`
	CreatedAt time.Time                     `json:"created_at"`
	UpdatedAt time.Time                     `json:"updated_at"`
}

// MarketplaceItemDownloadResponse returns a time-limited or external URL to fetch the item artifact.
type MarketplaceItemDownloadResponse struct {
	DownloadURL string `json:"download_url"`
}

// ExtensionUpdateResponse contains the latest version and download URL for the IDE extension.
type ExtensionUpdateResponse struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
}
