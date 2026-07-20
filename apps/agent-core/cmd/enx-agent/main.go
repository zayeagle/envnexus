package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/bootstrap"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/config"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/setup"
)

var version = "0.3.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			handleSetup()
			return
		case "version":
			fmt.Printf("enx-agent %s\n", version)
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	// On Windows, if double-clicked (no existing config), launch GUI setup then start agent
	if runtime.GOOS == "windows" && len(os.Args) == 1 && !hasExistingConfig() {
		setup.RunGUI(getConfigDir())
	}

	runAgent()
}

func printUsage() {
	fmt.Printf(`enx-agent %s — EnvNexus Agent Core

Usage:
  enx-agent                  Start the agent (default)
  enx-agent setup            Interactive setup wizard (CLI)
  enx-agent setup --gui      Graphical setup wizard (opens browser)
  enx-agent version          Show version
  enx-agent help             Show this help

Flags (for default run mode):
  --platform-url <url>       Platform API URL (overrides config)
  --ws-url <url>             WebSocket gateway URL (overrides config)
  --activation-mode <mode>   Activation mode: auto, manual, both
  --activation-key <key>     Activation key (for auto/both mode)
  --data-dir <path>          Data/config directory (default: ~/.envnexus/agent)

Config priority (highest to lowest):
  1. CLI flags (--platform-url, etc.)
  2. Saved config (agent.enx in data dir)
  3. Bundled agent.enx (from download package)
  4. Injected config (binary-embedded)
  5. Environment variables (ENX_PLATFORM_URL, etc.)
  6. Default values

`, version)
}

func getConfigDir() string {
	return bootstrap.DefaultConfigDir()
}

func hasExistingConfig() bool {
	dir := getConfigDir()
	if _, err := os.Stat(filepath.Join(dir, "agent.enx")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "agent_config.json")); err == nil {
		return true
	}
	return false
}

func handleSetup() {
	useGUI := false
	if len(os.Args) > 2 {
		for _, arg := range os.Args[2:] {
			if arg == "--gui" || arg == "-gui" {
				useGUI = true
			}
		}
	}

	// On Windows, default to GUI mode for setup
	if runtime.GOOS == "windows" && !useGUI {
		for _, arg := range os.Args[2:] {
			if arg == "--cli" || arg == "-cli" {
				useGUI = false
				break
			}
		}
		if len(os.Args) == 2 {
			useGUI = true
		}
	}

	if useGUI {
		setup.RunGUI(getConfigDir())
	} else {
		runCLISetup()
	}
}

func runCLISetup() {
	configDir := getConfigDir()
	mgr := config.NewManager(configDir)
	cfg := mgr.Get()

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         EnvNexus Agent Setup Wizard          ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	cfg.PlatformURL = prompt(reader, "Platform API URL", cfg.PlatformURL)
	cfg.WSURL = prompt(reader, "WebSocket Gateway URL", cfg.WSURL)
	cfg.ActivationMode = prompt(reader, "Activation Mode (auto/manual/both, empty to skip)", cfg.ActivationMode)
	if cfg.ActivationMode == "auto" || cfg.ActivationMode == "both" {
		cfg.ActivationKey = prompt(reader, "Activation Key", cfg.ActivationKey)
	}
	cfg.EnrollmentToken = prompt(reader, "Enrollment Token (optional)", cfg.EnrollmentToken)

	mgr.Update(func(c *config.AgentConfig) {
		c.PlatformURL = cfg.PlatformURL
		c.WSURL = cfg.WSURL
		c.ActivationMode = cfg.ActivationMode
		c.ActivationKey = cfg.ActivationKey
		c.EnrollmentToken = cfg.EnrollmentToken
	})

	fmt.Println()
	fmt.Printf("Configuration saved to: %s\n", filepath.Join(configDir, "agent.enx"))
	fmt.Println()
	fmt.Println("You can now start the agent with:")
	fmt.Println("  enx-agent")
	fmt.Println()
	fmt.Println("Or override any setting at runtime:")
	fmt.Println("  enx-agent --platform-url http://192.168.1.100:8080")
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func runAgent() {
	fs := flag.NewFlagSet("enx-agent", flag.ExitOnError)
	platformURL := fs.String("platform-url", "", "Platform API URL")
	wsURL := fs.String("ws-url", "", "WebSocket gateway URL")
	activationMode := fs.String("activation-mode", "", "Activation mode: auto, manual, both")
	activationKey := fs.String("activation-key", "", "Activation key")
	dataDir := fs.String("data-dir", "", "Data/config directory (overrides default ~/.envnexus/agent)")
	_ = fs.Parse(os.Args[1:])

	overrides := config.CLIOverrides{
		PlatformURL:    *platformURL,
		WSURL:          *wsURL,
		ActivationMode: *activationMode,
		ActivationKey:  *activationKey,
		DataDir:        *dataDir,
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("Starting enx-agent core...", "version", version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bootstrapper := bootstrap.NewBootstrapper(version)
	if overrides.DataDir != "" {
		bootstrapper.SetDataDir(overrides.DataDir)
	}
	bootstrapper.ApplyCLIOverrides(overrides)

	if err := bootstrapper.Run(ctx); err != nil {
		slog.Error("Bootstrap failed", "error", err)
		os.Exit(1)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down enx-agent...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	bootstrapper.Shutdown(shutdownCtx)

	slog.Info("enx-agent exiting")
}
