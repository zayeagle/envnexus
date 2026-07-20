package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type ToolInvocationRepository interface {
	Create(ctx context.Context, inv *domain.ToolInvocation) error
	Update(ctx context.Context, inv *domain.ToolInvocation) error
	ListBySession(ctx context.Context, sessionID string) ([]*domain.ToolInvocation, error)
	ListByDevice(ctx context.Context, deviceID string) ([]*domain.ToolInvocation, error)
}

type MySQLToolInvocationRepository struct {
	db *gorm.DB
}

func NewMySQLToolInvocationRepository(db *gorm.DB) *MySQLToolInvocationRepository {
	return &MySQLToolInvocationRepository{db: db}
}

func (r *MySQLToolInvocationRepository) Create(ctx context.Context, inv *domain.ToolInvocation) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

func (r *MySQLToolInvocationRepository) Update(ctx context.Context, inv *domain.ToolInvocation) error {
	return r.db.WithContext(ctx).Save(inv).Error
}

func (r *MySQLToolInvocationRepository) ListBySession(ctx context.Context, sessionID string) ([]*domain.ToolInvocation, error) {
	var invocations []*domain.ToolInvocation
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("started_at ASC").Find(&invocations).Error
	return invocations, err
}

func (r *MySQLToolInvocationRepository) ListByDevice(ctx context.Context, deviceID string) ([]*domain.ToolInvocation, error) {
	var invocations []*domain.ToolInvocation
	err := r.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("created_at DESC").Limit(100).Find(&invocations).Error
	return invocations, err
}
