package repository

import (
	"context"
	"errors"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"gorm.io/gorm"
)

type MySQLCommandExecutionRepository struct {
	db *gorm.DB
}

func NewMySQLCommandExecutionRepository(db *gorm.DB) *MySQLCommandExecutionRepository {
	return &MySQLCommandExecutionRepository{db: db}
}

func (r *MySQLCommandExecutionRepository) Create(ctx context.Context, exec *domain.CommandExecution) error {
	return r.db.WithContext(ctx).Create(exec).Error
}

func (r *MySQLCommandExecutionRepository) GetByID(ctx context.Context, id string) (*domain.CommandExecution, error) {
	var exec domain.CommandExecution
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&exec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &exec, nil
}

func (r *MySQLCommandExecutionRepository) GetByTaskAndDevice(ctx context.Context, taskID, deviceID string) (*domain.CommandExecution, error) {
	var exec domain.CommandExecution
	err := r.db.WithContext(ctx).Where("task_id = ? AND device_id = ?", taskID, deviceID).First(&exec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &exec, nil
}

func (r *MySQLCommandExecutionRepository) Update(ctx context.Context, exec *domain.CommandExecution) error {
	return r.db.WithContext(ctx).Save(exec).Error
}

func (r *MySQLCommandExecutionRepository) ListByTaskID(ctx context.Context, taskID string) ([]*domain.CommandExecution, error) {
	var execs []*domain.CommandExecution
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at ASC").Find(&execs).Error
	return execs, err
}

func (r *MySQLCommandExecutionRepository) DeleteByTaskID(ctx context.Context, taskID string) error {
	return r.db.WithContext(ctx).Where("task_id = ?", taskID).Delete(&domain.CommandExecution{}).Error
}
