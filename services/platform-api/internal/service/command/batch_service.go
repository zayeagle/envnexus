package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

// DefaultBatchSize is used when BatchTask.BatchSize <= 0.
const DefaultBatchSize = 10

// DefaultSuccessRateThreshold pauses the batch run when the cumulative
// success rate dips below this value (range [0, 1]).
const DefaultSuccessRateThreshold = 0.9

// DefaultInterBatchDelay is the wait time between batches.
const DefaultInterBatchDelay = 5 * time.Second

// PerBatchPollInterval controls how often we poll command_executions for batch completion.
const PerBatchPollInterval = 2 * time.Second

// PerBatchTimeout is the longest we wait for one batch's executions to settle.
const PerBatchTimeout = 5 * time.Minute

// BatchTaskRepository is the subset of methods BatchService needs from the
// device group repository. We declare it locally to avoid a hard import cycle
// on the device_group package.
type BatchTaskRepository interface {
	GetBatchTask(ctx context.Context, id string) (*domain.BatchTask, error)
	UpdateBatchTask(ctx context.Context, t *domain.BatchTask) error
	ListMembers(ctx context.Context, groupID string, page, pageSize int) ([]*domain.DeviceGroupMember, int64, error)
}

// BatchService runs a BatchTask: splits the device group into fixed-size
// batches, dispatches a CommandTask per batch (re-using the existing single-
// task pipeline), waits for executions, and stops if the success rate drops
// below the threshold.
type BatchService struct {
	commandSvc   *Service
	batchRepo    BatchTaskRepository
	execRepo     repository.CommandExecutionRepository
	taskRepo     repository.CommandTaskRepository
	pauseControl sync.Map // batchID -> chan struct{}{} (closed = cancel)
}

func NewBatchService(
	commandSvc *Service,
	batchRepo BatchTaskRepository,
	execRepo repository.CommandExecutionRepository,
	taskRepo repository.CommandTaskRepository,
) *BatchService {
	return &BatchService{
		commandSvc: commandSvc,
		batchRepo:  batchRepo,
		execRepo:   execRepo,
		taskRepo:   taskRepo,
	}
}

// BatchDispatchOptions controls one batch run.
type BatchDispatchOptions struct {
	TenantID             string
	UserID               string
	BatchTaskID          string
	GroupID              string
	BatchSize            int
	SuccessRateThreshold float64
	InterBatchDelay      time.Duration
	// CommandTemplate is the per-batch command payload (re-used for every batch).
	// DeviceIDs in the template are ignored; the batch service fills them per batch.
	CommandTemplate dto.CreateCommandTaskRequest
}

// Dispatch starts background execution of a BatchTask. Returns immediately;
// progress is written back to the BatchTask record.
func (b *BatchService) Dispatch(ctx context.Context, opts BatchDispatchOptions) error {
	if b.commandSvc == nil || b.batchRepo == nil {
		return fmt.Errorf("batch service: dependencies not configured")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.SuccessRateThreshold <= 0 {
		opts.SuccessRateThreshold = DefaultSuccessRateThreshold
	}
	if opts.InterBatchDelay <= 0 {
		opts.InterBatchDelay = DefaultInterBatchDelay
	}

	bt, err := b.batchRepo.GetBatchTask(ctx, opts.BatchTaskID)
	if err != nil {
		return fmt.Errorf("get batch task: %w", err)
	}
	if bt == nil {
		return fmt.Errorf("batch task %s not found", opts.BatchTaskID)
	}
	if bt.TenantID != opts.TenantID {
		return fmt.Errorf("batch task tenant mismatch")
	}

	cancel := make(chan struct{})
	b.pauseControl.Store(bt.ID, cancel)

	go b.run(opts, cancel)
	return nil
}

// Cancel signals an in-flight batch run to stop after the current batch.
func (b *BatchService) Cancel(batchTaskID string) {
	if v, ok := b.pauseControl.LoadAndDelete(batchTaskID); ok {
		close(v.(chan struct{}))
	}
}

func (b *BatchService) run(opts BatchDispatchOptions, cancelCh <-chan struct{}) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	go func() {
		<-cancelCh
		cancelRoot()
	}()

	bt, err := b.batchRepo.GetBatchTask(rootCtx, opts.BatchTaskID)
	if err != nil {
		slog.Error("[batch] reload batch task failed", "id", opts.BatchTaskID, "error", err)
		return
	}

	members, _, err := b.batchRepo.ListMembers(rootCtx, opts.GroupID, 0, 0)
	if err != nil || len(members) == 0 {
		b.markFinal(rootCtx, bt, "failed", "no members in device group")
		return
	}

	bt.Status = "running"
	bt.TotalDevices = len(members)
	if err := b.batchRepo.UpdateBatchTask(rootCtx, bt); err != nil {
		slog.Warn("[batch] mark running failed", "id", bt.ID, "error", err)
	}

	deviceIDs := make([]string, 0, len(members))
	for _, m := range members {
		deviceIDs = append(deviceIDs, m.DeviceID)
	}

	totalCompleted := 0
	totalFailed := 0
	for batchIdx, start := 0, 0; start < len(deviceIDs); batchIdx, start = batchIdx+1, start+opts.BatchSize {
		select {
		case <-rootCtx.Done():
			b.markFinal(rootCtx, bt, "cancelled", "batch run cancelled")
			return
		default:
		}

		end := start + opts.BatchSize
		if end > len(deviceIDs) {
			end = len(deviceIDs)
		}
		batchDevices := deviceIDs[start:end]

		slog.Info("[batch] dispatching batch",
			"batch_task_id", bt.ID,
			"batch_index", batchIdx,
			"device_count", len(batchDevices),
		)

		batchCmdReq := opts.CommandTemplate
		batchCmdReq.DeviceIDs = batchDevices
		batchCmdReq.Title = fmt.Sprintf("%s [batch %d]", opts.CommandTemplate.Title, batchIdx+1)

		taskResp, cerr := b.commandSvc.CreateTask(rootCtx, opts.TenantID, opts.UserID, batchCmdReq)
		if cerr != nil {
			slog.Error("[batch] create per-batch task failed", "error", cerr, "batch_index", batchIdx)
			totalFailed += len(batchDevices)
			b.updateProgress(rootCtx, bt, totalCompleted, totalFailed)
			break
		}

		// Submit so the task moves into the dispatch pipeline. SubmitTask handles
		// approval routing; for L0 risk it goes straight to execution.
		if _, serr := b.commandSvc.SubmitTask(rootCtx, opts.TenantID, opts.UserID, taskResp.ID); serr != nil {
			slog.Error("[batch] submit per-batch task failed", "error", serr, "batch_index", batchIdx, "task_id", taskResp.ID)
			totalFailed += len(batchDevices)
			b.updateProgress(rootCtx, bt, totalCompleted, totalFailed)
			break
		}

		ok, fail := b.waitForBatch(rootCtx, taskResp.ID, len(batchDevices))
		totalCompleted += ok
		totalFailed += fail
		b.updateProgress(rootCtx, bt, totalCompleted, totalFailed)

		processed := totalCompleted + totalFailed
		if processed > 0 {
			rate := float64(totalCompleted) / float64(processed)
			if rate < opts.SuccessRateThreshold {
				slog.Warn("[batch] success rate below threshold, pausing",
					"batch_task_id", bt.ID,
					"success_rate", rate,
					"threshold", opts.SuccessRateThreshold,
				)
				b.markFinal(rootCtx, bt, "paused",
					fmt.Sprintf("auto-paused: success rate %.2f below threshold %.2f", rate, opts.SuccessRateThreshold))
				return
			}
		}

		if end < len(deviceIDs) {
			select {
			case <-rootCtx.Done():
				b.markFinal(rootCtx, bt, "cancelled", "batch run cancelled")
				return
			case <-time.After(opts.InterBatchDelay):
			}
		}
	}

	b.updateProgress(rootCtx, bt, totalCompleted, totalFailed)
	if totalFailed == 0 {
		b.markFinal(rootCtx, bt, "completed", "")
	} else if totalCompleted > 0 {
		b.markFinal(rootCtx, bt, "partial_done", "")
	} else {
		b.markFinal(rootCtx, bt, "failed", "all executions failed")
	}
}

// waitForBatch polls the executions for the given task until all are terminal
// or PerBatchTimeout elapses. Returns (succeeded, failed) counts.
func (b *BatchService) waitForBatch(ctx context.Context, taskID string, expected int) (int, int) {
	deadline := time.Now().Add(PerBatchTimeout)
	for {
		select {
		case <-ctx.Done():
			return b.countTerminal(ctx, taskID)
		default:
		}

		execs, err := b.execRepo.ListByTaskID(ctx, taskID)
		if err == nil {
			succeeded, failed, terminal := 0, 0, 0
			for _, e := range execs {
				switch e.Status {
				case domain.ExecutionSucceeded:
					succeeded++
					terminal++
				case domain.ExecutionFailed, domain.ExecutionTimeout, domain.ExecutionSkipped:
					failed++
					terminal++
				}
			}
			if terminal >= expected {
				return succeeded, failed
			}
		}

		if time.Now().After(deadline) {
			s, f := b.countTerminal(ctx, taskID)
			// Anything still pending counts as failed for batch metrics.
			pending := expected - (s + f)
			if pending < 0 {
				pending = 0
			}
			return s, f + pending
		}
		time.Sleep(PerBatchPollInterval)
	}
}

func (b *BatchService) countTerminal(ctx context.Context, taskID string) (int, int) {
	execs, err := b.execRepo.ListByTaskID(ctx, taskID)
	if err != nil {
		return 0, 0
	}
	succeeded, failed := 0, 0
	for _, e := range execs {
		switch e.Status {
		case domain.ExecutionSucceeded:
			succeeded++
		case domain.ExecutionFailed, domain.ExecutionTimeout, domain.ExecutionSkipped:
			failed++
		}
	}
	return succeeded, failed
}

func (b *BatchService) updateProgress(ctx context.Context, bt *domain.BatchTask, completed, failed int) {
	bt.Completed = completed
	bt.Failed = failed
	if err := b.batchRepo.UpdateBatchTask(ctx, bt); err != nil {
		slog.Warn("[batch] update progress failed", "id", bt.ID, "error", err)
	}
}

func (b *BatchService) markFinal(ctx context.Context, bt *domain.BatchTask, status, note string) {
	bt.Status = status
	if err := b.batchRepo.UpdateBatchTask(ctx, bt); err != nil {
		slog.Warn("[batch] mark final failed", "id", bt.ID, "status", status, "error", err)
	}
	b.pauseControl.Delete(bt.ID)
	if note != "" {
		slog.Info("[batch] batch run finalized", "id", bt.ID, "status", status, "note", note)
	} else {
		slog.Info("[batch] batch run finalized", "id", bt.ID, "status", status)
	}
}

// Compile-time assert that we use the json package somewhere (prevents goimports
// from removing it later if we add structured payload encoding).
var _ = ulid.Make
var _ = json.Marshal
