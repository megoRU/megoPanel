package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/repository"
	"mego-panel/backend/internal/service"
)

func TestPMAInternalTokenRequiresLoopbackAndConsumesOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:autologin-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Admin{}, &domain.Setting{}, &domain.ServiceState{}, &domain.Website{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Server: config.ServerConfig{FrontendURL: "http://localhost"}, Security: config.SecurityConfig{JWTSecret: "test-secret"}}
	auth := service.NewAuthService(repository.NewAdminRepository(db), repository.NewSettingRepository(db), cfg)
	store := service.NewAutologinStore()
	store.Save("3ec26e32-44b1-7e5a-a024-3e07e97f0001", &service.PMAAutologinToken{
		UserID: "1", MySQLUsername: "megopanel", MySQLPassword: "database-secret", ExpiresAt: time.Now().Add(time.Minute),
	})
	router := NewRouter(RouterDeps{Config: cfg, Auth: auth, DB: db, Autologin: store})

	spoofedReq := httptest.NewRequest(http.MethodPost, "/internal/phpmyadmin/token", strings.NewReader(`{"token":"3ec26e32-44b1-7e5a-a024-3e07e97f0001"}`))
	spoofedReq.RemoteAddr = "203.0.113.10:1234"
	spoofedReq.Header.Set("Content-Type", "application/json")
	spoofedReq.Header.Set("X-Forwarded-For", "127.0.0.1")
	spoofed := httptest.NewRecorder()
	router.ServeHTTP(spoofed, spoofedReq)
	if spoofed.Code != http.StatusForbidden {
		t.Fatalf("spoofed forwarded address should be rejected, got %d", spoofed.Code)
	}

	localReq := httptest.NewRequest(http.MethodPost, "/internal/phpmyadmin/token", strings.NewReader(`{"token":"3ec26e32-44b1-7e5a-a024-3e07e97f0001"}`))
	local := httptest.NewRecorder()
	localReq.RemoteAddr = "127.0.0.1:5678"
	localReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(local, localReq)
	if local.Code != http.StatusOK || !strings.Contains(local.Body.String(), `"password":"database-secret"`) {
		t.Fatalf("loopback should receive credentials exactly once, got %d: %s", local.Code, local.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/internal/phpmyadmin/token", strings.NewReader(`{"token":"3ec26e32-44b1-7e5a-a024-3e07e97f0001"}`))
	second := httptest.NewRecorder()
	secondReq.RemoteAddr = "127.0.0.1:5678"
	secondReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(second, secondReq)
	if second.Code != http.StatusForbidden {
		t.Fatalf("a consumed token should be rejected, got %d", second.Code)
	}
}

func TestPMAInternalTokenRouteDoesNotAcceptTokenInQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:autologin-query-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Admin{}, &domain.Setting{}, &domain.ServiceState{}, &domain.Website{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Server: config.ServerConfig{FrontendURL: "http://localhost"}, Security: config.SecurityConfig{JWTSecret: "test-secret"}}
	auth := service.NewAuthService(repository.NewAdminRepository(db), repository.NewSettingRepository(db), cfg)
	router := NewRouter(RouterDeps{Config: cfg, Auth: auth, DB: db, Autologin: service.NewAutologinStore()})
	req := httptest.NewRequest(http.MethodGet, "/internal/phpmyadmin/token?token=3ec26e32-44b1-7e5a-a024-3e07e97f0001", nil)
	req.RemoteAddr = "127.0.0.1:5678"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if strings.Contains(response.Body.String(), "database-secret") {
		t.Fatal("GET query requests must never expose sign-on credentials")
	}
}
