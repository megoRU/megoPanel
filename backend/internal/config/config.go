package config

import (
	"gopkg.in/yaml.v3"
	"os"
	"time"
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
		cfg.Server.Address = ":8080"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "panel.db"
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
