package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/activation"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/api"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/audit"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/config"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/device"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/diagnosis"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/enrollment"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/governance"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/governance/watchlist"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/lifecycle"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/providers"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/llm/router"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/policy"
	agentruntime "github.com/zayeagle/envnexus/apps/agent-core/internal/runtime"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/session"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/store"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools/cache"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools/container"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools/database"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools/network"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools/service"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools/system"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/updater"
)

type Bootstrapper struct {
	identityManager *device.IdentityManager
	configManager   *config.Manager
	configDir       string
	localServer     *api.LocalServer
	localStore      *store.Store
	runtime         *agentruntime.Runtime
	updater         *updater.Updater
	platformSync    *watchlist.PlatformSync
	version         string
}

// NewBootstrapper creates a bootstrapper with the default config directory (~/.envnexus/agent).
// Use SetDataDir before Run() to override the data/config location (e.g. when running under Electron).
func NewBootstrapper(version string) *Bootstrapper {
	configDir := DefaultConfigDir()
	return &Bootstrapper{
		identityManager: device.NewIdentityManager(configDir),
		configManager:   config.NewManager(configDir),
		configDir:       configDir,
		version:         version,
	}
}

// SetDataDir overrides the config/data directory (used by Electron to align with install path).
func (b *Bootstrapper) SetDataDir(dir string) {
	b.configDir = dir
	b.identityManager = device.NewIdentityManager(dir)
	b.configManager = config.NewManager(dir)
}

func DefaultConfigDir() string {
	if dir := portableDataDir(); dir != "" {
		return dir
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".envnexus", "agent")
}

// portableDataDir returns the data directory when a `.portable` marker
// exists next to the executable or in a parent directory (ZIP/portable distribution).
// When bundled inside Electron, the binary lives at resources/bin/enx-agent.exe
// while .portable is at the app root, so we check up to 3 levels.
func portableDataDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exePath)
	for i := 0; i < 4; i++ {
		marker := filepath.Join(dir, ".portable")
		if _, err := os.Stat(marker); err == nil {
			dataDir := filepath.Join(dir, "data")
			os.MkdirAll(dataDir, 0o755)
			return dataDir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (b *Bootstrapper) ApplyCLIOverrides(o config.CLIOverrides) {
	b.configManager.ApplyCLIOverrides(o)
}

func (b *Bootstrapper) Run(ctx context.Context) error {
	slog.Info("[boot] Starting agent bootstrap sequence...")
	cfg := b.configManager.Get()

	// Step 0: Initialize local SQLite store (agentdb lives alongside config)
	dataDir := filepath.Join(b.configDir, "data")
	localStore, err := store.New(dataDir)
	if err != nil {
		slog.Warn("[boot] SQLite store init failed, running without local persistence", "error", err)
	} else {
		b.localStore = localStore
	}

	// Step 0.5: Activation check (if activation_mode is configured)
	var activationMgr *activation.Manager
	if cfg.ActivationMode != "" {
		activationMgr = activation.NewManager(cfg.PlatformURL, cfg.ActivationMode, cfg.ActivationKey, b.configDir)
		if !activationMgr.IsActivated() {
			slog.Info("[boot] Activation required", "mode", cfg.ActivationMode)
			if err := activationMgr.Activate(ctx); err != nil {
				slog.Warn("[boot] Activation attempt failed", "error", err)
			}
			if cfg.ActivationMode == "manual" && !activationMgr.IsActivated() {
				slog.Info("[boot] Waiting for admin to bind device", "device_code", activationMgr.GetDeviceCode())
			}
		} else {
			slog.Info("[boot] Device is activated", "device_code", activationMgr.GetDeviceCode())
		}
	}

	// Step 1: Check device identity / enroll if needed
	var deviceID, deviceToken, tenantID string
	platformReachable := true

	if b.identityManager.HasIdentity() {
		id, _ := b.identityManager.GetIdentity()
		deviceID = id.DeviceID
		deviceToken = id.Token
		tenantID = id.TenantID
		slog.Info("[boot] Loaded existing identity", "device", deviceID, "tenant", tenantID)
	} else {
		slog.Info("[boot] No identity found, attempting enrollment...")
		if cfg.EnrollmentToken == "" {
			slog.Info("[boot] No enrollment token configured. Set ENX_ENROLLMENT_TOKEN or wait for installer.")
			slog.Info("[boot] Running in standalone mode...")
			deviceID = "standalone"
		} else {
			enrollClient := enrollment.NewClient(cfg.PlatformURL)
			resp, err := enrollClient.Enroll(ctx, cfg.EnrollmentToken, cfg.AgentVersion)
			if err != nil {
				slog.Warn("[boot] Enrollment failed, running in standalone mode", "error", err)
				deviceID = "standalone"
				platformReachable = false
			} else {
				deviceID = resp.DeviceID
				deviceToken = resp.DeviceToken
				tenantID = resp.TenantID
				slog.Info("[boot] Enrolled successfully", "device", deviceID, "tenant", tenantID)

				if err := b.identityManager.SaveIdentity(&device.Identity{
					DeviceID: deviceID,
					TenantID: tenantID,
					Token:    deviceToken,
				}); err != nil {
					slog.Error("[boot] Failed to save identity", "error", err)
				}

				b.configManager.Update(func(c *config.AgentConfig) {
					c.ConfigVersion = resp.ConfigVersion
				})
			}
		}
	}

	// Step 2: Pull remote config (skip in offline mode)
	var remoteModelProfile *remoteModelConfig
	if deviceToken != "" && platformReachable {
		slog.Info("[boot] Pulling remote configuration...")
		lifecycleClient := lifecycle.NewClient(cfg.PlatformURL, deviceID, deviceToken)
		configResp, err := lifecycleClient.GetConfig(ctx, cfg.ConfigVersion)
		if err != nil {
			slog.Warn("[boot] Config pull failed, entering offline mode", "error", err)
			platformReachable = false
		} else {
			if configResp.HasUpdate {
				slog.Info("[boot] Config updated", "config_version", configResp.ConfigVersion)
				b.configManager.Update(func(c *config.AgentConfig) {
					c.ConfigVersion = configResp.ConfigVersion
				})
			} else {
				slog.Info("[boot] Config is up to date")
			}
			if len(configResp.ModelProfile) > 0 {
				slog.Debug("[boot] Raw model profile JSON", "json", string(configResp.ModelProfile))
				var mp remoteModelConfig
				if err := json.Unmarshal(configResp.ModelProfile, &mp); err != nil {
					slog.Warn("[boot] Failed to parse remote model profile", "error", err)
				} else if mp.Provider != "" {
					remoteModelProfile = &mp
					slog.Info("[boot] Remote model profile loaded",
						"provider", mp.Provider,
						"model", mp.ModelName,
						"base_url", mp.BaseURL,
						"has_api_key", mp.APIKey != "",
					)
				} else {
					slog.Warn("[boot] Remote model profile has empty provider, skipping", "raw", string(configResp.ModelProfile))
				}
			}
		}
	}

	// Step 3: Initialize tool registry
	registry := tools.NewRegistry()
	registry.Register(network.NewReadNetworkConfigTool())
	registry.Register(network.NewReadProxyConfigTool())
	registry.Register(network.NewFlushDNSTool())
	registry.Register(network.NewPingTool())
	registry.Register(network.NewDNSLookupTool())
	registry.Register(network.NewTracerouteTool())
	registry.Register(network.NewPortScanTool())
	registry.Register(network.NewHTTPCheckTool())
	registry.Register(network.NewRouteTableTool())
	registry.Register(network.NewTLSCheckTool())
	registry.Register(system.NewReadSystemInfoTool())
	registry.Register(system.NewReadDiskUsageTool())
	registry.Register(system.NewReadProcessListTool())
	registry.Register(system.NewReadEnvVarsTool())
	registry.Register(system.NewReadFileInfoTool())
	registry.Register(system.NewReadFileTailTool())
	registry.Register(system.NewReadDirListTool())
	registry.Register(system.NewReadInstalledAppsTool())
	registry.Register(system.NewReadEventLogTool())
	registry.Register(system.NewCheckRuntimeDepsTool())
	registry.Register(system.NewShellExecTool())
	registry.Register(system.NewProxyToggleTool())
	registry.Register(system.NewConfigModifyTool())
	registry.Register(system.NewFileRenameTool())
	registry.Register(service.NewRestartTool())
	registry.Register(service.NewContainerReloadTool())
	registry.Register(cache.NewRebuildTool())
	registry.Register(container.NewDockerInspectTool())
	registry.Register(container.NewDockerComposeTool())
	registry.Register(container.NewKubectlDiagnoseTool())
	registry.Register(database.NewMySQLCheckTool())
	registry.Register(database.NewPostgresCheckTool())
	registry.Register(database.NewRedisCheckTool())
	registry.Register(database.NewMongoCheckTool())
	registry.Register(system.NewFileDownloadTool())
	registry.Register(system.NewScreenshotTool())

	if !platformReachable {
		slog.Info("[boot] Offline mode: only read-only tools available", "count", countReadOnly(registry))
	} else {
		slog.Info("[boot] Registered tools", "count", registry.Count())
	}

	// Step 4: Initialize LLM router
	llmRouter := b.initLLMRouter(remoteModelProfile)

	// Step 5: Initialize engines
	policyEngine := policy.NewEngine()
	if deviceToken != "" && platformReachable {
		policyEngine.SetPlatformClient(policy.NewPlatformClient(cfg.PlatformURL, deviceToken))
	}

	auditClient := audit.NewClient(cfg.PlatformURL, deviceID, deviceToken)
	if platformReachable {
		go auditClient.StartFlushLoop(ctx, 15*time.Second)
	}

	diagnosisEngine := diagnosis.NewEngine(registry, llmRouter)

	// Step 6: Start governance engine
	governanceEngine := governance.NewEngine()
	if localStore != nil {
		governanceEngine.SetStore(localStore)
	}

	// Step 6b: Initialize watchlist manager
	if localStore != nil {
		wlStore := watchlist.NewStore(localStore.DB())
		if err := wlStore.Migrate(); err != nil {
			slog.Warn("[boot] Watchlist migration failed", "error", err)
		} else {
			wlManager := watchlist.NewManager(wlStore, registry, llmRouter)
			wlManager.RegisterBuiltinRules(ctx, registry)
			governanceEngine.SetWatchlistManager(wlManager)
			slog.Info("[boot] Watchlist manager initialized")

			if deviceToken != "" && platformReachable {
				platformSync := watchlist.NewPlatformSync(wlManager, cfg.PlatformURL, deviceToken, 5*time.Minute)
				platformSync.Start(ctx)
				b.platformSync = platformSync
				slog.Info("[boot] Watchlist platform sync started")
			}
		}
	}

	// Step 7: Start local API
	localServer := api.NewLocalServer(17700, b.identityManager, policyEngine, diagnosisEngine, llmRouter, registry)
	localServer.SetPlatformConnected(platformReachable)
	localServer.SetGovernanceEngine(governanceEngine)
	if activationMgr != nil {
		localServer.SetActivationManager(activationMgr)
	}
	if localStore != nil {
		localServer.SetStore(localStore)
	}
	if err := localServer.Start(); err != nil {
		slog.Error("[boot] Failed to start local API", "error", err)
		return fmt.Errorf("start local API: %w", err)
	}
	b.localServer = localServer

	// Step 8: Initialize runtime with background tasks
	rt := agentruntime.New()
	b.runtime = rt

	rt.Register(agentruntime.Task{
		Name:     "governance_baseline",
		Interval: 5 * time.Minute,
		Fn: func(ctx context.Context) error {
			governanceEngine.RunBaselineCheck(ctx)
			return nil
		},
	})

	if deviceToken != "" && platformReachable {
		lifecycleClient := lifecycle.NewClient(cfg.PlatformURL, deviceID, deviceToken)
		rt.Register(agentruntime.Task{
			Name:     "heartbeat",
			Interval: time.Duration(cfg.HeartbeatSeconds) * time.Second,
			Fn: func(ctx context.Context) error {
				currentCfg := b.configManager.Get()
				var devCode string
				if activationMgr != nil {
					devCode = activationMgr.GetDeviceCode()
				}
				return lifecycleClient.Heartbeat(ctx, currentCfg.AgentVersion, currentCfg.DistributionPackageVersion, currentCfg.ConfigVersion, lifecycle.CollectRuntimeEnvironment(), devCode)
			},
		})
	}

	if activationMgr != nil {
		if activationMgr.IsActivated() {
			rt.Register(agentruntime.Task{
				Name:     "activation_heartbeat",
				Interval: 5 * time.Minute,
				Fn: func(ctx context.Context) error {
					return activationMgr.SendHeartbeat(ctx)
				},
			})
		} else if cfg.ActivationMode == "manual" {
			rt.Register(agentruntime.Task{
				Name:     "activation_status_poll",
				Interval: 30 * time.Second,
				Fn: func(ctx context.Context) error {
					return activationMgr.CheckStatus(ctx)
				},
			})
		}
	}

	if localStore != nil {
		rt.Register(agentruntime.Task{
			Name:     "store_vacuum",
			Interval: 24 * time.Hour,
			Fn: func(ctx context.Context) error {
				return localStore.Vacuum()
			},
		})
	}

	rt.Start(ctx)

	// Step 9: Initialize self-updater (only when platform reachable and device enrolled)
	if deviceToken != "" && platformReachable {
		updateCheckVer := strings.TrimSpace(cfg.DistributionPackageVersion)
		if updateCheckVer == "" {
			updateCheckVer = b.version
			slog.Warn("[boot] distribution_package_version not set in config; using agent-core version for update checks",
				"version", updateCheckVer,
				"hint", "reinstall from a newly built download package, or add distribution_package_version to agent.enx",
			)
		}
		agentUpdater := updater.New(updater.Config{
			PlatformURL:    cfg.PlatformURL,
			DeviceToken:    deviceToken,
			CurrentVersion: updateCheckVer,
			CoreVersion:    b.version,
			AutoUpdate:     cfg.AutoUpdate,
			CheckInterval:  1 * time.Hour,
			DataDir:        b.configDir,
			OnVersionApplied: func(newVersion string) {
				b.configManager.Update(func(c *config.AgentConfig) {
					c.DistributionPackageVersion = newVersion
				})
				slog.Info("[boot] Persisted new distribution_package_version to config", "version", newVersion)
			},
		})
		b.updater = agentUpdater

		rt.Register(agentruntime.Task{
			Name:     "self_update_check",
			Interval: 1 * time.Hour,
			Fn: func(ctx context.Context) error {
				info, err := agentUpdater.CheckForUpdate(ctx)
				if err != nil {
					return err
				}
				if info.HasUpdate {
					slog.Info("[updater] Update available", "distribution_version", updateCheckVer, "core_version", b.version, "latest", info.LatestVersion)
					if cfg.AutoUpdate {
						if _, dlErr := agentUpdater.DownloadUpdate(ctx, info); dlErr != nil {
							slog.Error("[updater] Download failed", "error", dlErr)
						}
					}
				}
				return nil
			},
		})
	}

	// Step 10: Connect to session gateway (only when platform reachable)
	if deviceToken != "" && platformReachable {
		tokenProvider := lifecycle.NewClient(cfg.PlatformURL, deviceID, deviceToken)
		wsClient := session.NewWSClient(cfg.WSURL, deviceID, tenantID, tokenProvider, registry, auditClient, policyEngine, diagnosisEngine, localStore)
		wsClient.Start(ctx)
	}

	// Expose updater to local API
	if b.updater != nil {
		localServer.SetUpdater(b.updater)
	}

	if !platformReachable {
		slog.Info("[boot] Bootstrap complete. Agent is running in OFFLINE mode (read-only only).")
	} else {
		slog.Info("[boot] Bootstrap complete. Agent is running.", "runtime_tasks", rt.TaskCount())
	}
	return nil
}

func (b *Bootstrapper) Shutdown(ctx context.Context) {
	slog.Info("[boot] Shutting down...")
	if b.runtime != nil {
		b.runtime.Stop()
	}
	if b.localServer != nil {
		if err := b.localServer.Stop(ctx); err != nil {
			slog.Error("[boot] Failed to stop local server", "error", err)
		}
	}
	if b.localStore != nil {
		if err := b.localStore.Close(); err != nil {
			slog.Error("[boot] Failed to close store", "error", err)
		}
	}
	slog.Info("[boot] Shutdown complete")
}

type remoteModelConfig struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	ModelName string `json:"model_name"`
	APIKey    string `json:"api_key"`
}

var providerFactory = map[string]func(router.ProviderConfig) router.Provider{
	"openai":     func(c router.ProviderConfig) router.Provider { return providers.NewOpenAIProvider(c) },
	"deepseek":   func(c router.ProviderConfig) router.Provider { return providers.NewDeepSeekProvider(c) },
	"anthropic":  func(c router.ProviderConfig) router.Provider { return providers.NewAnthropicProvider(c) },
	"gemini":     func(c router.ProviderConfig) router.Provider { return providers.NewGeminiProvider(c) },
	"openrouter": func(c router.ProviderConfig) router.Provider { return providers.NewOpenRouterProvider(c) },
	"ollama":     func(c router.ProviderConfig) router.Provider { return providers.NewOllamaProvider(c) },
}

func (b *Bootstrapper) initLLMRouter(remote *remoteModelConfig) *router.Router {
	llmRouter := router.NewRouter(600 * time.Second)

	// Priority 1: Remote model profile from platform (pushed via config API)
	if remote != nil {
		factory, ok := providerFactory[remote.Provider]
		if ok {
			p := factory(router.ProviderConfig{
				Name:    "remote-" + remote.Provider,
				BaseURL: remote.BaseURL,
				APIKey:  remote.APIKey,
				Model:   remote.ModelName,
			})
			llmRouter.RegisterProvider(p)
			llmRouter.SetPrimary("remote-" + remote.Provider)
			slog.Info("[boot] Registered remote model provider (from platform)", "provider", remote.Provider, "model", remote.ModelName)
		} else {
			slog.Warn("[boot] Unknown remote model provider, falling back to env config", "provider", remote.Provider)
		}
	}

	// Priority 2: Environment variable providers
	if apiKey := os.Getenv("ENX_OPENAI_API_KEY"); apiKey != "" {
		p := providers.NewOpenAIProvider(router.ProviderConfig{
			Name:    "openai",
			BaseURL: envOrDefault("ENX_OPENAI_BASE_URL", "https://api.openai.com/v1"),
			APIKey:  apiKey,
			Model:   envOrDefault("ENX_OPENAI_MODEL", "gpt-4o-mini"),
		})
		llmRouter.RegisterProvider(p)
		slog.Info("[boot] Registered OpenAI provider")
	}

	if apiKey := os.Getenv("ENX_OPENROUTER_API_KEY"); apiKey != "" {
		p := providers.NewOpenRouterProvider(router.ProviderConfig{
			Name:    "openrouter",
			BaseURL: envOrDefault("ENX_OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
			APIKey:  apiKey,
			Model:   envOrDefault("ENX_OPENROUTER_MODEL", ""),
		})
		llmRouter.RegisterProvider(p)
		slog.Info("[boot] Registered OpenRouter provider")
	}

	if apiKey := os.Getenv("ENX_DEEPSEEK_API_KEY"); apiKey != "" {
		p := providers.NewDeepSeekProvider(router.ProviderConfig{
			Name:    "deepseek",
			BaseURL: envOrDefault("ENX_DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
			APIKey:  apiKey,
			Model:   envOrDefault("ENX_DEEPSEEK_MODEL", "deepseek-chat"),
		})
		llmRouter.RegisterProvider(p)
		slog.Info("[boot] Registered DeepSeek provider")
	}

	if apiKey := os.Getenv("ENX_ANTHROPIC_API_KEY"); apiKey != "" {
		p := providers.NewAnthropicProvider(router.ProviderConfig{
			Name:    "anthropic",
			BaseURL: envOrDefault("ENX_ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
			APIKey:  apiKey,
			Model:   envOrDefault("ENX_ANTHROPIC_MODEL", "claude-sonnet-4-20250514"),
		})
		llmRouter.RegisterProvider(p)
		slog.Info("[boot] Registered Anthropic provider")
	}

	if apiKey := os.Getenv("ENX_GEMINI_API_KEY"); apiKey != "" {
		p := providers.NewGeminiProvider(router.ProviderConfig{
			Name:    "gemini",
			BaseURL: envOrDefault("ENX_GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
			APIKey:  apiKey,
			Model:   envOrDefault("ENX_GEMINI_MODEL", "gemini-2.0-flash"),
		})
		llmRouter.RegisterProvider(p)
		slog.Info("[boot] Registered Gemini provider")
	}

	// Priority 3: Ollama as local fallback
	ollamaURL := envOrDefault("ENX_OLLAMA_URL", "http://localhost:11434")
	p := providers.NewOllamaProvider(router.ProviderConfig{
		Name:    "ollama",
		BaseURL: ollamaURL,
		Model:   envOrDefault("ENX_OLLAMA_MODEL", "llama3.2"),
	})
	llmRouter.RegisterProvider(p)
	slog.Info("[boot] Registered Ollama provider (fallback)")

	if primary := os.Getenv("ENX_LLM_PRIMARY"); primary != "" {
		llmRouter.SetPrimary(primary)
	}

	slog.Info("[boot] LLM router initialized", "providers", llmRouter.ListProviders())
	return llmRouter
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func countReadOnly(registry *tools.Registry) int {
	count := 0
	for _, t := range registry.List() {
		if t.IsReadOnly() {
			count++
		}
	}
	return count
}
