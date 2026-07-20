package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

const (
	maxAttempts    = 5
	requestTimeout = 10 * time.Second
)

type Service struct {
	subRepo      repository.WebhookSubscriptionRepository
	deliveryRepo repository.WebhookDeliveryRepository
}

func NewService(
	subRepo repository.WebhookSubscriptionRepository,
	deliveryRepo repository.WebhookDeliveryRepository,
) *Service {
	return &Service{subRepo: subRepo, deliveryRepo: deliveryRepo}
}

// CreateSubscription creates a new webhook subscription.
func (s *Service) CreateSubscription(ctx context.Context, tenantID, name, url, secret string, eventTypes []string) (*domain.WebhookSubscription, error) {
	typesJSON, _ := json.Marshal(eventTypes)
	sub := &domain.WebhookSubscription{
		ID:             ulid.Make().String(),
		TenantID:       tenantID,
		Name:           name,
		URL:            url,
		Secret:         secret,
		EventTypesJSON: string(typesJSON),
		Status:         "active",
	}
	if err := s.subRepo.Create(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// ListSubscriptions returns all subscriptions for a tenant.
func (s *Service) ListSubscriptions(ctx context.Context, tenantID string) ([]*domain.WebhookSubscription, error) {
	return s.subRepo.ListByTenant(ctx, tenantID)
}

// DeleteSubscription removes a subscription.
func (s *Service) DeleteSubscription(ctx context.Context, id string) error {
	return s.subRepo.Delete(ctx, id)
}

// Dispatch fans out an event to all matching subscriptions and queues deliveries.
func (s *Service) Dispatch(ctx context.Context, tenantID, eventType string, payload interface{}) error {
	subs, err := s.subRepo.FindByEventType(ctx, tenantID, eventType)
	if err != nil {
		return fmt.Errorf("find subscriptions: %w", err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	idempotencyBase := fmt.Sprintf("%s-%s-%d", tenantID, eventType, time.Now().UnixNano())

	for _, sub := range subs {
		delivery := &domain.WebhookDelivery{
			ID:             ulid.Make().String(),
			SubscriptionID: sub.ID,
			TenantID:       tenantID,
			EventType:      eventType,
			PayloadJSON:    string(payloadJSON),
			IdempotencyKey: idempotencyBase + "-" + sub.ID,
			Status:         "pending",
			AttemptCount:   0,
		}
		if err := s.deliveryRepo.Create(ctx, delivery); err != nil {
			slog.Error("Failed to create webhook delivery", "sub_id", sub.ID, "error", err)
			continue
		}

		// Attempt delivery immediately in a goroutine
		go s.deliver(context.Background(), sub, delivery)
	}
	return nil
}

// RetryPending retries all pending/failed deliveries (called by job-runner).
func (s *Service) RetryPending(ctx context.Context) {
	deliveries, err := s.deliveryRepo.ListPendingRetries(ctx, 50)
	if err != nil {
		slog.Error("RetryPending: list deliveries", "error", err)
		return
	}
	for _, d := range deliveries {
		sub, err := s.subRepo.GetByID(ctx, d.SubscriptionID)
		if err != nil || sub == nil {
			continue
		}
		go s.deliver(context.Background(), sub, d)
	}
}

// deliver performs the actual HTTP POST.
func (s *Service) deliver(ctx context.Context, sub *domain.WebhookSubscription, delivery *domain.WebhookDelivery) {
	delivery.AttemptCount++

	sig := computeHMAC(sub.Secret, []byte(delivery.PayloadJSON))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewBufferString(delivery.PayloadJSON))
	if err != nil {
		s.markFailed(ctx, delivery, -1, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EnvNexus-Event", delivery.EventType)
	req.Header.Set("X-EnvNexus-Signature-256", "sha256="+sig)
	req.Header.Set("X-EnvNexus-Delivery", delivery.ID)

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		s.markFailed(ctx, delivery, -1, err.Error())
		return
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	delivery.HTTPStatus = &status

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		now := time.Now()
		delivery.Status = "delivered"
		delivery.DeliveredAt = &now
	} else {
		body := fmt.Sprintf("HTTP %d", resp.StatusCode)
		s.markFailed(ctx, delivery, resp.StatusCode, body)
		return
	}

	_ = s.deliveryRepo.Update(ctx, delivery)
	slog.Info("Webhook delivered", "delivery_id", delivery.ID, "url", sub.URL, "event", delivery.EventType)
}

func (s *Service) markFailed(ctx context.Context, delivery *domain.WebhookDelivery, httpStatus int, errMsg string) {
	if httpStatus > 0 {
		delivery.HTTPStatus = &httpStatus
	}
	resp := errMsg
	delivery.ResponseBody = &resp

	if delivery.AttemptCount >= maxAttempts {
		delivery.Status = "failed"
		delivery.NextRetryAt = nil
	} else {
		delivery.Status = "pending"
		backoff := time.Duration(delivery.AttemptCount*delivery.AttemptCount) * time.Minute
		next := time.Now().Add(backoff)
		delivery.NextRetryAt = &next
	}

	_ = s.deliveryRepo.Update(ctx, delivery)
	slog.Warn("Webhook delivery failed", "delivery_id", delivery.ID, "attempt", delivery.AttemptCount, "error", errMsg)
}

func computeHMAC(secret string, data []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
