package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type SessionRepository interface {
	Create(ctx context.Context, session *domain.Session) error
	GetByID(ctx context.Context, id string) (*domain.Session, error)
	Update(ctx context.Context, session *domain.Session) error
	ListByTenant(ctx context.Context, tenantID string, page, pageSize int) ([]*domain.Session, int64, error)
	ListByDevice(ctx context.Context, deviceID string) ([]*domain.Session, error)
	CountActiveByTenant(ctx context.Context, tenantID string) (int64, error)
}

type MySQLSessionRepository struct {
	db *gorm.DB
}

func NewMySQLSessionRepository(db *gorm.DB) *MySQLSessionRepository {
	return &MySQLSessionRepository{db: db}
}

func (r *MySQLSessionRepository) Create(ctx context.Context, session *domain.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *MySQLSessionRepository) GetByID(ctx context.Context, id string) (*domain.Session, error) {
	var session domain.Session
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (r *MySQLSessionRepository) Update(ctx context.Context, session *domain.Session) error {
	return r.db.WithContext(ctx).Save(session).Error
}

func (r *MySQLSessionRepository) ListByTenant(ctx context.Context, tenantID string, page, pageSize int) ([]*domain.Session, int64, error) {
	var sessions []*domain.Session
	var total int64

	// 计算总数（过滤掉已完成、已中止、已过期的会话）
	err := r.db.WithContext(ctx).Model(&domain.Session{}).
		Where("tenant_id = ? AND status NOT IN ?", tenantID, []string{"completed", "aborted", "expired"}).
		Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询（过滤掉已完成、已中止、已过期的会话）
	offset := (page - 1) * pageSize
	err = r.db.WithContext(ctx).
		Where("tenant_id = ? AND status NOT IN ?", tenantID, []string{"completed", "aborted", "expired"}).
		Order("started_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&sessions).Error
	return sessions, total, err
}

func (r *MySQLSessionRepository) ListByDevice(ctx context.Context, deviceID string) ([]*domain.Session, error) {
	var sessions []*domain.Session
	err := r.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("started_at DESC").Find(&sessions).Error
	return sessions, err
}

func (r *MySQLSessionRepository) CountActiveByTenant(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Session{}).
		Where("tenant_id = ? AND status NOT IN ?", tenantID, []string{"completed", "aborted", "expired"}).
		Count(&count).Error
	return count, err
}
