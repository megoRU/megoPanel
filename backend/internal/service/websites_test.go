package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"mego-panel/backend/internal/domain"
)

func TestWebsiteCreateAndDeleteManagesNginxAndPreservesFiles(t *testing.T) {
	service, root, available, enabled := newWebsiteTestService(t, "#!/bin/sh\nexit 0\n")
	website, err := service.Create(context.Background(), "example.com", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if website.Path != filepath.Join(root, "example.com") {
		t.Fatalf("unexpected website path: %s", website.Path)
	}
	configPath := filepath.Join(available, "example.com.conf")
	config, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(config), "server_name example.com;") {
		t.Fatalf("expected managed Nginx server block, got %q, error %v", config, err)
	}
	if target, err := os.Readlink(filepath.Join(enabled, "example.com.conf")); err != nil || target != configPath {
		t.Fatalf("expected enabled site to link to its config, got %q, error %v", target, err)
	}
	index := filepath.Join(website.Path, "index.html")
	if _, err := os.Stat(index); err != nil {
		t.Fatalf("expected a starter page: %v", err)
	}
	if err := service.Delete(context.Background(), uint64(website.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("managed Nginx config should be removed")
	}
	if _, err := os.Stat(index); err != nil {
		t.Fatalf("website files should be preserved on removal: %v", err)
	}
	var count int64
	if err := service.db.Model(&domain.Website{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("website record should be removed, count %d, error %v", count, err)
	}
}

func TestWebsiteCreateRollsBackWhenNginxRejectsConfig(t *testing.T) {
	service, root, available, enabled := newWebsiteTestService(t, "#!/bin/sh\necho invalid config >&2\nexit 1\n")
	if _, err := service.Create(context.Background(), "example.com", "192.0.2.10"); err == nil {
		t.Fatal("expected invalid Nginx configuration to fail")
	}
	for _, path := range []string{
		filepath.Join(available, "example.com.conf"),
		filepath.Join(enabled, "example.com.conf"),
		filepath.Join(root, "example.com", "index.html"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("failed creation left behind %s", path)
		}
	}
}

func TestWebsiteCreateRejectsPhpMyAdminReservedDirectory(t *testing.T) {
	service, _, _, _ := newWebsiteTestService(t, "#!/bin/sh\nexit 0\n")
	if _, err := service.Create(context.Background(), "phpmyadmin", "192.0.2.10"); err == nil {
		t.Fatal("expected the phpMyAdmin path to be reserved")
	}
}

func TestNginxNameMatches(t *testing.T) {
	tests := []struct {
		configured string
		hostname   string
		want       bool
	}{
		{configured: "example.com", hostname: "example.com", want: true},
		{configured: "*.example.com", hostname: "shop.example.com", want: true},
		{configured: "*.example.com", hostname: "example.com", want: false},
		{configured: ".example.com", hostname: "example.com", want: true},
		{configured: ".example.com", hostname: "shop.example.com", want: true},
		{configured: "other.example.com", hostname: "example.com", want: false},
	}
	for _, test := range tests {
		if got := nginxNameMatches(test.configured, test.hostname); got != test.want {
			t.Errorf("nginxNameMatches(%q, %q) = %v, want %v", test.configured, test.hostname, got, test.want)
		}
	}
}

func newWebsiteTestService(t *testing.T, nginxScript string) (*WebsiteService, string, string, string) {
	t.Helper()
	root := t.TempDir()
	webRoot := filepath.Join(root, "www")
	available := filepath.Join(root, "sites-available")
	enabled := filepath.Join(root, "sites-enabled")
	bin := filepath.Join(root, "bin")
	for _, path := range []string{webRoot, available, enabled, bin} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	nginx := filepath.Join(bin, "nginx")
	systemctl := filepath.Join(bin, "systemctl")
	if err := os.WriteFile(nginx, []byte(nginxScript), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemctl, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Website{}); err != nil {
		t.Fatal(err)
	}
	return newWebsiteService(db, webRoot, available, enabled, nginx, systemctl), webRoot, available, enabled
}
