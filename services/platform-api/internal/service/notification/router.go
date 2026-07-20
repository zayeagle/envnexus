package notification

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Router struct {
	channelRepo  repository.UserNotificationChannelRepository
	providerRepo repository.IMProviderRepository
	notifiers    map[string]Notifier
}

func NewRouter(
	channelRepo repository.UserNotificationChannelRepository,
	providerRepo repository.IMProviderRepository,
) *Router {
	return &Router{
		channelRepo:  channelRepo,
		providerRepo: providerRepo,
		notifiers:    make(map[string]Notifier),
	}
}

func (r *Router) RegisterNotifier(n Notifier) {
	r.notifiers[n.ProviderType()] = n
}

func (r *Router) SendApprovalRequest(ctx context.Context, req ApprovalNotification) error {
	if r.channelRepo == nil {
		return nil
	}
	channels, err := r.channelRepo.ListActiveByUserID(ctx, req.TargetUser)
	if err != nil || len(channels) == 0 {
		slog.Warn("[notification] Approver has no IM channels", "user_id", req.TargetUser)
		return fmt.Errorf("approver has no notification channels configured")
	}
	for _, ch := range channels {
		notifier, ok := r.notifiers[ch.Provider]
		if !ok {
			continue
		}
		if err := notifier.SendApprovalCard(req.TaskID, req.TenantID, ch.ExternalID, req.RequestedBy, req.Title, req.CommandType, req.RiskLevel, req.DeviceCount, req.ExpiresAt); err != nil {
			slog.Error("[notification] Send approval card failed", "provider", ch.Provider, "error", err)
			continue
		}
		return nil
	}
	return fmt.Errorf("all notification channels failed")
}

func (r *Router) SendEmergencyNotice(ctx context.Context, req EmergencyNotification) error {
	if r.channelRepo == nil {
		return nil
	}
	channels, err := r.channelRepo.ListActiveByUserID(ctx, req.TargetUser)
	if err != nil || len(channels) == 0 {
		return nil
	}
	for _, ch := range channels {
		notifier, ok := r.notifiers[ch.Provider]
		if !ok {
			continue
		}
		if err := notifier.SendEmergencyCard(req.TaskID, req.TenantID, ch.ExternalID, req.RequestedBy, req.Title, req.CommandPayload, req.RiskLevel, req.DeviceCount, req.BypassReason); err != nil {
			continue
		}
		return nil
	}
	return nil
}

func (r *Router) SendExecutionResult(ctx context.Context, req ResultNotification) error {
	if r.channelRepo == nil {
		return nil
	}
	channels, err := r.channelRepo.ListActiveByUserID(ctx, req.TargetUser)
	if err != nil || len(channels) == 0 {
		return nil
	}
	for _, ch := range channels {
		notifier, ok := r.notifiers[ch.Provider]
		if !ok {
			continue
		}
		if err := notifier.SendResultCard(req.TaskID, req.TenantID, ch.ExternalID, req.Title, req.Status, req.Succeeded, req.Failed); err != nil {
			continue
		}
		return nil
	}
	return nil
}
