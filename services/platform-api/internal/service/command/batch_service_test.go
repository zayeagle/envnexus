package command

import (
	"context"
	"testing"
	"time"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
)

type fakeBatchRepo struct {
	bt      *domain.BatchTask
	members []*domain.DeviceGroupMember
	updates int
}

func (r *fakeBatchRepo) GetBatchTask(_ context.Context, id string) (*domain.BatchTask, error) {
	if r.bt != nil && r.bt.ID == id {
		return r.bt, nil
	}
	return nil, nil
}

func (r *fakeBatchRepo) UpdateBatchTask(_ context.Context, t *domain.BatchTask) error {
	r.updates++
	*r.bt = *t
	return nil
}

func (r *fakeBatchRepo) ListMembers(_ context.Context, _ string, _ int, _ int) ([]*domain.DeviceGroupMember, int64, error) {
	return r.members, int64(len(r.members)), nil
}

type fakeExecRepo struct {
	executions []*domain.CommandExecution
}

func (r *fakeExecRepo) Create(_ context.Context, _ *domain.CommandExecution) error { return nil }
func (r *fakeExecRepo) Update(_ context.Context, _ *domain.CommandExecution) error { return nil }
func (r *fakeExecRepo) GetByID(_ context.Context, _ string) (*domain.CommandExecution, error) {
	return nil, nil
}
func (r *fakeExecRepo) ListByTaskID(_ context.Context, _ string) ([]*domain.CommandExecution, error) {
	return r.executions, nil
}
func (r *fakeExecRepo) GetByTaskAndDevice(_ context.Context, _, _ string) (*domain.CommandExecution, error) {
	return nil, nil
}
func (r *fakeExecRepo) DeleteByTaskID(_ context.Context, _ string) error { return nil }

func TestBatchServiceCountTerminal(t *testing.T) {
	execs := []*domain.CommandExecution{
		{Status: domain.ExecutionSucceeded},
		{Status: domain.ExecutionSucceeded},
		{Status: domain.ExecutionFailed},
		{Status: domain.ExecutionPending},
	}
	bs := &BatchService{execRepo: &fakeExecRepo{executions: execs}}
	ok, fail := bs.countTerminal(context.Background(), "task-1")
	if ok != 2 {
		t.Errorf("expected 2 succeeded, got %d", ok)
	}
	if fail != 1 {
		t.Errorf("expected 1 failed, got %d", fail)
	}
}

func TestBatchServiceUpdateProgress(t *testing.T) {
	bt := &domain.BatchTask{ID: "bt-1", TenantID: "t1", TotalDevices: 10}
	repo := &fakeBatchRepo{bt: bt}
	bs := &BatchService{batchRepo: repo}
	bs.updateProgress(context.Background(), bt, 5, 2)
	if bt.Completed != 5 || bt.Failed != 2 {
		t.Errorf("expected 5/2, got %d/%d", bt.Completed, bt.Failed)
	}
	if repo.updates != 1 {
		t.Errorf("expected 1 update call, got %d", repo.updates)
	}
}

func TestBatchServiceMarkFinal(t *testing.T) {
	bt := &domain.BatchTask{ID: "bt-2", TenantID: "t1", Status: "running"}
	repo := &fakeBatchRepo{bt: bt}
	bs := &BatchService{batchRepo: repo}
	bs.markFinal(context.Background(), bt, "completed", "")
	if bt.Status != "completed" {
		t.Errorf("expected status completed, got %s", bt.Status)
	}
}

func TestBatchServiceCancelClosesChannel(t *testing.T) {
	bs := &BatchService{}
	ch := make(chan struct{})
	bs.pauseControl.Store("bt-x", ch)
	bs.Cancel("bt-x")
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Error("Cancel did not close channel within timeout")
	}
}
