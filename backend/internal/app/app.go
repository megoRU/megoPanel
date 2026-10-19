package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"mego-panel/backend/internal/config"
	api "mego-panel/backend/internal/http"
	"mego-panel/backend/internal/platform"
	"mego-panel/backend/internal/repository"
	"mego-panel/backend/internal/service"
)

type App struct {
	cfg    *config.Config
	logger *slog.Logger
	router *gin.Engine
}

func New(cfg *config.Config, logger *slog.Logger) (*App, error) {
	db, err := platform.OpenDatabase(cfg.Database.Path)
	if err != nil {
		return nil, err
	}
	pm, err := platform.DetectPackageManager()
	if err != nil {
		logger.Warn("package manager unavailable", "error", err)
		pm = &noopPackageManager{}
	}
	admins := repository.NewAdminRepository(db)
	settings := repository.NewSettingRepository(db)
	services := repository.NewServiceRepository(db)
	auth := service.NewAuthService(admins, settings, cfg)
	dashboard := service.NewDashboardService()
	install := service.NewInstallService(services, pm, settings)
	update := service.NewUpdateService()
	configPath := os.Getenv("MEGOPANEL_CONFIG")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	databasePath := cfg.Database.Path
	if !filepath.IsAbs(databasePath) {
		databasePath, err = filepath.Abs(databasePath)
		if err != nil {
			return nil, err
		}
	}
	backups := service.NewBackupService(db, settings, databasePath, configPath)
	backups.Start(context.Background())
	websites := service.NewWebsiteService(db)
	autologin := service.NewAutologinStore()
	if err := install.EnsureMariaDBAdmin(context.Background()); err != nil {
		logger.Warn("MegoPanel MariaDB account is not ready", "error", err)
	}

	// Automatically ensure phpMyAdmin autologin files are present and updated on startup
	port := "8888"
	if cfg != nil {
		if parts := strings.Split(cfg.Server.Address, ":"); len(parts) > 0 && parts[len(parts)-1] != "" {
			port = parts[len(parts)-1]
		}
	}
	if err := install.EnsureAutologinFiles(port); err != nil {
		logger.Warn("failed to ensure phpmyadmin autologin files on startup", "error", err)
	}

	router := api.NewRouter(api.RouterDeps{Config: cfg, Auth: auth, Dashboard: dashboard, Install: install, Update: update, Backups: backups, Websites: websites, DB: db, Autologin: autologin})
	return &App{cfg: cfg, logger: logger, router: router}, nil
}
func (a *App) Run() error {
	a.logger.Info("starting server", "address", a.cfg.Server.Address)
	return a.router.Run(a.cfg.Server.Address)
}

type noopPackageManager struct{}

func (n *noopPackageManager) Restart(string) error { return errors.New("system service manager is unavailable") }
func (n *noopPackageManager) Enable(string) error  { return errors.New("system service manager is unavailable") }
