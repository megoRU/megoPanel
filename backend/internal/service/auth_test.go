package service

import (
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/repository"
)

func TestRefreshTokenRotatesAndIsSingleUse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Admin{}, &domain.Setting{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Security: config.SecurityConfig{JWTSecret: "test-secret-with-at-least-32-bytes", AccessTTLMinutes: 15, RefreshTTLHours: 1}}
	auth := NewAuthService(repository.NewAdminRepository(db), repository.NewSettingRepository(db), cfg)
	auth.setupTokenPath = filepath.Join(t.TempDir(), "setup.token")
	if err := os.WriteFile(auth.setupTokenPath, []byte("test-setup-token"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := auth.CreateAdmin("admin", "StrongPassword12!", "test-setup-token"); err != nil {
		t.Fatal(err)
	}
	access, refresh, err := auth.Login("admin", "StrongPassword12!")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Verify(access); err != nil {
		t.Fatalf("login access token should be valid: %v", err)
	}
	rotatedAccess, rotatedRefresh, err := auth.Refresh(refresh)
	if err != nil {
		t.Fatalf("refresh should rotate the session: %v", err)
	}
	if err := auth.Verify(rotatedAccess); err != nil {
		t.Fatalf("rotated access token should be valid: %v", err)
	}
	if _, _, err := auth.Refresh(refresh); err == nil {
		t.Fatal("old refresh token must not be reusable")
	}
	auth.RevokeRefresh(rotatedRefresh)
	if _, _, err := auth.Refresh(rotatedRefresh); err == nil {
		t.Fatal("revoked refresh token must not be reusable")
	}
	if _, _, err := auth.Refresh("unknown"); err == nil {
		t.Fatal("unknown refresh token must be rejected")
	}
	if len(auth.refreshSessions) != 0 {
		t.Fatalf("expected no retained sessions, got %d", len(auth.refreshSessions))
	}
}
