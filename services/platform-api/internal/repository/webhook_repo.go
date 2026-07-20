package repository

import (
	"context"
	"errors"
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

// ── WebhookSubscriptionRepository ────────────────────────────────────────────

type WebhookSubscriptionRepository interface {
	Create(ctx context.Context, sub *domain.WebhookSubscription) error
	GetByID(ctx context.Context, id string) (*domain.WebhookSubscription, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*domain.WebhookSubscription, error)
	FindByEventType(ctx context.Context, tenantID, eventType string) ([]*domain.WebhookSubscription, error)
	Update(ctx context.Context, sub *domain.WebhookSubscription) error
	Delete(ctx context.Context, id string) error
}

type MySQLWebhookSubscriptionRepository struct {
	db *gorm.DB
}

func NewMySQLWebhookSubscriptionRepository(db *gorm.DB) *MySQLWebhookSubscriptionRepository {
	return &MySQLWebhookSubscriptionRepository{db: db}
}

func (r *MySQLWebhookSubscriptionRepository) Create(ctx context.Context, sub *domain.WebhookSubscription) error {
	return r.db.WithContext(ctx).Create(sub).Error
}

func (r *MySQLWebhookSubscriptionRepository) GetByID(ctx context.Context, id string) (*domain.WebhookSubscription, error) {
	var sub domain.WebhookSubscription
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&sub).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

func (r *MySQLWebhookSubscriptionRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domain.WebhookSubscription, error) {
	var subs []*domain.WebhookSubscription
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&subs).Error
	return subs, err
}

func (r *MySQLWebhookSubscriptionRepository) FindByEventType(ctx context.Context, tenantID, eventType string) ([]*domain.WebhookSubscription, error) {
	var subs []*domain.WebhookSubscription
	// Find subs that have "*" or the specific event_type in their JSON array
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND (JSON_CONTAINS(event_types, JSON_QUOTE(?)) OR JSON_CONTAINS(event_types, JSON_QUOTE('*')))",
			tenantID, "active", eventType).
		Find(&subs).Error
	return subs, err
}

func (r *MySQLWebhookSubscriptionRepository) Update(ctx context.Context, sub *domain.WebhookSubscription) error {
	return r.db.WithContext(ctx).Save(sub).Error
}

func (r *MySQLWebhookSubscriptionRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.WebhookSubscription{}, "id = ?", id).Error
}

// ── WebhookDeliveryRepository ─────────────────────────────────────────────────

type WebhookDeliveryRepository interface {
	Create(ctx context.Context, delivery *domain.WebhookDelivery) error
	GetByID(ctx context.Context, id string) (*domain.WebhookDelivery, error)
	ListPendingRetries(ctx context.Context, limit int) ([]*domain.WebhookDelivery, error)
	Update(ctx context.Context, delivery *domain.WebhookDelivery) error
}

type MySQLWebhookDeliveryRepository struct {
	db *gorm.DB
}

func NewMySQLWebhookDeliveryRepository(db *gorm.DB) *MySQLWebhookDeliveryRepository {
	return &MySQLWebhookDeliveryRepository{db: db}
}

func (r *MySQLWebhookDeliveryRepository) Create(ctx context.Context, delivery *domain.WebhookDelivery) error {
	return r.db.WithContext(ctx).Create(delivery).Error
}

func (r *MySQLWebhookDeliveryRepository) GetByID(ctx context.Context, id string) (*domain.WebhookDelivery, error) {
	var d domain.WebhookDelivery
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (r *MySQLWebhookDeliveryRepository) ListPendingRetries(ctx context.Context, limit int) ([]*domain.WebhookDelivery, error) {
	var deliveries []*domain.WebhookDelivery
	now := time.Now()
	err := r.db.WithContext(ctx).
		Where("status IN (?) AND (next_retry_at IS NULL OR next_retry_at <= ?)", []string{"pending", "failed"}, now).
		Order("created_at ASC").
		Limit(limit).
		Find(&deliveries).Error
	return deliveries, err
}

func (r *MySQLWebhookDeliveryRepository) Update(ctx context.Context, delivery *domain.WebhookDelivery) error {
	return r.db.WithContext(ctx).Save(delivery).Error
}
