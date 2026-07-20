package governance

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/governance/watchlist"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/store"
)

type BaselineData struct {
	Hostname    string            `json:"hostname"`
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	Interfaces  []InterfaceInfo   `json:"interfaces"`
	EnvVars     map[string]string `json:"env_vars"`
	CapturedAt  string            `json:"captured_at"`
}

type InterfaceInfo struct {
	Name  string   `json:"name"`
	Addrs []string `json:"addrs"`
}

type DriftResult struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type Engine struct {
	store            *store.Store
	watchlistManager *watchlist.Manager
}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) SetStore(s *store.Store) {
	e.store = s
}

func (e *Engine) SetWatchlistManager(wm *watchlist.Manager) {
	e.watchlistManager = wm
}

func (e *Engine) GetWatchlistManager() *watchlist.Manager {
	return e.watchlistManager
}

func (e *Engine) GetHealthScore() (*watchlist.HealthScore, error) {
	if e.watchlistManager == nil {
		return &watchlist.HealthScore{Score: 100, LastUpdated: time.Now().UTC().Format(time.RFC3339)}, nil
	}
	return e.watchlistManager.GetHealthScore()
}

func (e *Engine) GetAlerts(resolved *bool, limit int) ([]*watchlist.WatchAlert, error) {
	if e.watchlistManager == nil {
		return nil, nil
	}
	return e.watchlistManager.ListAlerts(resolved, limit)
}

type Status struct {
	HasBaseline bool `json:"has_baseline"`
	DriftCount  int  `json:"drift_count"`
}

func (e *Engine) GetStatus() Status {
	if e.store == nil {
		return Status{}
	}
	existing, _ := e.store.GetLatestBaseline("system")
	hasBaseline := existing != nil

	var driftCount int
	if hasBaseline {
		drifts, err := e.DetectDrift()
		if err == nil {
			driftCount = len(drifts)
		}
	}
	return Status{HasBaseline: hasBaseline, DriftCount: driftCount}
}

// Start launches the governance engine with its own internal ticker.
// Deprecated: prefer registering RunBaselineCheck as a runtime.Task instead.
func (e *Engine) Start(ctx context.Context) {
	slog.Info("[GovernanceEngine] Starting background baseline checks...")

	ticker := time.NewTicker(5 * time.Minute)

	go func() {
		defer ticker.Stop()

		e.RunBaselineCheck(ctx)

		for {
			select {
			case <-ctx.Done():
				slog.Info("[GovernanceEngine] Stopping...")
				return
			case <-ticker.C:
				e.RunBaselineCheck(ctx)
			}
		}
	}()

	if e.watchlistManager != nil {
		if err := e.watchlistManager.Start(ctx); err != nil {
			slog.Warn("[GovernanceEngine] Failed to start watchlist manager", "error", err)
		}
	}
}

func (e *Engine) CaptureBaseline() (*BaselineData, error) {
	hostname, _ := os.Hostname()

	ifaces, _ := net.Interfaces()
	var interfaces []InterfaceInfo
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		var addrStrs []string
		for _, a := range addrs {
			addrStrs = append(addrStrs, a.String())
		}
		interfaces = append(interfaces, InterfaceInfo{Name: iface.Name, Addrs: addrStrs})
	}

	trackedEnvVars := []string{"PATH", "HOME", "USERPROFILE", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}
	envVars := make(map[string]string)
	for _, key := range trackedEnvVars {
		if val := os.Getenv(key); val != "" {
			envVars[key] = val
		}
	}

	baseline := &BaselineData{
		Hostname:   hostname,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Interfaces: interfaces,
		EnvVars:    envVars,
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if e.store != nil {
		data, _ := json.Marshal(baseline)
		if _, err := e.store.SaveBaseline("system", string(data)); err != nil {
			slog.Warn("[GovernanceEngine] Failed to save baseline", "error", err)
		}
	}

	return baseline, nil
}

func (e *Engine) DetectDrift() ([]DriftResult, error) {
	if e.store == nil {
		return nil, nil
	}

	existing, err := e.store.GetLatestBaseline("system")
	if err != nil || existing == nil {
		return nil, err
	}

	var savedBaseline BaselineData
	if err := json.Unmarshal([]byte(existing.BaselineJSON), &savedBaseline); err != nil {
		return nil, err
	}

	current, err := e.CaptureBaseline()
	if err != nil {
		return nil, err
	}

	var drifts []DriftResult

	if savedBaseline.Hostname != current.Hostname {
		drifts = append(drifts, DriftResult{
			Field:    "hostname",
			Expected: savedBaseline.Hostname,
			Actual:   current.Hostname,
		})
	}

	savedIfaces := make(map[string][]string)
	for _, iface := range savedBaseline.Interfaces {
		savedIfaces[iface.Name] = iface.Addrs
	}
	currentIfaces := make(map[string][]string)
	for _, iface := range current.Interfaces {
		currentIfaces[iface.Name] = iface.Addrs
	}

	for name := range savedIfaces {
		if _, ok := currentIfaces[name]; !ok {
			drifts = append(drifts, DriftResult{
				Field:    "interface_removed",
				Expected: name,
				Actual:   "(missing)",
			})
		}
	}
	for name := range currentIfaces {
		if _, ok := savedIfaces[name]; !ok {
			drifts = append(drifts, DriftResult{
				Field:    "interface_added",
				Expected: "(none)",
				Actual:   name,
			})
		}
	}

	for key, expected := range savedBaseline.EnvVars {
		actual := os.Getenv(key)
		if actual != expected {
			drifts = append(drifts, DriftResult{
				Field:    "env:" + key,
				Expected: expected,
				Actual:   actual,
			})
		}
	}

	if len(drifts) > 0 {
		driftJSON, _ := json.Marshal(drifts)
		severity := "info"
		if len(drifts) > 3 {
			severity = "warning"
		}
		_ = e.store.SaveDrift(existing.ID, string(driftJSON), severity)
		slog.Info("[GovernanceEngine] Drift detected", "count", len(drifts), "severity", severity)
	}

	return drifts, nil
}

// RunBaselineCheck performs a single baseline check cycle.
func (e *Engine) RunBaselineCheck(ctx context.Context) {
	if e.store == nil {
		slog.Info("[GovernanceEngine] No store configured, skipping baseline check")
		return
	}

	existing, _ := e.store.GetLatestBaseline("system")
	if existing == nil {
		slog.Info("[GovernanceEngine] No baseline found, capturing initial baseline")
		if _, err := e.CaptureBaseline(); err != nil {
			slog.Warn("[GovernanceEngine] Failed to capture baseline", "error", err)
		}
		return
	}

	drifts, err := e.DetectDrift()
	if err != nil {
		slog.Warn("[GovernanceEngine] Drift detection failed", "error", err)
		return
	}

	if len(drifts) == 0 {
		slog.Info("[GovernanceEngine] No drift detected")
	}
}
