package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/zy-eagle/envnexus/services/platform-api/internal/domain"
	"github.com/zy-eagle/envnexus/services/platform-api/internal/dto"
	"github.com/zy-eagle/envnexus/services/platform-api/internal/infrastructure"
)

type Service struct {
	repo        domain.MarketplaceRepository
	minioClient *infrastructure.MinIOClient
}

func NewService(repo domain.MarketplaceRepository, minio *infrastructure.MinIOClient) *Service {
	return &Service{repo: repo, minioClient: minio}
}

// CreateMarketplaceItemInput is parsed from multipart (or form) for creating an item.
type CreateMarketplaceItemInput struct {
	Type        domain.MarketplaceItemType
	Name        string
	Description string
	Version     string
	Author      string
	Status      domain.MarketplaceItemStatus
	File        io.Reader
	FileSize    int64
	Filename    string
	ContentType string
	// PayloadJSON is used when there is no file (e.g. skill/rule content as raw JSON string).
	PayloadJSON string
}

// UpdateMarketplaceItemInput is parsed from multipart for updating an item.
// Empty string means "leave unchanged" (except type: if set, it must match the current type).
// FileSize > 0 means a new file replaces the stored artifact (plugin) or JSON payload (other types).
// Non-empty PayloadJSON without a new file updates stored JSON for non-plugin types only.
type UpdateMarketplaceItemInput struct {
	Type        string
	Name        string
	Description string
	Version     string
	Author      string
	Status      string
	File        io.Reader
	FileSize    int64
	Filename    string
	ContentType string
	PayloadJSON string
}

// ListItems lists marketplace items with optional type/status filters.
func (s *Service) ListItems(
	ctx context.Context, itemType *domain.MarketplaceItemType, status *domain.MarketplaceItemStatus, page, pageSize int,
) ([]*dto.MarketplaceItemResponse, int64, error) {
	items, total, err := s.repo.ListMarketplaceItems(ctx, itemType, status, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*dto.MarketplaceItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, itemToDTO(it))
	}
	return out, total, nil
}

func itemToDTO(it *domain.MarketplaceItem) *dto.MarketplaceItemResponse {
	if it == nil {
		return nil
	}
	return &dto.MarketplaceItemResponse{
		ID:          it.ID,
		Type:        it.Type,
		Name:        it.Name,
		Description: it.Description,
		Version:     it.Version,
		Author:      it.Author,
		Payload:     it.Payload,
		Status:      it.Status,
		CreatedAt:   it.CreatedAt,
		UpdatedAt:   it.UpdatedAt,
	}
}

// Subscribe adds or reactivates a tenant subscription to a published item.
func (s *Service) Subscribe(ctx context.Context, tenantID, itemID string) (*dto.TenantSubscriptionResponse, error) {
	item, err := s.repo.GetMarketplaceItemByID(ctx, itemID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMarketplaceItemNotFound
		}
		return nil, err
	}
	if item.Status != domain.MarketplaceItemStatusPublished {
		return nil, domain.ErrMarketplaceItemNotPublished
	}
	existing, err := s.repo.GetTenantSubscriptionByTenantAndItem(ctx, tenantID, itemID)
	if err == nil && existing != nil {
		switch existing.Status {
		case domain.TenantSubscriptionStatusActive:
			return nil, domain.ErrMarketplaceAlreadySubscribed
		case domain.TenantSubscriptionStatusRevoked, domain.TenantSubscriptionStatusPending:
			now := time.Now()
			existing.Status = domain.TenantSubscriptionStatusActive
			existing.UpdatedAt = now
			if err := s.repo.UpdateTenantSubscription(ctx, existing); err != nil {
				return nil, err
			}
			return subToDTO(existing), nil
		default:
			return nil, domain.ErrInvalidRequest
		}
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	now := time.Now()
	sub := &domain.TenantSubscription{
		ID:        ulid.Make().String(),
		TenantID:  tenantID,
		ItemID:    itemID,
		Status:    domain.TenantSubscriptionStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.CreateTenantSubscription(ctx, sub); err != nil {
		return nil, err
	}
	return subToDTO(sub), nil
}

// Unsubscribe marks a subscription as revoked. Idempotent if already revoked.
func (s *Service) Unsubscribe(ctx context.Context, tenantID, itemID string) error {
	sub, err := s.repo.GetTenantSubscriptionByTenantAndItem(ctx, tenantID, itemID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrMarketplaceSubscriptionNotFound
		}
		return err
	}
	if sub.Status == domain.TenantSubscriptionStatusRevoked {
		return nil
	}
	now := time.Now()
	sub.Status = domain.TenantSubscriptionStatusRevoked
	sub.UpdatedAt = now
	return s.repo.UpdateTenantSubscription(ctx, sub)
}

// ListSubscriptions returns paginated tenant subscriptions.
func (s *Service) ListSubscriptions(ctx context.Context, tenantID string, page, pageSize int) ([]*dto.TenantSubscriptionResponse, int64, error) {
	subs, total, err := s.repo.ListTenantSubscriptionsByTenantID(ctx, tenantID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*dto.TenantSubscriptionResponse, 0, len(subs))
	for _, sub := range subs {
		out = append(out, subToDTO(sub))
	}
	return out, total, nil
}

func subToDTO(s *domain.TenantSubscription) *dto.TenantSubscriptionResponse {
	if s == nil {
		return nil
	}
	return &dto.TenantSubscriptionResponse{
		ID:        s.ID,
		TenantID:  s.TenantID,
		ItemID:    s.ItemID,
		Status:    s.Status,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// IdeSyncManifest returns full marketplace item payloads for all active tenant subscriptions (IDE pull).
func (s *Service) IdeSyncManifest(ctx context.Context, tenantID string) ([]*dto.MarketplaceItemResponse, error) {
	items, err := s.repo.ListActiveSubscribedItemsForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*dto.MarketplaceItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, itemToDTO(it))
	}
	return out, nil
}

// GetItemDownloadURL returns a download URL for a published, subscribed marketplace item.
// If the payload has "object_key" and object storage is configured, a fresh presigned GET URL is returned.
// Otherwise, if the payload has "download_url", that value is used; else a placeholder URL is returned.
func (s *Service) GetLatestIDEExtensionInfo(ctx context.Context) (*dto.ExtensionUpdateResponse, error) {
	item, err := s.repo.GetLatestMarketplaceItemByName(ctx, "EnvNexus IDE Sync")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMarketplaceItemNotFound
		}
		return nil, err
	}
	var payload struct {
		ObjectKey   string `json:"object_key"`
		DownloadURL string `json:"download_url"`
	}
	if item.Payload != "" {
		_ = json.Unmarshal([]byte(item.Payload), &payload)
	}
	url := strings.TrimSpace(payload.DownloadURL)
	if payload.ObjectKey != "" && s.minioClient != nil {
		if presigned, err := s.minioClient.PresignedGetURL(ctx, payload.ObjectKey, time.Hour); err == nil && presigned != nil {
			url = presigned.String()
		}
	}
	if url == "" {
		url = fmt.Sprintf("https://example.com/marketplace/plugins/%s.zip", item.ID)
	}
	return &dto.ExtensionUpdateResponse{
		Version:     item.Version,
		DownloadURL: url,
	}, nil
}

func (s *Service) GetItemDownloadURL(ctx context.Context, tenantID, itemID string) (*dto.MarketplaceItemDownloadResponse, error) {
	item, err := s.repo.GetMarketplaceItemByID(ctx, itemID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMarketplaceItemNotFound
		}
		return nil, err
	}
	if item.Status != domain.MarketplaceItemStatusPublished {
		return nil, domain.ErrMarketplaceItemNotPublished
	}
	sub, err := s.repo.GetTenantSubscriptionByTenantAndItem(ctx, tenantID, itemID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMarketplaceSubscriptionNotFound
		}
		return nil, err
	}
	if sub.Status != domain.TenantSubscriptionStatusActive {
		return nil, domain.ErrMarketplaceSubscriptionNotFound
	}
	var payload struct {
		DownloadURL string `json:"download_url"`
		ObjectKey   string `json:"object_key"`
	}
	if item.Payload != "" {
		_ = json.Unmarshal([]byte(item.Payload), &payload)
	}
	if key := strings.TrimSpace(payload.ObjectKey); key != "" && s.minioClient != nil {
		u, err := s.minioClient.PresignedGetURL(ctx, key, time.Hour)
		if err == nil && u != nil {
			return &dto.MarketplaceItemDownloadResponse{DownloadURL: u.String()}, nil
		}
	}
	url := strings.TrimSpace(payload.DownloadURL)
	if url == "" {
		url = fmt.Sprintf("https://example.com/marketplace/plugins/%s.zip", itemID)
	}
	return &dto.MarketplaceItemDownloadResponse{DownloadURL: url}, nil
}

const maxMarketplaceFileBytes = 32 << 20

func isKnownItemType(t domain.MarketplaceItemType) bool {
	switch t {
	case domain.MarketplaceItemTypeMcp, domain.MarketplaceItemTypeSkill, domain.MarketplaceItemTypeSubagent,
		domain.MarketplaceItemTypePlugin, domain.MarketplaceItemTypeRule:
		return true
	default:
		return false
	}
}

func isKnownStatus(st domain.MarketplaceItemStatus) bool {
	switch st {
	case domain.MarketplaceItemStatusPublished, domain.MarketplaceItemStatusDraft, domain.MarketplaceItemStatusArchived:
		return true
	default:
		return false
	}
}

func safeObjectFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\x00", ""))
	if name == "." || name == "/" || name == "" || strings.Contains(name, "..") {
		return "artifact"
	}
	return name
}

func extractObjectKey(payload string) string {
	var p struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.ObjectKey)
}

func (s *Service) buildPluginPayloadJSON(ctx context.Context, objectKey string) (string, error) {
	p := struct {
		ObjectKey   string `json:"object_key"`
		DownloadURL string `json:"download_url"`
	}{
		ObjectKey: objectKey,
	}
	if s.minioClient != nil {
		if u, err := s.minioClient.PresignedGetURL(ctx, objectKey, 7*24*time.Hour); err == nil && u != nil {
			p.DownloadURL = u.String()
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readValidateJSONPayload(r io.Reader, size int64) (string, error) {
	if size <= 0 || size > maxMarketplaceFileBytes {
		return "", domain.ErrInvalidRequest
	}
	b, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > size || int64(len(b)) > maxMarketplaceFileBytes {
		return "", domain.ErrInvalidRequest
	}
	if !json.Valid(b) {
		return "", domain.ErrInvalidRequest
	}
	return string(b), nil
}

// itemTypeUsesObjectStorage returns true for types whose artifacts are stored
// in MinIO as binary files (zip/vsix) rather than inline JSON payloads.
func itemTypeUsesObjectStorage(t domain.MarketplaceItemType) bool {
	switch t {
	case domain.MarketplaceItemTypePlugin, domain.MarketplaceItemTypeSkill, domain.MarketplaceItemTypeRule:
		return true
	default:
		return false
	}
}

// CreateMarketplaceItem stores a new item. Plugin types require a file and MinIO; other types use a file or a raw JSON "payload" string.
func (s *Service) CreateMarketplaceItem(ctx context.Context, in CreateMarketplaceItemInput) (*dto.MarketplaceItemResponse, error) {
	name := strings.TrimSpace(in.Name)
	ver := strings.TrimSpace(in.Version)
	if in.Type == "" || name == "" || ver == "" {
		return nil, domain.ErrInvalidRequest
	}
	if !isKnownItemType(in.Type) {
		return nil, domain.ErrInvalidRequest
	}
	st := in.Status
	if st == "" {
		st = domain.MarketplaceItemStatusDraft
	}
	if !isKnownStatus(st) {
		return nil, domain.ErrInvalidRequest
	}
	now := time.Now()
	id := ulid.Make().String()
	var payload string
	if itemTypeUsesObjectStorage(in.Type) {
		if in.File == nil || in.FileSize <= 0 {
			return nil, domain.ErrInvalidRequest
		}
		if s.minioClient == nil {
			return nil, domain.ErrObjectStorageUnavailable
		}
		ct := strings.TrimSpace(in.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		key := fmt.Sprintf("marketplace/items/%s/%s", id, safeObjectFilename(in.Filename))
		if err := s.minioClient.PutObject(ctx, key, in.File, in.FileSize, ct); err != nil {
			return nil, err
		}
		p, err := s.buildPluginPayloadJSON(ctx, key)
		if err != nil {
			return nil, err
		}
		payload = p
	} else {
		if in.File != nil && in.FileSize > 0 {
			p, err := readValidateJSONPayload(in.File, in.FileSize)
			if err != nil {
				return nil, err
			}
			payload = p
		} else {
			p := strings.TrimSpace(in.PayloadJSON)
			if p == "" {
				return nil, domain.ErrInvalidRequest
			}
			if !json.Valid([]byte(p)) {
				return nil, domain.ErrInvalidRequest
			}
			payload = p
		}
	}
	item := &domain.MarketplaceItem{
		ID:          id,
		Type:        in.Type,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Version:     ver,
		Author:      strings.TrimSpace(in.Author),
		Payload:     payload,
		Status:      st,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.CreateMarketplaceItem(ctx, item); err != nil {
		return nil, err
	}
	return itemToDTO(item), nil
}

// UpdateMarketplaceItem updates metadata and/or replaces the artifact (plugin) or JSON payload (other types).
func (s *Service) UpdateMarketplaceItem(ctx context.Context, id string, in UpdateMarketplaceItemInput) (*dto.MarketplaceItemResponse, error) {
	item, err := s.repo.GetMarketplaceItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMarketplaceItemNotFound
		}
		return nil, err
	}
	if t := strings.TrimSpace(in.Type); t != "" && domain.MarketplaceItemType(t) != item.Type {
		return nil, domain.ErrInvalidRequest
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		item.Name = n
	}
	if n := strings.TrimSpace(in.Version); n != "" {
		item.Version = n
	}
	if n := strings.TrimSpace(in.Description); n != "" {
		item.Description = n
	}
	if n := strings.TrimSpace(in.Author); n != "" {
		item.Author = n
	}
	if s := strings.TrimSpace(in.Status); s != "" {
		st := domain.MarketplaceItemStatus(s)
		if !isKnownStatus(st) {
			return nil, domain.ErrInvalidRequest
		}
		item.Status = st
	}
	if in.FileSize > 0 && in.File != nil {
		if itemTypeUsesObjectStorage(item.Type) {
			if s.minioClient == nil {
				return nil, domain.ErrObjectStorageUnavailable
			}
			ct := strings.TrimSpace(in.ContentType)
			if ct == "" {
				ct = "application/octet-stream"
			}
			newKey := fmt.Sprintf("marketplace/items/%s/%s", id, safeObjectFilename(in.Filename))
			if err := s.minioClient.PutObject(ctx, newKey, in.File, in.FileSize, ct); err != nil {
				return nil, err
			}
			if old := extractObjectKey(item.Payload); old != "" && old != newKey {
				_ = s.minioClient.RemoveObject(ctx, old)
			}
			p, err := s.buildPluginPayloadJSON(ctx, newKey)
			if err != nil {
				return nil, err
			}
			item.Payload = p
		} else {
			p, err := readValidateJSONPayload(in.File, in.FileSize)
			if err != nil {
				return nil, err
			}
			item.Payload = p
		}
	} else if strings.TrimSpace(in.PayloadJSON) != "" {
		if itemTypeUsesObjectStorage(item.Type) {
			return nil, domain.ErrInvalidRequest
		}
		if !json.Valid([]byte(in.PayloadJSON)) {
			return nil, domain.ErrInvalidRequest
		}
		item.Payload = in.PayloadJSON
	}
	item.UpdatedAt = time.Now()
	if err := s.repo.UpdateMarketplaceItem(ctx, item); err != nil {
		return nil, err
	}
	return itemToDTO(item), nil
}

// DeleteMarketplaceItem removes the item. Plugin artifacts in object storage are best-effort deleted when present in payload.
func (s *Service) DeleteMarketplaceItem(ctx context.Context, id string) error {
	item, err := s.repo.GetMarketplaceItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrMarketplaceItemNotFound
		}
		return err
	}
	if itemTypeUsesObjectStorage(item.Type) && s.minioClient != nil {
		if k := extractObjectKey(item.Payload); k != "" {
			_ = s.minioClient.RemoveObject(ctx, k)
		}
	}
	return s.repo.DeleteMarketplaceItem(ctx, id)
}
