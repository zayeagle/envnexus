package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/config"
)

// RunGUI starts a local HTTP server serving the setup wizard UI and opens it in the default browser.
// It blocks until the user completes or cancels setup.
func RunGUI(configDir string) {
	mgr := config.NewManager(configDir)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(setupHTML))
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			cfg := mgr.Get()
			json.NewEncoder(w).Encode(cfg)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/api/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			PlatformURL    string `json:"platform_url"`
			WSURL          string `json:"ws_url"`
			ActivationMode string `json:"activation_mode"`
			ActivationKey  string `json:"activation_key"`
			EnrollmentToken string `json:"enrollment_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mgr.Update(func(c *config.AgentConfig) {
			if req.PlatformURL != "" {
				c.PlatformURL = req.PlatformURL
			}
			if req.WSURL != "" {
				c.WSURL = req.WSURL
			}
			c.ActivationMode = req.ActivationMode
			c.ActivationKey = req.ActivationKey
			if req.EnrollmentToken != "" {
				c.EnrollmentToken = req.EnrollmentToken
			}
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"message": "Configuration saved successfully",
		})
	})

	doneCh := make(chan struct{})
	mux.HandleFunc("/api/done", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		go func() {
			time.Sleep(500 * time.Millisecond)
			close(doneCh)
		}()
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("[setup] Failed to start setup server", "error", err)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("[setup] Server error", "error", err)
		}
	}()

	fmt.Printf("EnvNexus Agent Setup Wizard running at: %s\n", url)
	openBrowser(url)

	<-doneCh

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	fmt.Println("Setup complete. Starting agent...")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("[setup] Could not open browser automatically", "error", err, "url", url)
		fmt.Printf("Please open your browser and navigate to: %s\n", url)
	}
}
