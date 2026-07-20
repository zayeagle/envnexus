package audit

import (
	"context"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Service struct {
	auditRepo repository.AuditRepository
}

func NewService(auditRepo repository.AuditRepository) *Service {
	return &Service{auditRepo: auditRepo}
}

// BulkCreate stores a batch of audit events received from an agent.
func (s *Service) BulkCreate(ctx context.Context, events []*domain.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	return s.auditRepo.CreateBatch(ctx, events)
}

func (s *Service) ListEvents(ctx context.Context, tenantID string, filters repository.AuditFilters, page, pageSize int) ([]*dto.AuditEventResponse, int64, error) {
	events, total, err := s.auditRepo.ListByTenant(ctx, tenantID, filters, page, pageSize)
	if err != nil {
		return nil, 0, domain.ErrInternalError
	}

	var resp []*dto.AuditEventResponse
	for _, e := range events {
		item := &dto.AuditEventResponse{
			ID:               e.ID,
			TenantID:         e.TenantID,
			EventType:        e.EventType,
			EventPayloadJSON: e.EventPayloadJSON,
			CreatedAt:        e.CreatedAt,
		}
		if e.DeviceID != nil {
			item.DeviceID = *e.DeviceID
		}
		if e.SessionID != nil {
			item.SessionID = *e.SessionID
		}
		resp = append(resp, item)
	}
	return resp, total, nil
}
