package watchlist

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type AlertHandler func(alert *WatchAlert)

type Scheduler struct {
	store        *Store
	registry     *tools.Registry
	alertHandler AlertHandler

	mu       sync.Mutex
	timers   map[string]*time.Timer
	running  bool
	cancelFn context.CancelFunc
}

func NewScheduler(store *Store, registry *tools.Registry) *Scheduler {
	return &Scheduler{
		store:    store,
		registry: registry,
		timers:   make(map[string]*time.Timer),
	}
}

func (s *Scheduler) SetAlertHandler(h AlertHandler) {
	s.alertHandler = h
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFn = cancel
	s.running = true
	s.mu.Unlock()

	items, err := s.store.ListEnabledWatchItems()
	if err != nil {
		slog.Warn("[WatchScheduler] failed to load watch items", "error", err)
		return err
	}

	for _, item := range items {
		s.scheduleItem(ctx, item)
	}

	slog.Info("[WatchScheduler] started", "items", len(items))
	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancelFn != nil {
		s.cancelFn()
	}
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
	s.running = false
	slog.Info("[WatchScheduler] stopped")
}

func (s *Scheduler) Register(ctx context.Context, item *WatchItem) error {
	if err := s.store.SaveWatchItem(item); err != nil {
		return fmt.Errorf("save watch item: %w", err)
	}
	if item.Enabled {
		s.scheduleItem(ctx, item)
	}
	slog.Info("[WatchScheduler] registered", "id", item.ID, "name", item.Name)
	return nil
}

func (s *Scheduler) Unregister(id string) error {
	s.mu.Lock()
	if timer, ok := s.timers[id]; ok {
		timer.Stop()
		delete(s.timers, id)
	}
	s.mu.Unlock()

	if err := s.store.DeleteWatchItem(id); err != nil {
		return fmt.Errorf("delete watch item: %w", err)
	}
	slog.Info("[WatchScheduler] unregistered", "id", id)
	return nil
}

func (s *Scheduler) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if err := s.store.SetWatchItemEnabled(id, enabled); err != nil {
		return err
	}

	s.mu.Lock()
	if timer, ok := s.timers[id]; ok {
		timer.Stop()
		delete(s.timers, id)
	}
	s.mu.Unlock()

	if enabled {
		item, err := s.store.GetWatchItem(id)
		if err != nil || item == nil {
			return err
		}
		s.scheduleItem(ctx, item)
	}
	return nil
}

func (s *Scheduler) RunNow(ctx context.Context, id string) error {
	item, err := s.store.GetWatchItem(id)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("watch item %s not found", id)
	}
	s.executeCheck(ctx, item)
	return nil
}

func (s *Scheduler) scheduleItem(ctx context.Context, item *WatchItem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.timers[item.ID]; ok {
		existing.Stop()
	}

	interval := item.Interval
	if interval < 60*time.Second {
		interval = 60 * time.Second
	}

	var scheduleNext func()
	scheduleNext = func() {
		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			return
		}
		timer := time.AfterFunc(interval, func() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			s.executeCheck(ctx, item)
			// Reload item to get updated state
			updated, err := s.store.GetWatchItem(item.ID)
			if err != nil || updated == nil || !updated.Enabled {
				return
			}
			item = updated
			scheduleNext()
		})
		s.timers[item.ID] = timer
		s.mu.Unlock()
	}

	// Run first check after a short delay to stagger startup
	initialDelay := time.Duration(len(s.timers)) * 2 * time.Second
	if initialDelay > 30*time.Second {
		initialDelay = 30 * time.Second
	}

	timer := time.AfterFunc(initialDelay, func() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.executeCheck(ctx, item)
		updated, err := s.store.GetWatchItem(item.ID)
		if err != nil || updated == nil || !updated.Enabled {
			return
		}
		item = updated
		scheduleNext()
	})
	s.timers[item.ID] = timer
}

func (s *Scheduler) executeCheck(ctx context.Context, item *WatchItem) {
	tool, ok := s.registry.Get(item.ToolName)
	if !ok {
		slog.Warn("[WatchScheduler] tool not found", "tool", item.ToolName, "item", item.ID)
		s.updateStatus(item, "error", item.ConsecutiveFail+1)
		return
	}

	toolCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := tool.Execute(toolCtx, item.ToolParams)
	if err != nil {
		slog.Warn("[WatchScheduler] tool execution failed", "tool", item.ToolName, "item", item.ID, "error", err)
		s.handleFailure(item, fmt.Sprintf("tool execution error: %v", err))
		return
	}

	if result.Status != "success" {
		s.handleFailure(item, fmt.Sprintf("tool returned status: %s, error: %s", result.Status, result.Error))
		return
	}

	output, err := parseToolOutput(result.Output)
	if err != nil {
		slog.Warn("[WatchScheduler] failed to parse tool output", "item", item.ID, "error", err)
		s.handleFailure(item, fmt.Sprintf("failed to parse tool output: %v", err))
		return
	}

	evalResult, err := Evaluate(item.Condition, output)
	if err != nil {
		slog.Warn("[WatchScheduler] condition evaluation failed", "item", item.ID, "error", err)
		s.handleFailure(item, fmt.Sprintf("condition evaluation error: %v", err))
		return
	}

	if evalResult.Triggered {
		s.handleTriggered(item, evalResult)
	} else {
		s.updateStatus(item, "healthy", 0)
	}
}

func (s *Scheduler) handleFailure(item *WatchItem, message string) {
	newFail := item.ConsecutiveFail + 1
	s.updateStatus(item, "error", newFail)

	if newFail >= 3 {
		severity := SeverityWarning
		if newFail >= 5 {
			severity = SeverityCritical
		}
		s.emitAlert(item, severity, message)
	}
}

func (s *Scheduler) handleTriggered(item *WatchItem, evalResult *EvalResult) {
	newFail := item.ConsecutiveFail + 1
	s.updateStatus(item, "triggered", newFail)

	severity := SeverityWarning
	if newFail >= 3 {
		severity = SeverityCritical
	}
	s.emitAlert(item, severity, evalResult.Message)
}

func (s *Scheduler) updateStatus(item *WatchItem, status string, consecutiveFail int) {
	if err := s.store.UpdateWatchItemStatus(item.ID, status, consecutiveFail); err != nil {
		slog.Warn("[WatchScheduler] failed to update status", "item", item.ID, "error", err)
	}
}

func (s *Scheduler) emitAlert(item *WatchItem, severity AlertSeverity, message string) {
	alert := &WatchAlert{
		ID:          fmt.Sprintf("wa_%d", time.Now().UnixNano()),
		WatchItemID: item.ID,
		ItemName:    item.Name,
		Severity:    severity,
		Message:     message,
		CreatedAt:   time.Now().UTC(),
	}

	if err := s.store.SaveAlert(alert); err != nil {
		slog.Warn("[WatchScheduler] failed to save alert", "item", item.ID, "error", err)
		return
	}

	if s.alertHandler != nil {
		s.alertHandler(alert)
	}

	slog.Info("[WatchScheduler] alert emitted",
		"item", item.Name, "severity", severity, "message", truncate(message, 100))
}

func parseToolOutput(output interface{}) (map[string]interface{}, error) {
	switch v := output.(type) {
	case map[string]interface{}:
		return v, nil
	case string:
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return map[string]interface{}{"raw": v}, nil
		}
		return m, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal tool output: %w", err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return map[string]interface{}{"raw": string(b)}, nil
		}
		return m, nil
	}
}
