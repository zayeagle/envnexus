package repository

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type CommandExecutionRepository interface {
	Create(ctx context.Context, exec *domain.CommandExecution) error
	GetByID(ctx context.Context, id string) (*domain.CommandExecution, error)
	GetByTaskAndDevice(ctx context.Context, taskID, deviceID string) (*domain.CommandExecution, error)
	Update(ctx context.Context, exec *domain.CommandExecution) error
	ListByTaskID(ctx context.Context, taskID string) ([]*domain.CommandExecution, error)
	DeleteByTaskID(ctx context.Context, taskID string) error
}
