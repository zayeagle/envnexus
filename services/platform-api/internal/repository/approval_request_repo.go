package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type ApprovalRequestRepository interface {
	Create(ctx context.Context, req *domain.ApprovalRequest) error
	GetByID(ctx context.Context, id string) (*domain.ApprovalRequest, error)
	Update(ctx context.Context, req *domain.ApprovalRequest) error
	ListBySession(ctx context.Context, sessionID string) ([]*domain.ApprovalRequest, error)
	ListByDevice(ctx context.Context, deviceID string) ([]*domain.ApprovalRequest, error)
	GetPendingBySession(ctx context.Context, sessionID string) (*domain.ApprovalRequest, error)
}

type MySQLApprovalRequestRepository struct {
	db *gorm.DB
}

func NewMySQLApprovalRequestRepository(db *gorm.DB) *MySQLApprovalRequestRepository {
	return &MySQLApprovalRequestRepository{db: db}
}

func (r *MySQLApprovalRequestRepository) Create(ctx context.Context, req *domain.ApprovalRequest) error {
	return r.db.WithContext(ctx).Create(req).Error
}

func (r *MySQLApprovalRequestRepository) GetByID(ctx context.Context, id string) (*domain.ApprovalRequest, error) {
	var req domain.ApprovalRequest
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&req).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &req, nil
}

func (r *MySQLApprovalRequestRepository) Update(ctx context.Context, req *domain.ApprovalRequest) error {
	return r.db.WithContext(ctx).Save(req).Error
}

func (r *MySQLApprovalRequestRepository) ListBySession(ctx context.Context, sessionID string) ([]*domain.ApprovalRequest, error) {
	var reqs []*domain.ApprovalRequest
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("created_at DESC").Find(&reqs).Error
	return reqs, err
}

func (r *MySQLApprovalRequestRepository) ListByDevice(ctx context.Context, deviceID string) ([]*domain.ApprovalRequest, error) {
	var reqs []*domain.ApprovalRequest
	err := r.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("created_at DESC").Find(&reqs).Error
	return reqs, err
}

func (r *MySQLApprovalRequestRepository) GetPendingBySession(ctx context.Context, sessionID string) (*domain.ApprovalRequest, error) {
	var req domain.ApprovalRequest
	err := r.db.WithContext(ctx).Where("session_id = ? AND status = ?", sessionID, domain.ApprovalStatusPendingUser).First(&req).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &req, nil
}
