package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type MySQLUserNotificationChannelRepository struct {
	db *gorm.DB
}

func NewMySQLUserNotificationChannelRepository(db *gorm.DB) *MySQLUserNotificationChannelRepository {
	return &MySQLUserNotificationChannelRepository{db: db}
}

func (r *MySQLUserNotificationChannelRepository) Create(ctx context.Context, channel *domain.UserNotificationChannel) error {
	return r.db.WithContext(ctx).Create(channel).Error
}

func (r *MySQLUserNotificationChannelRepository) GetByID(ctx context.Context, id string) (*domain.UserNotificationChannel, error) {
	var channel domain.UserNotificationChannel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&channel).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &channel, nil
}

func (r *MySQLUserNotificationChannelRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.UserNotificationChannel{}, "id = ?", id).Error
}

func (r *MySQLUserNotificationChannelRepository) ListByUserID(ctx context.Context, userID string) ([]*domain.UserNotificationChannel, error) {
	var channels []*domain.UserNotificationChannel
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("priority DESC").Find(&channels).Error
	return channels, err
}

func (r *MySQLUserNotificationChannelRepository) ListActiveByUserID(ctx context.Context, userID string) ([]*domain.UserNotificationChannel, error) {
	var channels []*domain.UserNotificationChannel
	err := r.db.WithContext(ctx).Where("user_id = ? AND status = ? AND verified = ?", userID, "active", true).Order("priority DESC").Find(&channels).Error
	return channels, err
}
