package file_access

import (
	"context"
	"testing"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type memoryFileAccessRepo struct {
	items map[string]*domain.FileAccessRequest
}

// DeleteCompletedByTenant implements [repository.FileAccessRepository].
func (r *memoryFileAccessRepo) DeleteCompletedByTenant(ctx context.Context, tenantID string) (int64, error) {
	panic("unimplemented")
}

func newMemoryFileAccessRepo() *memoryFileAccessRepo {
	return &memoryFileAccessRepo{items: make(map[string]*domain.FileAccessRequest)}
}
func (r *memoryFileAccessRepo) Create(_ context.Context, req *domain.FileAccessRequest) error {
	r.items[req.ID] = req
	return nil
}
func (r *memoryFileAccessRepo) GetByID(_ context.Context, id string) (*domain.FileAccessRequest, error) {
	req, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return req, nil
}
func (r *memoryFileAccessRepo) ListByTenant(_ context.Context, tenantID string, status string) ([]*domain.FileAccessRequest, error) {
	var result []*domain.FileAccessRequest
	for _, req := range r.items {
		if req.TenantID == tenantID {
			if status == "" || string(req.Status) == status {
				result = append(result, req)
			}
		}
	}
	return result, nil
}
func (r *memoryFileAccessRepo) Update(_ context.Context, req *domain.FileAccessRequest) error {
	r.items[req.ID] = req
	return nil
}

type noopAuditRepo struct{}

func (r *noopAuditRepo) Create(_ context.Context, _ *domain.AuditEvent) error        { return nil }
func (r *noopAuditRepo) CreateBatch(_ context.Context, _ []*domain.AuditEvent) error { return nil }
func (r *noopAuditRepo) ListByTenant(_ context.Context, _ string, _ interface{}) ([]*domain.AuditEvent, error) {
	return nil, nil
}

func TestCreateAndApprove(t *testing.T) {
	repo := newMemoryFileAccessRepo()
	svc := NewService(repo, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Use download: browse/preview are auto-approved by CreateRequest; download stays pending when policy is nil.
	req, err := svc.CreateRequest(ctx, "t1", "d1", "u1", "/var/log/app.log", domain.FileAccessDownload, "need to check logs")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if req.Status != domain.FileAccessPending {
		t.Errorf("expected pending, got %s", req.Status)
	}

	approved, err := svc.Approve(ctx, req.ID, "u2", true)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != domain.FileAccessApproved {
		t.Errorf("expected approved, got %s", approved.Status)
	}
}

func TestCreateAndDeny(t *testing.T) {
	repo := newMemoryFileAccessRepo()
	svc := NewService(repo, nil, nil, nil, nil, nil)
	ctx := context.Background()

	req, err := svc.CreateRequest(ctx, "t1", "d1", "u1", "/etc/config", domain.FileAccessDownload, "")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	denied, err := svc.Deny(ctx, req.ID, "u2")
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if denied.Status != domain.FileAccessDenied {
		t.Errorf("expected denied, got %s", denied.Status)
	}
}

func TestDoubleApprove(t *testing.T) {
	repo := newMemoryFileAccessRepo()
	svc := NewService(repo, nil, nil, nil, nil, nil)
	ctx := context.Background()

	req, _ := svc.CreateRequest(ctx, "t1", "d1", "u1", "/tmp/file", domain.FileAccessPreview, "")
	_, _ = svc.Approve(ctx, req.ID, "u2", true)

	_, err := svc.Approve(ctx, req.ID, "u3", true)
	if err == nil {
		t.Error("expected error on double approve")
	}
}

func TestListByTenant(t *testing.T) {
	repo := newMemoryFileAccessRepo()
	svc := NewService(repo, nil, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateRequest(ctx, "t1", "d1", "u1", "/a", domain.FileAccessBrowse, "")
	svc.CreateRequest(ctx, "t1", "d2", "u1", "/b", domain.FileAccessPreview, "")
	svc.CreateRequest(ctx, "t2", "d3", "u2", "/c", domain.FileAccessDownload, "")

	items, err := svc.ListByTenant(ctx, "t1", "")
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items for t1, got %d", len(items))
	}
}
