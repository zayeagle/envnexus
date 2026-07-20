package watchlist

import (
	"context"
	"log/slog"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type Manager struct {
	store      *Store
	scheduler  *Scheduler
	decomposer *Decomposer
	alerter    *Alerter
}

func NewManager(store *Store, registry *tools.Registry, llmRouter *router.Router) *Manager {
	scheduler := NewScheduler(store, registry)
	decomposer := NewDecomposer(llmRouter, registry)
	alerter := NewAlerter(store, scheduler)

	scheduler.SetAlertHandler(func(alert *WatchAlert) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		alerter.HandleAlert(ctx, alert)
	})

	return &Manager{
		store:      store,
		scheduler:  scheduler,
		decomposer: decomposer,
		alerter:    alerter,
	}
}

func (m *Manager) SetRemediator(r AlertRemediator) {
	m.alerter.SetRemediator(r)
}

func (m *Manager) Start(ctx context.Context) error {
	return m.scheduler.Start(ctx)
}

func (m *Manager) Stop() {
	m.scheduler.Stop()
}

// RegisterBuiltinRules registers all built-in watch rules, skipping any
// that reference tools not present in the registry.
func (m *Manager) RegisterBuiltinRules(ctx context.Context, registry *tools.Registry) {
	rules := BuiltinRules()
	registered := 0
	for _, rule := range rules {
		if _, ok := registry.Get(rule.ToolName); !ok {
			slog.Debug("[WatchlistManager] skipping builtin rule (tool not available)",
				"rule", rule.Name, "tool", rule.ToolName)
			continue
		}
		rule.CreatedAt = time.Now().UTC()
		if err := m.scheduler.Register(ctx, rule); err != nil {
			slog.Warn("[WatchlistManager] failed to register builtin rule",
				"rule", rule.Name, "error", err)
			continue
		}
		registered++
	}
	slog.Info("[WatchlistManager] builtin rules registered", "count", registered, "total", len(rules))
}

// Decompose converts natural language into watch items via LLM.
func (m *Manager) Decompose(ctx context.Context, input string) (*DecomposeResult, error) {
	return m.decomposer.Decompose(ctx, input)
}

// ConfirmItems registers the given watch items (typically from a decompose result).
func (m *Manager) ConfirmItems(ctx context.Context, items []WatchItem) error {
	for i := range items {
		if err := m.scheduler.Register(ctx, &items[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ListItems(source string) ([]*WatchItem, error) {
	return m.store.ListWatchItems(source)
}

func (m *Manager) GetItem(id string) (*WatchItem, error) {
	return m.store.GetWatchItem(id)
}

func (m *Manager) UpdateItem(ctx context.Context, item *WatchItem) error {
	if err := m.store.SaveWatchItem(item); err != nil {
		return err
	}
	if item.Enabled {
		m.scheduler.SetEnabled(ctx, item.ID, true)
	}
	return nil
}

func (m *Manager) DeleteItem(id string) error {
	return m.scheduler.Unregister(id)
}

func (m *Manager) SetItemEnabled(ctx context.Context, id string, enabled bool) error {
	return m.scheduler.SetEnabled(ctx, id, enabled)
}

func (m *Manager) ListAlerts(resolved *bool, limit int) ([]*WatchAlert, error) {
	return m.store.ListAlerts(resolved, limit)
}

func (m *Manager) ResolveAlert(id string) error {
	return m.store.ResolveAlert(id)
}

func (m *Manager) GetHealthScore() (*HealthScore, error) {
	items, err := m.store.ListEnabledWatchItems()
	if err != nil {
		return nil, err
	}

	total := len(items)
	healthy := 0
	warning := 0
	critical := 0

	for _, item := range items {
		switch item.LastStatus {
		case "healthy", "":
			healthy++
		case "triggered":
			if item.ConsecutiveFail >= 3 {
				critical++
			} else {
				warning++
			}
		case "error":
			if item.ConsecutiveFail >= 5 {
				critical++
			} else {
				warning++
			}
		}
	}

	score := 100
	if total > 0 {
		score = (healthy * 100) / total
	}

	return &HealthScore{
		Score:       score,
		Total:       total,
		Healthy:     healthy,
		Warning:     warning,
		Critical:    critical,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
