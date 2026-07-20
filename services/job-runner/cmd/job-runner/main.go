package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/zayeagle/envnexus/services/job-runner/internal/infrastructure"
	"github.com/zayeagle/envnexus/services/job-runner/internal/worker"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Injected at link time, e.g. -X main.buildRevision=$(git rev-parse HEAD)
var buildRevision = "dev"

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	dsn := os.Getenv("ENX_DATABASE_DSN")
	healthPort := envOrDefault("ENX_HEALTH_PORT", "8082")

	var db *gorm.DB
	if dsn != "" {
		var err error
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			slog.Error("Failed to connect to database", "error", err)
			os.Exit(1)
		}
		slog.Info("Connected to MySQL database")
	} else {
		slog.Info("Warning: ENX_DATABASE_DSN not set, workers requiring DB will be disabled")
	}

	// --- MinIO ---
	var minioClient *infrastructure.MinIOClient
	minioEndpoint := os.Getenv("ENX_OBJECT_STORAGE_ENDPOINT")
	if minioEndpoint != "" {
		minioAccessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
		minioSecretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin")
		minioBucket := envOrDefault("ENX_OBJECT_STORAGE_BUCKET", "envnexus")
		var err error
		minioClient, err = infrastructure.NewMinIOClient(minioEndpoint, minioAccessKey, minioSecretKey, minioBucket, false)
		if err != nil {
			slog.Warn("MinIO connection failed, audit archival will skip upload", "error", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	allWorkers := []string{"token_cleanup", "link_cleanup", "audit_flush", "session_cleanup", "approval_expiry", "package_build", "governance_scan"}

	if db != nil {
		var mc *minio.Client
		var bucket string
		if minioClient != nil {
			mc = minioClient.Client()
			bucket = minioClient.BucketName()
		}
		go worker.NewTokenCleanupWorker(db).Start(ctx)
		go worker.NewLinkCleanupWorker(db).Start(ctx)
		go worker.NewAuditFlushWorker(db, mc, bucket).Start(ctx)
		go worker.NewSessionCleanupWorker(db).Start(ctx)
		go worker.NewApprovalExpiryWorker(db).Start(ctx)
		go worker.NewPackageBuildWorker(db, mc, bucket).Start(ctx)
		go worker.NewGovernanceScanWorker(db).Start(ctx)
	}

	router := gin.Default()
	router.GET("/healthz", func(c *gin.Context) {
		status := "ok"
		if db == nil {
			status = "degraded (no database)"
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   status,
			"revision": buildRevision,
			"workers":  allWorkers,
		})
	})

	server := &http.Server{
		Addr:    ":" + healthPort,
		Handler: router,
	}

	go func() {
		slog.Info("Starting job-runner health server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down job-runner")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("job-runner exited")
}
