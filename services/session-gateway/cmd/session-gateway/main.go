package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/session-gateway/internal/handler/ws"
)

// Injected at link time, e.g. -X main.buildRevision=$(git rev-parse HEAD)
var buildRevision = "dev"

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envRequired(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable is not set", "env_key", key)
		os.Exit(1)
	}
	return v
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	port := envOrDefault("ENX_HTTP_PORT", "8081")
	redisAddr := envOrDefault("ENX_REDIS_ADDR", "localhost:6379")
	redisPassword := os.Getenv("ENX_REDIS_PASSWORD")
	sessionTokenSecret := envRequired("ENX_SESSION_TOKEN_SECRET")
	allowedOrigins := envOrDefault("ENX_CORS_ALLOWED_ORIGINS", "http://localhost:3000")

	platformURL := envOrDefault("ENX_PLATFORM_URL", "http://localhost:8080")

	origins := strings.Split(allowedOrigins, ",")
	sessionManager := ws.NewSessionManager(sessionTokenSecret, origins)
	sessionManager.SetPlatformURL(platformURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rc, err := ws.NewRedisClient(redisAddr, redisPassword, 0)
	if err != nil {
		slog.Info("Redis connection failed, running without pub/sub", "redis_addr", redisAddr, "error", err)
	} else {
		sessionManager.SetRedisClient(rc)
		go rc.SubscribeSessionEvents(ctx)
		slog.Info("Connected to Redis", "redis_addr", redisAddr)
	}

	router := gin.Default()

	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	router.GET("/readyz", func(c *gin.Context) {
		status := "ready"
		redisOK := false
		if rc != nil {
			if err := rc.Ping(c.Request.Context()); err == nil {
				redisOK = true
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"status":         status,
			"revision":       buildRevision,
			"online_devices": sessionManager.GetOnlineDeviceCount(),
			"redis":          redisOK,
		})
	})

	router.GET("/ws/v1/sessions/:deviceId", sessionManager.HandleAgentConnection)
	router.POST("/api/v1/sessions/command", sessionManager.HandleCommand)
	router.POST("/api/v1/sessions/:sessionId/events", sessionManager.HandleSessionEvent)
	router.POST("/api/v1/devices/:deviceId/events", sessionManager.HandleDeviceEvent)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		slog.Info("Starting session-gateway", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down session-gateway...")

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if rc != nil {
		rc.Close()
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("Session-gateway exited")
}
