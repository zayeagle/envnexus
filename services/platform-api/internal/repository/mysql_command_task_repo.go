package repository

import (
	"context"
	"errors"
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type MySQLCommandTaskRepository struct {
	db *gorm.DB
}

func NewMySQLCommandTaskRepository(db *gorm.DB) *MySQLCommandTaskRepository {
	return &MySQLCommandTaskRepository{db: db}
}

func (r *MySQLCommandTaskRepository) Create(ctx context.Context, task *domain.CommandTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *MySQLCommandTaskRepository) GetByID(ctx context.Context, id string) (*domain.CommandTask, error) {
	var task domain.CommandTask
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func (r *MySQLCommandTaskRepository) Update(ctx context.Context, task *domain.CommandTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *MySQLCommandTaskRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.CommandTask{}, "id = ?", id).Error
}

func (r *MySQLCommandTaskRepository) ListByTenant(ctx context.Context, tenantID string, filters CommandTaskFilters, limit, offset int) ([]*domain.CommandTask, int64, error) {
	query := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if !filters.IncludeArchived {
		query = query.Where("archived_at IS NULL")
	}
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.CreatedBy != "" {
		query = query.Where("created_by_user_id = ?", filters.CreatedBy)
	}
	if filters.ApproverID != "" {
		query = query.Where("approver_user_id = ?", filters.ApproverID)
	}
	if filters.RiskLevel != "" {
		query = query.Where("risk_level = ?", filters.RiskLevel)
	}
	var total int64
	query.Model(&domain.CommandTask{}).Count(&total)
	var tasks []*domain.CommandTask
	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&tasks).Error
	return tasks, total, err
}

func (r *MySQLCommandTaskRepository) ListPendingByApprover(ctx context.Context, tenantID, approverUserID string) ([]*domain.CommandTask, error) {
	var tasks []*domain.CommandTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND approver_user_id = ? AND status = ? AND archived_at IS NULL", tenantID, approverUserID, domain.CommandTaskPendingApproval).
		Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (r *MySQLCommandTaskRepository) ListPendingByApproverRole(ctx context.Context, tenantID, roleID string) ([]*domain.CommandTask, error) {
	var tasks []*domain.CommandTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND approver_role_id = ? AND status = ? AND archived_at IS NULL", tenantID, roleID, domain.CommandTaskPendingApproval).
		Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (r *MySQLCommandTaskRepository) ListPendingInTenant(ctx context.Context, tenantID string) ([]*domain.CommandTask, error) {
	var tasks []*domain.CommandTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND archived_at IS NULL", tenantID, domain.CommandTaskPendingApproval).
		Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (r *MySQLCommandTaskRepository) CountPendingInTenant(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.CommandTask{}).
		Where("tenant_id = ? AND status = ? AND archived_at IS NULL", tenantID, domain.CommandTaskPendingApproval).
		Count(&count).Error
	return count, err
}

func (r *MySQLCommandTaskRepository) CountPendingByApprover(ctx context.Context, tenantID, approverUserID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.CommandTask{}).
		Where("tenant_id = ? AND approver_user_id = ? AND status = ? AND archived_at IS NULL", tenantID, approverUserID, domain.CommandTaskPendingApproval).
		Count(&count).Error
	return count, err
}

func (r *MySQLCommandTaskRepository) ListExpired(ctx context.Context) ([]*domain.CommandTask, error) {
	var tasks []*domain.CommandTask
	err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at < ? AND archived_at IS NULL", domain.CommandTaskPendingApproval, time.Now()).
		Find(&tasks).Error
	return tasks, err
}
