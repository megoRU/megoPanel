package main

import (
	"log/slog"
	"os"

	"mego-panel/backend/internal/app"
	"mego-panel/backend/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	configPath := os.Getenv("MEGOPANEL_CONFIG")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}
	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("application bootstrap failed", "error", err)
		os.Exit(1)
	}
	if err := application.Run(); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
