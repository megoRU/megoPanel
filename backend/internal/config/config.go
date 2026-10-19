package config

import (
	"errors"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Environment string         `yaml:"environment"`
	Server      ServerConfig   `yaml:"server"`
	Database    DatabaseConfig `yaml:"database"`
	Security    SecurityConfig `yaml:"security"`
}
type ServerConfig struct {
	Address     string `yaml:"address"`
	FrontendURL string `yaml:"frontend_url"`
}
type DatabaseConfig struct {
	Path string `yaml:"path"`
}
type SecurityConfig struct {
	JWTSecret        string `yaml:"jwt_secret"`
	AccessTTLMinutes int    `yaml:"access_ttl_minutes"`
	RefreshTTLHours  int    `yaml:"refresh_ttl_hours"`
	CookieSecure     bool   `yaml:"cookie_secure"`
	CSRFSecret       string `yaml:"csrf_secret"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Server.Address == "" {
		cfg.Server.Address = ":8888"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "panel.db"
	}
	if cfg.Security.AccessTTLMinutes <= 0 {
		cfg.Security.AccessTTLMinutes = 15
	}
	if cfg.Security.RefreshTTLHours <= 0 {
		cfg.Security.RefreshTTLHours = 168
	}
	if len(cfg.Security.JWTSecret) < 32 || strings.Contains(strings.ToLower(cfg.Security.JWTSecret), "change-me") {
		return nil, errors.New("security.jwt_secret must be set to a random value of at least 32 bytes")
	}
	if len(cfg.Security.CSRFSecret) < 32 || strings.Contains(strings.ToLower(cfg.Security.CSRFSecret), "change-me") {
		return nil, errors.New("security.csrf_secret must be set to a random value of at least 32 bytes")
	}
	return &cfg, nil
}
func (c *Config) AccessTTL() time.Duration {
	return time.Duration(c.Security.AccessTTLMinutes) * time.Minute
}
func (c *Config) RefreshTTL() time.Duration {
	return time.Duration(c.Security.RefreshTTLHours) * time.Hour
}
func (c *Config) IsProduction() bool { return c.Environment == "production" }
