package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type UserNotificationChannelRepository interface {
	Create(ctx context.Context, channel *domain.UserNotificationChannel) error
	GetByID(ctx context.Context, id string) (*domain.UserNotificationChannel, error)
	Delete(ctx context.Context, id string) error
	ListByUserID(ctx context.Context, userID string) ([]*domain.UserNotificationChannel, error)
	ListActiveByUserID(ctx context.Context, userID string) ([]*domain.UserNotificationChannel, error)
}
