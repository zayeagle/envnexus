package command

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type ApprovalPolicyService struct {
	repo repository.ApprovalPolicyRepository
}

func NewApprovalPolicyService(repo repository.ApprovalPolicyRepository) *ApprovalPolicyService {
	return &ApprovalPolicyService{repo: repo}
}

func (s *ApprovalPolicyService) Create(ctx context.Context, tenantID string, req dto.CreateApprovalPolicyRequest) (*dto.ApprovalPolicyResponse, error) {
	rule := domain.ApprovalRuleSingle
	if req.ApprovalRule != "" {
		rule = domain.ApprovalRule(req.ApprovalRule)
	}
	expiresMin := req.ExpiresMinutes
	if expiresMin <= 0 {
		expiresMin = 30
	}
	policy := &domain.ApprovalPolicy{
		ID:               ulid.Make().String(),
		TenantID:         tenantID,
		Name:             req.Name,
		RiskLevel:        req.RiskLevel,
		AutoApprove:      req.AutoApprove,
		ApprovalRule:     rule,
		SeparationOfDuty: req.SeparationOfDuty,
		ExpiresMinutes:   expiresMin,
		Priority:         req.Priority,
		Status:           "active",
		Version:          1,
	}
	if req.ApproverUserID != "" {
		policy.ApproverUserID = &req.ApproverUserID
	}
	if req.ApproverRoleID != "" {
		policy.ApproverRoleID = &req.ApproverRoleID
	}
	if err := s.repo.Create(ctx, policy); err != nil {
		return nil, err
	}
	return policyToResponse(policy), nil
}

func (s *ApprovalPolicyService) GetByID(ctx context.Context, id string) (*dto.ApprovalPolicyResponse, error) {
	policy, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, domain.ErrApprovalPolicyNotFound
	}
	return policyToResponse(policy), nil
}

func (s *ApprovalPolicyService) Update(ctx context.Context, id string, req dto.UpdateApprovalPolicyRequest) (*dto.ApprovalPolicyResponse, error) {
	policy, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, domain.ErrApprovalPolicyNotFound
	}
	if req.Name != nil {
		policy.Name = *req.Name
	}
	if req.ApproverUserID != nil {
		policy.ApproverUserID = req.ApproverUserID
	}
	if req.ApproverRoleID != nil {
		policy.ApproverRoleID = req.ApproverRoleID
	}
	if req.AutoApprove != nil {
		policy.AutoApprove = *req.AutoApprove
	}
	if req.ApprovalRule != nil {
		policy.ApprovalRule = domain.ApprovalRule(*req.ApprovalRule)
	}
	if req.SeparationOfDuty != nil {
		policy.SeparationOfDuty = *req.SeparationOfDuty
	}
	if req.ExpiresMinutes != nil {
		policy.ExpiresMinutes = *req.ExpiresMinutes
	}
	if req.Priority != nil {
		policy.Priority = *req.Priority
	}
	if req.Status != nil {
		policy.Status = *req.Status
	}
	policy.Version++
	if err := s.repo.Update(ctx, policy); err != nil {
		return nil, err
	}
	return policyToResponse(policy), nil
}

func (s *ApprovalPolicyService) Delete(ctx context.Context, id string) error {
	policy, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if policy == nil {
		return domain.ErrApprovalPolicyNotFound
	}
	return s.repo.Delete(ctx, id)
}

func (s *ApprovalPolicyService) List(ctx context.Context, tenantID string) ([]dto.ApprovalPolicyResponse, error) {
	policies, err := s.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.ApprovalPolicyResponse, 0, len(policies))
	for _, p := range policies {
		result = append(result, *policyToResponse(p))
	}
	return result, nil
}

func (s *ApprovalPolicyService) FindPolicy(ctx context.Context, tenantID, riskLevel string) (*domain.ApprovalPolicy, error) {
	return s.repo.FindByTenantAndRisk(ctx, tenantID, riskLevel)
}

func policyToResponse(p *domain.ApprovalPolicy) *dto.ApprovalPolicyResponse {
	return &dto.ApprovalPolicyResponse{
		ID:               p.ID,
		TenantID:         p.TenantID,
		Name:             p.Name,
		RiskLevel:        p.RiskLevel,
		ApproverUserID:   p.ApproverUserID,
		ApproverRoleID:   p.ApproverRoleID,
		AutoApprove:      p.AutoApprove,
		ApprovalRule:     string(p.ApprovalRule),
		SeparationOfDuty: p.SeparationOfDuty,
		ExpiresMinutes:   p.ExpiresMinutes,
		Status:           p.Status,
		Priority:         p.Priority,
		Version:          p.Version,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}
