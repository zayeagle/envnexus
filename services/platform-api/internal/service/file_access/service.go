package file_access

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/infrastructure"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type ApprovalPolicyFinder interface {
	FindByTenantAndRisk(ctx context.Context, tenantID, riskLevel string) (*domain.ApprovalPolicy, error)
}

type RBACChecker interface {
	UserHasRoleInTenant(ctx context.Context, tenantID, userID, roleID string) (bool, error)
}

type Service struct {
	repo          repository.FileAccessRepository
	auditRepo     repository.AuditRepository
	gatewayClient *infrastructure.GatewayClient
	minioClient   *infrastructure.MinIOClient
	policyRepo    ApprovalPolicyFinder
	rbacChecker   RBACChecker
}

func NewService(repo repository.FileAccessRepository, auditRepo repository.AuditRepository, gatewayClient *infrastructure.GatewayClient, minioClient *infrastructure.MinIOClient, policyRepo ApprovalPolicyFinder, rbacChecker RBACChecker) *Service {
	return &Service{repo: repo, auditRepo: auditRepo, gatewayClient: gatewayClient, minioClient: minioClient, policyRepo: policyRepo, rbacChecker: rbacChecker}
}

func (s *Service) CreateRequest(ctx context.Context, tenantID, deviceID, requestedBy, path string, action domain.FileAccessAction, note string) (*domain.FileAccessRequest, error) {
	req := &domain.FileAccessRequest{
		ID:          ulid.Make().String(),
		TenantID:    tenantID,
		DeviceID:    deviceID,
		RequestedBy: requestedBy,
		Path:        path,
		Action:      action,
		Status:      domain.FileAccessPending,
		Note:        note,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	// Auto-approve browse and preview: low-risk read-only operations
	if action == domain.FileAccessBrowse || action == domain.FileAccessPreview {
		now := time.Now()
		req.Status = domain.FileAccessApproved
		req.ApprovedBy = &requestedBy
		req.ResolvedAt = &now
	}

	// Download requires approval — look up L1 policy
	if action == domain.FileAccessDownload && s.policyRepo != nil {
		policy, _ := s.policyRepo.FindByTenantAndRisk(ctx, tenantID, "L1")
		expiresMinutes := 30
		if policy != nil {
			req.PolicySnapshotID = &policy.ID
			if policy.ExpiresMinutes > 0 {
				expiresMinutes = policy.ExpiresMinutes
			}
			if policy.AutoApprove {
				now := time.Now()
				req.Status = domain.FileAccessApproved
				req.ApprovedBy = &requestedBy
				req.ResolvedAt = &now
			} else {
				req.ApproverUserID = policy.ApproverUserID
				req.ApproverRoleID = policy.ApproverRoleID
			}
		}
		req.ExpiresAt = time.Now().Add(time.Duration(expiresMinutes) * time.Minute)
	}

	if err := s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	if req.Status == domain.FileAccessApproved {
		go s.dispatchToDevice(context.Background(), req)
	}

	return req, nil
}

func (s *Service) Approve(ctx context.Context, id, approverID string, isPlatformSuperAdmin bool) (*domain.FileAccessRequest, error) {
	req, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	if req.Status != domain.FileAccessPending {
		return nil, domain.NewAppError("invalid_state", "request is not pending", 409)
	}
	if time.Now().After(req.ExpiresAt) {
		req.Status = domain.FileAccessExpired
		_ = s.repo.Update(ctx, req)
		return nil, domain.NewAppError("expired", "file access request has expired", 409)
	}

	if !isPlatformSuperAdmin {
		if err := s.canApprove(ctx, req, approverID); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	req.Status = domain.FileAccessApproved
	req.ApprovedBy = &approverID
	req.ResolvedAt = &now
	if err := s.repo.Update(ctx, req); err != nil {
		return nil, err
	}

	go s.dispatchToDevice(context.Background(), req)

	return req, nil
}

func (s *Service) canApprove(ctx context.Context, req *domain.FileAccessRequest, approverID string) error {
	if req.ApproverUserID != nil && *req.ApproverUserID == approverID {
		return nil
	}
	if req.ApproverRoleID != nil && *req.ApproverRoleID != "" && s.rbacChecker != nil {
		ok, err := s.rbacChecker.UserHasRoleInTenant(ctx, req.TenantID, approverID, *req.ApproverRoleID)
		if err == nil && ok {
			return nil
		}
	}
	// If no specific approver is assigned, anyone with approve permission can approve
	if req.ApproverUserID == nil && req.ApproverRoleID == nil {
		return nil
	}
	return domain.ErrInsufficientPermission
}

func (s *Service) dispatchToDevice(ctx context.Context, req *domain.FileAccessRequest) {
	if s.gatewayClient == nil {
		slog.Warn("[file_access] gateway client not configured, cannot dispatch")
		return
	}

	payload := map[string]interface{}{
		"request_id": req.ID,
		"path":       req.Path,
		"action":     string(req.Action),
	}

	if req.Action == domain.FileAccessDownload && s.minioClient != nil {
		objectKey := fmt.Sprintf("file-access/%s/%s", req.TenantID, req.ID)
		uploadURL, err := s.minioClient.PresignedPutURL(ctx, objectKey, 30*time.Minute)
		if err != nil {
			slog.Error("[file_access] Failed to generate presigned upload URL",
				"request_id", req.ID,
				"error", err,
			)
			return
		}
		filename := filepath.Base(req.Path)
		downloadURL, err := s.minioClient.PresignedDownloadURL(ctx, objectKey, 24*time.Hour, filename)
		if err != nil {
			slog.Error("[file_access] Failed to generate presigned download URL",
				"request_id", req.ID,
				"error", err,
			)
			return
		}
		payload["upload_url"] = uploadURL.String()
		payload["download_url"] = downloadURL.String()
	}

	evt := infrastructure.SessionEvent{
		EventID:   fmt.Sprintf("fa_%s", req.ID),
		EventType: "file.execute",
		TenantID:  req.TenantID,
		DeviceID:  req.DeviceID,
		SessionID: req.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}

	if err := s.gatewayClient.SendToDevice(ctx, req.DeviceID, evt); err != nil {
		slog.Error("[file_access] Failed to dispatch to device",
			"request_id", req.ID,
			"device_id", req.DeviceID,
			"error", err,
		)
	}
}

func (s *Service) Deny(ctx context.Context, id, approverID string) (*domain.FileAccessRequest, error) {
	req, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	if req.Status != domain.FileAccessPending {
		return nil, domain.NewAppError("invalid_state", "request is not pending", 409)
	}
	now := time.Now()
	req.Status = domain.FileAccessDenied
	req.ApprovedBy = &approverID
	req.ResolvedAt = &now
	if err := s.repo.Update(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*domain.FileAccessRequest, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) ListByTenant(ctx context.Context, tenantID, status string) ([]*domain.FileAccessRequest, error) {
	return s.repo.ListByTenant(ctx, tenantID, status)
}

func (s *Service) ListPending(ctx context.Context, tenantID string) ([]*domain.FileAccessRequest, error) {
	return s.repo.ListByTenant(ctx, tenantID, string(domain.FileAccessPending))
}

func (s *Service) ClearHistory(ctx context.Context, tenantID string) (int64, error) {
	return s.repo.DeleteCompletedByTenant(ctx, tenantID)
}

func (s *Service) SetResult(ctx context.Context, id, resultJSON string) error {
	req, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.ErrNotFound
	}
	req.ResultJSON = resultJSON
	return s.repo.Update(ctx, req)
}
