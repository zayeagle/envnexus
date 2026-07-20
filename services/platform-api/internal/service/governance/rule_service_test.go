package governance

import (
	"context"
	"testing"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type memRuleRepo struct {
	rules map[string]*domain.GovernanceRule
}

func newMemRuleRepo() *memRuleRepo {
	return &memRuleRepo{rules: make(map[string]*domain.GovernanceRule)}
}
func (r *memRuleRepo) Create(_ context.Context, rule *domain.GovernanceRule) error {
	r.rules[rule.ID] = rule
	return nil
}
func (r *memRuleRepo) GetByID(_ context.Context, id string) (*domain.GovernanceRule, error) {
	rule, ok := r.rules[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return rule, nil
}
func (r *memRuleRepo) ListByTenant(_ context.Context, tenantID string) ([]*domain.GovernanceRule, error) {
	var result []*domain.GovernanceRule
	for _, rule := range r.rules {
		if rule.TenantID == tenantID {
			result = append(result, rule)
		}
	}
	return result, nil
}
func (r *memRuleRepo) Update(_ context.Context, rule *domain.GovernanceRule) error {
	r.rules[rule.ID] = rule
	return nil
}
func (r *memRuleRepo) Delete(_ context.Context, id string) error {
	delete(r.rules, id)
	return nil
}

type memPermRepo struct {
	perms map[string]*domain.ToolPermission
}

// GetByID implements [repository.ToolPermissionRepository].
func (r *memPermRepo) GetByID(ctx context.Context, id string) (*domain.ToolPermission, error) {
	panic("unimplemented")
}

// Update implements [repository.ToolPermissionRepository].
func (r *memPermRepo) Update(ctx context.Context, tp *domain.ToolPermission) error {
	panic("unimplemented")
}

func newMemPermRepo() *memPermRepo {
	return &memPermRepo{perms: make(map[string]*domain.ToolPermission)}
}
func (r *memPermRepo) Create(_ context.Context, tp *domain.ToolPermission) error {
	r.perms[tp.ID] = tp
	return nil
}
func (r *memPermRepo) ListByTenant(_ context.Context, tenantID string) ([]*domain.ToolPermission, error) {
	var result []*domain.ToolPermission
	for _, tp := range r.perms {
		if tp.TenantID == tenantID {
			result = append(result, tp)
		}
	}
	return result, nil
}
func (r *memPermRepo) Delete(_ context.Context, id string) error {
	delete(r.perms, id)
	return nil
}
func (r *memPermRepo) GetByToolAndRole(_ context.Context, tenantID, toolName string, roleID *string) (*domain.ToolPermission, error) {
	for _, tp := range r.perms {
		if tp.TenantID == tenantID && tp.ToolName == toolName {
			if roleID == nil && tp.RoleID == nil {
				return tp, nil
			}
			if roleID != nil && tp.RoleID != nil && *roleID == *tp.RoleID {
				return tp, nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func TestCreateAndListRules(t *testing.T) {
	svc := NewRuleService(newMemRuleRepo(), newMemPermRepo())
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, "t1", "no-root-ssh", "Block root SSH", "ssh_check", `{"user":"root"}`, `{"action":"deny"}`, "critical", "u1")
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	if rule.Name != "no-root-ssh" {
		t.Errorf("expected name 'no-root-ssh', got %q", rule.Name)
	}

	rules, err := svc.ListRules(ctx, "t1")
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestUpdateRule(t *testing.T) {
	svc := NewRuleService(newMemRuleRepo(), newMemPermRepo())
	ctx := context.Background()

	rule, _ := svc.CreateRule(ctx, "t1", "test", "", "check", "{}", "", "warning", "u1")
	enabled := false
	updated, err := svc.UpdateRule(ctx, rule.ID, "test-updated", "", "", "", "critical", &enabled)
	if err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	if updated.Name != "test-updated" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}
	if updated.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDeleteRule(t *testing.T) {
	svc := NewRuleService(newMemRuleRepo(), newMemPermRepo())
	ctx := context.Background()

	rule, _ := svc.CreateRule(ctx, "t1", "del-me", "", "check", "{}", "", "info", "u1")
	if err := svc.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	rules, _ := svc.ListRules(ctx, "t1")
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestToolPermissions(t *testing.T) {
	svc := NewRuleService(newMemRuleRepo(), newMemPermRepo())
	ctx := context.Background()

	perm, err := svc.CreateToolPermission(ctx, "t1", "file_download", nil, false, "L2")
	if err != nil {
		t.Fatalf("CreateToolPermission: %v", err)
	}
	if perm.Allowed {
		t.Error("expected allowed=false")
	}

	allowed, err := svc.IsToolAllowed(ctx, "t1", "file_download", nil)
	if err != nil {
		t.Fatalf("IsToolAllowed: %v", err)
	}
	if allowed {
		t.Error("expected tool to be blocked")
	}

	allowed, err = svc.IsToolAllowed(ctx, "t1", "read_file_tail", nil)
	if err != nil {
		t.Fatalf("IsToolAllowed unknown: %v", err)
	}
	if !allowed {
		t.Error("expected unknown tool to be allowed by default")
	}
}

func TestDeleteToolPermission(t *testing.T) {
	svc := NewRuleService(newMemRuleRepo(), newMemPermRepo())
	ctx := context.Background()

	perm, _ := svc.CreateToolPermission(ctx, "t1", "screenshot", nil, true, "")
	if err := svc.DeleteToolPermission(ctx, perm.ID); err != nil {
		t.Fatalf("DeleteToolPermission: %v", err)
	}
	perms, _ := svc.ListToolPermissions(ctx, "t1")
	if len(perms) != 0 {
		t.Errorf("expected 0 perms after delete, got %d", len(perms))
	}
}
