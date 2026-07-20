package device_group

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Service struct {
	repo repository.DeviceGroupRepository
}

func NewService(repo repository.DeviceGroupRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateGroup(ctx context.Context, tenantID, name, description, filterJSON, createdBy string) (*domain.DeviceGroup, error) {
	g := &domain.DeviceGroup{
		ID:          ulid.Make().String(),
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		FilterJSON:  filterJSON,
		CreatedBy:   createdBy,
	}
	if err := s.repo.CreateGroup(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) GetGroup(ctx context.Context, id string) (*domain.DeviceGroup, error) {
	return s.repo.GetGroup(ctx, id)
}

func (s *Service) ListGroups(ctx context.Context, tenantID string, page, pageSize int) ([]*domain.DeviceGroup, int64, error) {
	return s.repo.ListGroups(ctx, tenantID, page, pageSize)
}

func (s *Service) UpdateGroup(ctx context.Context, id, name, description, filterJSON string) (*domain.DeviceGroup, error) {
	g, err := s.repo.GetGroup(ctx, id)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	if name != "" {
		g.Name = name
	}
	if description != "" {
		g.Description = description
	}
	if filterJSON != "" {
		g.FilterJSON = filterJSON
	}
	g.UpdatedAt = time.Now()
	if err := s.repo.UpdateGroup(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) DeleteGroup(ctx context.Context, id string) error {
	return s.repo.DeleteGroup(ctx, id)
}

func (s *Service) AddMembers(ctx context.Context, groupID string, deviceIDs []string) error {
	members := make([]*domain.DeviceGroupMember, 0, len(deviceIDs))
	for _, did := range deviceIDs {
		members = append(members, &domain.DeviceGroupMember{
			ID:            ulid.Make().String(),
			DeviceGroupID: groupID,
			DeviceID:      did,
		})
	}
	return s.repo.AddMembers(ctx, members)
}

func (s *Service) RemoveMember(ctx context.Context, groupID, deviceID string) error {
	return s.repo.RemoveMember(ctx, groupID, deviceID)
}

func (s *Service) ListMembers(ctx context.Context, groupID string, page, pageSize int) ([]*domain.DeviceGroupMember, int64, error) {
	return s.repo.ListMembers(ctx, groupID, page, pageSize)
}

func (s *Service) CreateBatchTask(ctx context.Context, tenantID, groupID, commandTaskID, strategy, createdBy string, batchSize int) (*domain.BatchTask, error) {
	members, _, err := s.repo.ListMembers(ctx, groupID, 0, 0)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, domain.NewAppError("empty_group", "device group has no members", 400)
	}

	if strategy == "" {
		strategy = "all_at_once"
	}

	bt := &domain.BatchTask{
		ID:            ulid.Make().String(),
		TenantID:      tenantID,
		DeviceGroupID: groupID,
		CommandTaskID: commandTaskID,
		Strategy:      strategy,
		BatchSize:     batchSize,
		TotalDevices:  len(members),
		Status:        "pending",
		CreatedBy:     createdBy,
	}
	if err := s.repo.CreateBatchTask(ctx, bt); err != nil {
		return nil, err
	}
	return bt, nil
}

func (s *Service) GetBatchTask(ctx context.Context, id string) (*domain.BatchTask, error) {
	return s.repo.GetBatchTask(ctx, id)
}

func (s *Service) ListBatchTasks(ctx context.Context, tenantID string, page, pageSize int) ([]*domain.BatchTask, int64, error) {
	return s.repo.ListBatchTasks(ctx, tenantID, page, pageSize)
}

func (s *Service) UpdateBatchTaskProgress(ctx context.Context, id string, completed, failed int) error {
	bt, err := s.repo.GetBatchTask(ctx, id)
	if err != nil {
		return domain.ErrNotFound
	}
	bt.Completed = completed
	bt.Failed = failed
	if completed+failed >= bt.TotalDevices {
		if bt.Failed > 0 {
			bt.Status = "partial_done"
		} else {
			bt.Status = "completed"
		}
	} else {
		bt.Status = "executing"
	}
	bt.UpdatedAt = time.Now()
	return s.repo.UpdateBatchTask(ctx, bt)
}
