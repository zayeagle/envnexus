package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

// MySQLMarketplaceRepository implements domain.MarketplaceRepository with GORM.
type MySQLMarketplaceRepository struct {
	db *gorm.DB
}

// NewMySQLMarketplaceRepository creates a MySQL-backed marketplace repository.
func NewMySQLMarketplaceRepository(db *gorm.DB) *MySQLMarketplaceRepository {
	return &MySQLMarketplaceRepository{db: db}
}

func (r *MySQLMarketplaceRepository) CreateMarketplaceItem(ctx context.Context, item *domain.MarketplaceItem) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *MySQLMarketplaceRepository) GetMarketplaceItemByID(ctx context.Context, id string) (*domain.MarketplaceItem, error) {
	var item domain.MarketplaceItem
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MySQLMarketplaceRepository) GetLatestMarketplaceItemByName(ctx context.Context, name string) (*domain.MarketplaceItem, error) {
	var item domain.MarketplaceItem
	if err := r.db.WithContext(ctx).Where("name = ? AND status = ?", name, domain.MarketplaceItemStatusPublished).Order("created_at DESC").First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MySQLMarketplaceRepository) ListMarketplaceItems(
	ctx context.Context, itemType *domain.MarketplaceItemType, status *domain.MarketplaceItemStatus, page, pageSize int,
) ([]*domain.MarketplaceItem, int64, error) {
	var items []*domain.MarketplaceItem
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.MarketplaceItem{})
	if itemType != nil {
		q = q.Where("type = ?", *itemType)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *MySQLMarketplaceRepository) UpdateMarketplaceItem(ctx context.Context, item *domain.MarketplaceItem) error {
	return r.db.WithContext(ctx).Save(item).Error
}

func (r *MySQLMarketplaceRepository) DeleteMarketplaceItem(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.MarketplaceItem{}).Error
}

func (r *MySQLMarketplaceRepository) CreateTenantSubscription(ctx context.Context, sub *domain.TenantSubscription) error {
	return r.db.WithContext(ctx).Create(sub).Error
}

func (r *MySQLMarketplaceRepository) GetTenantSubscriptionByID(ctx context.Context, id string) (*domain.TenantSubscription, error) {
	var sub domain.TenantSubscription
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *MySQLMarketplaceRepository) GetTenantSubscriptionByTenantAndItem(ctx context.Context, tenantID, itemID string) (*domain.TenantSubscription, error) {
	var sub domain.TenantSubscription
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_id = ?", tenantID, itemID).
		First(&sub).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *MySQLMarketplaceRepository) ListActiveSubscribedItemsForTenant(
	ctx context.Context, tenantID string,
) ([]*domain.MarketplaceItem, error) {
	var items []*domain.MarketplaceItem
	err := r.db.WithContext(ctx).Model(&domain.MarketplaceItem{}).
		Joins(
			"JOIN tenant_subscriptions ON tenant_subscriptions.item_id = marketplace_items.id AND tenant_subscriptions.tenant_id = ? AND tenant_subscriptions.status = ?",
			tenantID, domain.TenantSubscriptionStatusActive,
		).
		Order("marketplace_items.name ASC, marketplace_items.version DESC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *MySQLMarketplaceRepository) ListTenantSubscriptionsByTenantID(
	ctx context.Context, tenantID string, page, pageSize int,
) ([]*domain.TenantSubscription, int64, error) {
	var subs []*domain.TenantSubscription
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.TenantSubscription{}).Where("tenant_id = ?", tenantID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&subs).Error; err != nil {
		return nil, 0, err
	}
	return subs, total, nil
}

func (r *MySQLMarketplaceRepository) ListTenantSubscriptionsByItemID(
	ctx context.Context, itemID string, page, pageSize int,
) ([]*domain.TenantSubscription, int64, error) {
	var subs []*domain.TenantSubscription
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.TenantSubscription{}).Where("item_id = ?", itemID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		q = q.Offset(offset).Limit(pageSize)
	}
	if err := q.Order("created_at DESC").Find(&subs).Error; err != nil {
		return nil, 0, err
	}
	return subs, total, nil
}

func (r *MySQLMarketplaceRepository) UpdateTenantSubscription(ctx context.Context, sub *domain.TenantSubscription) error {
	return r.db.WithContext(ctx).Save(sub).Error
}

func (r *MySQLMarketplaceRepository) DeleteTenantSubscription(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.TenantSubscription{}).Error
}

// Compile-time check that the repository implements the domain interface.
var _ domain.MarketplaceRepository = (*MySQLMarketplaceRepository)(nil)
