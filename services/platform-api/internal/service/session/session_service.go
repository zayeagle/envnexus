package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/auth"
)

type GatewayNotifier interface {
	NotifySessionCreated(ctx context.Context, tenantID, deviceID, sessionID string) error
}

type Service struct {
	sessionRepo     repository.SessionRepository
	approvalRepo    repository.ApprovalRequestRepository
	deviceRepo      repository.DeviceRepository
	auditRepo       repository.AuditRepository
	toolInvRepo     repository.ToolInvocationRepository
	authService     *auth.Service
	gatewayNotifier GatewayNotifier
}

func NewService(
	sessionRepo repository.SessionRepository,
	approvalRepo repository.ApprovalRequestRepository,
	deviceRepo repository.DeviceRepository,
	auditRepo repository.AuditRepository,
	authService *auth.Service,
	gatewayNotifier GatewayNotifier,
) *Service {
	return &Service{
		sessionRepo:     sessionRepo,
		approvalRepo:    approvalRepo,
		deviceRepo:      deviceRepo,
		auditRepo:       auditRepo,
		authService:     authService,
		gatewayNotifier: gatewayNotifier,
	}
}

// SetToolInvocationRepository injects the tool invocation repository (optional dependency).
func (s *Service) SetToolInvocationRepository(repo repository.ToolInvocationRepository) {
	s.toolInvRepo = repo
}

func (s *Service) ListByTenant(ctx context.Context, tenantID string, page, pageSize int) ([]*domain.Session, int64, error) {
	return s.sessionRepo.ListByTenant(ctx, tenantID, page, pageSize)
}

func (s *Service) CountActiveByTenant(ctx context.Context, tenantID string) (int64, error) {
	return s.sessionRepo.CountActiveByTenant(ctx, tenantID)
}

type CreateSessionResult struct {
	Session *domain.Session
	WSToken string
}

func (s *Service) CreateSession(ctx context.Context, req dto.CreateSessionRequest) (*CreateSessionResult, error) {
	device, err := s.deviceRepo.GetByID(ctx, req.DeviceID)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if device == nil {
		return nil, domain.ErrDeviceNotFound
	}
	if device.IsRevoked() {
		return nil, domain.ErrDeviceRevoked
	}

	now := time.Now()
	sess := &domain.Session{
		ID:            ulid.Make().String(),
		TenantID:      device.TenantID,
		DeviceID:      req.DeviceID,
		Transport:     req.Transport,
		Status:        domain.SessionStatusCreated,
		InitiatorType: req.InitiatorType,
		StartedAt:     now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.sessionRepo.Create(ctx, sess); err != nil {
		return nil, domain.ErrInternalError
	}

	s.recordAudit(ctx, sess.TenantID, sess.DeviceID, sess.ID, "session.created", map[string]interface{}{
		"transport":      req.Transport,
		"initiator_type": req.InitiatorType,
	})

	if s.gatewayNotifier != nil {
		if err := s.gatewayNotifier.NotifySessionCreated(ctx, sess.TenantID, sess.DeviceID, sess.ID); err != nil {
			slog.Warn("failed to notify gateway of session creation", "session_id", sess.ID, "error", err)
		}
	}

	wsToken, err := s.authService.IssueSessionToken(sess.DeviceID, sess.TenantID, sess.ID)
	if err != nil {
		return nil, domain.ErrInternalError
	}

	return &CreateSessionResult{
		Session: sess,
		WSToken: wsToken,
	}, nil
}

func (s *Service) GetByID(ctx context.Context, sessionID string) (*domain.Session, error) {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	if session == nil {
		return nil, domain.ErrSessionNotFound
	}
	return session, nil
}

func (s *Service) ApproveSession(ctx context.Context, sessionID string, approvalRequestID string, approverUserID string, comment string) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil || session == nil {
		return domain.ErrSessionNotFound
	}

	approval, err := s.approvalRepo.GetByID(ctx, approvalRequestID)
	if err != nil || approval == nil {
		return domain.ErrApprovalNotFound
	}
	if approval.SessionID != sessionID {
		return domain.ErrApprovalInvalidState
	}
	if approval.IsExpired() {
		approval.Status = domain.ApprovalStatusExpired
		_ = s.approvalRepo.Update(ctx, approval)
		return domain.ErrApprovalExpired
	}
	if !approval.CanApprove() {
		return domain.ErrApprovalInvalidState
	}

	now := time.Now()
	approval.Status = domain.ApprovalStatusApproved
	approval.ApproverUserID = &approverUserID
	approval.ApprovedAt = &now
	approval.UpdatedAt = now

	if err := s.approvalRepo.Update(ctx, approval); err != nil {
		return domain.ErrInternalError
	}

	s.recordAudit(ctx, session.TenantID, session.DeviceID, sessionID, "approval.approved", map[string]interface{}{
		"approval_id": approvalRequestID,
		"approver":    approverUserID,
	})

	return nil
}

func (s *Service) DenySession(ctx context.Context, sessionID string, approvalRequestID string, reason string) error {
	approval, err := s.approvalRepo.GetByID(ctx, approvalRequestID)
	if err != nil || approval == nil {
		return domain.ErrApprovalNotFound
	}
	if approval.SessionID != sessionID {
		return domain.ErrApprovalInvalidState
	}
	if !approval.CanDeny() {
		return domain.ErrApprovalInvalidState
	}

	approval.Status = domain.ApprovalStatusDenied
	approval.UpdatedAt = time.Now()

	if err := s.approvalRepo.Update(ctx, approval); err != nil {
		return domain.ErrInternalError
	}

	session, _ := s.sessionRepo.GetByID(ctx, sessionID)
	if session != nil {
		s.recordAudit(ctx, session.TenantID, session.DeviceID, sessionID, "approval.denied", map[string]interface{}{
			"approval_id": approvalRequestID,
			"reason":      reason,
		})
	}

	return nil
}

func (s *Service) AbortSession(ctx context.Context, sessionID string, reason string) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil || session == nil {
		return domain.ErrSessionNotFound
	}

	if !session.TransitionTo(domain.SessionStatusAborted) {
		return domain.ErrSessionInvalidState
	}

	if err := s.sessionRepo.Update(ctx, session); err != nil {
		return domain.ErrInternalError
	}

	s.recordAudit(ctx, session.TenantID, session.DeviceID, sessionID, "session.aborted", map[string]interface{}{
		"reason": reason,
	})

	return nil
}

func (s *Service) BatchDeleteSessions(ctx context.Context, tenantID string, sessionIDs []string) error {
	for _, sessionID := range sessionIDs {
		session, err := s.sessionRepo.GetByID(ctx, sessionID)
		if err != nil || session == nil {
			continue
		}
		if session.TenantID != tenantID {
			continue
		}
		// 这里可以添加删除逻辑，例如将会话标记为已删除或直接从数据库中删除
		// 由于没有明确的删除方法，我们可以通过更新状态来实现
		if session.TransitionTo(domain.SessionStatusAborted) {
			_ = s.sessionRepo.Update(ctx, session)
			s.recordAudit(ctx, session.TenantID, session.DeviceID, sessionID, "session.deleted", nil)
		}
	}
	return nil
}

func (s *Service) TransitionSession(ctx context.Context, sessionID string, target domain.SessionStatus) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil || session == nil {
		return domain.ErrSessionNotFound
	}

	if !session.TransitionTo(target) {
		return domain.ErrSessionInvalidState
	}

	return s.sessionRepo.Update(ctx, session)
}

func (s *Service) CreateApprovalRequest(ctx context.Context, sessionID, deviceID, actionJSON, riskLevel string) (*domain.ApprovalRequest, error) {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil || session == nil {
		return nil, domain.ErrSessionNotFound
	}

	now := time.Now()
	expires := now.Add(10 * time.Minute)

	approval := &domain.ApprovalRequest{
		ID:                  ulid.Make().String(),
		SessionID:           sessionID,
		DeviceID:            deviceID,
		RequestedActionJSON: actionJSON,
		RiskLevel:           riskLevel,
		Status:              domain.ApprovalStatusPendingUser,
		ExpiresAt:           &expires,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.approvalRepo.Create(ctx, approval); err != nil {
		return nil, domain.ErrInternalError
	}

	if session.CanTransitionTo(domain.SessionStatusAwaitingApproval) {
		session.TransitionTo(domain.SessionStatusAwaitingApproval)
		_ = s.sessionRepo.Update(ctx, session)
	}

	return approval, nil
}

func (s *Service) MarkApprovalExecuting(ctx context.Context, approvalID string) error {
	approval, err := s.approvalRepo.GetByID(ctx, approvalID)
	if err != nil || approval == nil {
		return domain.ErrApprovalNotFound
	}
	if approval.Status != domain.ApprovalStatusApproved {
		return domain.ErrApprovalInvalidState
	}

	now := time.Now()
	approval.Status = domain.ApprovalStatusExecuting
	approval.ExecutedAt = &now
	approval.UpdatedAt = now
	return s.approvalRepo.Update(ctx, approval)
}

func (s *Service) MarkApprovalSucceeded(ctx context.Context, approvalID string) error {
	approval, err := s.approvalRepo.GetByID(ctx, approvalID)
	if err != nil || approval == nil {
		return domain.ErrApprovalNotFound
	}
	if approval.Status != domain.ApprovalStatusExecuting {
		return domain.ErrApprovalInvalidState
	}

	approval.Status = domain.ApprovalStatusSucceeded
	approval.UpdatedAt = time.Now()
	return s.approvalRepo.Update(ctx, approval)
}

func (s *Service) MarkApprovalFailed(ctx context.Context, approvalID string) error {
	approval, err := s.approvalRepo.GetByID(ctx, approvalID)
	if err != nil || approval == nil {
		return domain.ErrApprovalNotFound
	}
	if approval.Status != domain.ApprovalStatusExecuting {
		return domain.ErrApprovalInvalidState
	}

	approval.Status = domain.ApprovalStatusFailed
	approval.UpdatedAt = time.Now()
	return s.approvalRepo.Update(ctx, approval)
}

func (s *Service) MarkApprovalRolledBack(ctx context.Context, approvalID string) error {
	approval, err := s.approvalRepo.GetByID(ctx, approvalID)
	if err != nil || approval == nil {
		return domain.ErrApprovalNotFound
	}
	if approval.Status != domain.ApprovalStatusFailed {
		return domain.ErrApprovalInvalidState
	}

	approval.Status = domain.ApprovalStatusRolledBack
	approval.UpdatedAt = time.Now()
	return s.approvalRepo.Update(ctx, approval)
}

// ListToolInvocations returns tool invocations for a session.
func (s *Service) ListToolInvocations(ctx context.Context, sessionID string) ([]*domain.ToolInvocation, error) {
	if s.toolInvRepo == nil {
		return []*domain.ToolInvocation{}, nil
	}
	invocations, err := s.toolInvRepo.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, domain.ErrInternalError
	}
	return invocations, nil
}

func (s *Service) GetApprovalByID(ctx context.Context, approvalID string) (*domain.ApprovalRequest, error) {
	approval, err := s.approvalRepo.GetByID(ctx, approvalID)
	if err != nil || approval == nil {
		return nil, domain.ErrApprovalNotFound
	}
	return approval, nil
}

func (s *Service) GetPendingApproval(ctx context.Context, sessionID string) (*domain.ApprovalRequest, error) {
	return s.approvalRepo.GetPendingBySession(ctx, sessionID)
}

func (s *Service) recordAudit(ctx context.Context, tenantID, deviceID, sessionID, eventType string, details interface{}) {
	if s.auditRepo == nil {
		return
	}

	var payloadJSON string
	if details != nil {
		data, err := json.Marshal(details)
		if err != nil {
			slog.Warn("failed to marshal audit payload", "event_type", eventType, "error", err)
		} else {
			payloadJSON = string(data)
		}
	}

	if err := s.auditRepo.Create(ctx, &domain.AuditEvent{
		ID:               ulid.Make().String(),
		TenantID:         tenantID,
		DeviceID:         &deviceID,
		SessionID:        &sessionID,
		EventType:        eventType,
		EventPayloadJSON: payloadJSON,
		CreatedAt:        time.Now(),
	}); err != nil {
		slog.Error("failed to write audit event", "event_type", eventType, "session_id", sessionID, "error", err)
	}
}
