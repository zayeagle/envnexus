package governance

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type RuleService struct {
	ruleRepo repository.GovernanceRuleRepository
	permRepo repository.ToolPermissionRepository
}

func NewRuleService(ruleRepo repository.GovernanceRuleRepository, permRepo repository.ToolPermissionRepository) *RuleService {
	return &RuleService{ruleRepo: ruleRepo, permRepo: permRepo}
}

func (s *RuleService) CreateRule(ctx context.Context, tenantID, name, description, ruleType, conditionJSON, actionJSON, severity, createdBy string) (*domain.GovernanceRule, error) {
	rule := &domain.GovernanceRule{
		ID:            ulid.Make().String(),
		TenantID:      tenantID,
		Name:          name,
		Description:   description,
		RuleType:      ruleType,
		ConditionJSON: conditionJSON,
		ActionJSON:    actionJSON,
		Severity:      severity,
		Enabled:       true,
		CreatedBy:     createdBy,
	}
	if err := s.ruleRepo.Create(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *RuleService) ListRules(ctx context.Context, tenantID string) ([]*domain.GovernanceRule, error) {
	return s.ruleRepo.ListByTenant(ctx, tenantID)
}

func (s *RuleService) GetRule(ctx context.Context, id string) (*domain.GovernanceRule, error) {
	return s.ruleRepo.GetByID(ctx, id)
}

func (s *RuleService) UpdateRule(ctx context.Context, id string, name, description, conditionJSON, actionJSON, severity string, enabled *bool) (*domain.GovernanceRule, error) {
	rule, err := s.ruleRepo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	if name != "" {
		rule.Name = name
	}
	if description != "" {
		rule.Description = description
	}
	if conditionJSON != "" {
		rule.ConditionJSON = conditionJSON
	}
	if actionJSON != "" {
		rule.ActionJSON = actionJSON
	}
	if severity != "" {
		rule.Severity = severity
	}
	if enabled != nil {
		rule.Enabled = *enabled
	}
	rule.UpdatedAt = time.Now()
	if err := s.ruleRepo.Update(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *RuleService) DeleteRule(ctx context.Context, id string) error {
	return s.ruleRepo.Delete(ctx, id)
}

func (s *RuleService) CreateToolPermission(ctx context.Context, tenantID, toolName string, roleID *string, allowed bool, maxRisk string) (*domain.ToolPermission, error) {
	tp := &domain.ToolPermission{
		ID:       ulid.Make().String(),
		TenantID: tenantID,
		ToolName: toolName,
		RoleID:   roleID,
		Allowed:  allowed,
		MaxRisk:  maxRisk,
	}
	if err := s.permRepo.Create(ctx, tp); err != nil {
		return nil, err
	}
	return tp, nil
}

func (s *RuleService) ListToolPermissions(ctx context.Context, tenantID string) ([]*domain.ToolPermission, error) {
	return s.permRepo.ListByTenant(ctx, tenantID)
}

func (s *RuleService) DeleteToolPermission(ctx context.Context, id string) error {
	return s.permRepo.Delete(ctx, id)
}

func (s *RuleService) IsToolAllowed(ctx context.Context, tenantID, toolName string, roleID *string) (bool, error) {
	tp, err := s.permRepo.GetByToolAndRole(ctx, tenantID, toolName, roleID)
	if err != nil {
		return true, nil
	}
	return tp.Allowed, nil
}

func (s *RuleService) UpdateToolPermission(ctx context.Context, id, toolName string, roleID *string, allowed bool, maxRisk string) (*domain.ToolPermission, error) {
	tp, err := s.permRepo.GetByID(ctx, id)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	if toolName != "" {
		tp.ToolName = toolName
	}
	tp.RoleID = roleID
	tp.Allowed = allowed
	if maxRisk != "" {
		tp.MaxRisk = maxRisk
	}
	if err := s.permRepo.Update(ctx, tp); err != nil {
		return nil, err
	}
	return tp, nil
}

func (s *RuleService) GetToolPermission(ctx context.Context, id string) (*domain.ToolPermission, error) {
	return s.permRepo.GetByID(ctx, id)
}
