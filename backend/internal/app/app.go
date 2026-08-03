package app

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"mego-panel/backend/internal/config"
	api "mego-panel/backend/internal/http"
	"mego-panel/backend/internal/platform"
	"mego-panel/backend/internal/repository"
	"mego-panel/backend/internal/service"
	"strings"
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
	autologin := service.NewAutologinStore()

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

	router := api.NewRouter(api.RouterDeps{Config: cfg, Auth: auth, Dashboard: dashboard, Install: install, Update: update, DB: db, Autologin: autologin})
	return &App{cfg: cfg, logger: logger, router: router}, nil
}
func (a *App) Run() error {
	a.logger.Info("starting server", "address", a.cfg.Server.Address)
	return a.router.Run(a.cfg.Server.Address)
}

type noopPackageManager struct{}

func (n *noopPackageManager) Restart(string) error { return nil }
func (n *noopPackageManager) Enable(string) error  { return nil }
