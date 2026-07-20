package main

import (
	"log/slog"
	"os"

	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/migrations"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	dsn := os.Getenv("ENX_DATABASE_DSN")
	if dsn == "" {
		slog.Error("ENX_DATABASE_DSN is required")
		os.Exit(1)
	}

	db, err := repository.NewDB(dsn)
	if err != nil {
		slog.Error("database connect failed", "error", err)
		os.Exit(1)
	}

	if err := migrations.Run(db); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations complete")
}
