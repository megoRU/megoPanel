package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/platform"
	"mego-panel/backend/internal/repository"
)

type AuthService struct {
	admins   repository.AdminRepository
	settings repository.SettingRepository
	cfg      *config.Config
}

func NewAuthService(admins repository.AdminRepository, settings repository.SettingRepository, cfg *config.Config) *AuthService {
	return &AuthService{admins: admins, settings: settings, cfg: cfg}
}
func (s *AuthService) IsConfigured() (bool, error) {
	count, err := s.admins.Count()
	return count > 0, err
}
func (s *AuthService) CreateAdmin(username string, password string) error {
	if len(password) < 8 {
		return errors.New("password must contain at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.admins.Create(&domain.Admin{Username: username, PasswordHash: string(hash)}); err != nil {
		return err
	}
	return s.settings.Set("configured", "true")
}
func (s *AuthService) Login(username string, password string) (string, string, error) {
	admin, err := s.admins.ByUsername(username)
	if err != nil || admin == nil {
		return "", "", errors.New("invalid credentials")
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)) != nil {
		return "", "", errors.New("invalid credentials")
	}
	access, err := s.token(admin.ID, s.cfg.AccessTTL())
	if err != nil {
		return "", "", err
	}
	refresh, err := randomToken()
	return access, refresh, err
}
func (s *AuthService) token(id uint, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{"sub": strconv.Itoa(int(id)), "exp": time.Now().Add(ttl).Unix(), "iat": time.Now().Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.Security.JWTSecret))
}
func (s *AuthService) Verify(tokenString string) error {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) { return []byte(s.cfg.Security.JWTSecret), nil })
	if err != nil || !token.Valid {
		return errors.New("unauthorized")
	}
	return nil
}
func randomToken() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	return hex.EncodeToString(bytes), err
}

type DashboardService struct{}

func NewDashboardService() *DashboardService { return &DashboardService{} }
func (s *DashboardService) Stats() domain.DashboardStats {
	hostname, _ := os.Hostname()
	osVersion := readFirst("/etc/os-release", "PRETTY_NAME=")
	return domain.DashboardStats{CPUUsage: sampleCPU(), RAMUsage: sampleRAM(), DiskUsage: sampleDisk(), Uptime: readUptime(), OSVersion: osVersion, Hostname: hostname}
}
func readFirst(path string, prefix string) string {
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), "\"")
		}
	}
	return runtime.GOOS
}
func readUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "unknown"
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	return (time.Duration(seconds) * time.Second).Round(time.Second).String()
}
func sampleCPU() float64 {
	data, _ := os.ReadFile("/proc/loadavg")
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
	return value * 10
}
func sampleRAM() float64 {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return float64(mem.Sys % 100)
}
func sampleDisk() float64 { return 42 }

type InstallService struct {
	repo repository.ServiceRepository
	pm   platform.PackageManager
}

func NewInstallService(repo repository.ServiceRepository, pm platform.PackageManager) *InstallService {
	return &InstallService{repo: repo, pm: pm}
}
func (s *InstallService) Status(name string) (*domain.ServiceState, error) { return s.repo.Get(name) }
func (s *InstallService) InstallMariaDB(remote bool) (*domain.ServiceState, error) {
	if err := s.pm.Install("mariadb-server"); err != nil {
		return nil, err
	}
	if remote {
		_ = os.WriteFile("/etc/mysql/mariadb.conf.d/60-megopanel.cnf", []byte("[mysqld]\nbind-address=0.0.0.0\n"), 0644)
	}
	_ = s.pm.Enable("mariadb")
	_ = s.pm.Restart("mariadb")
	state := &domain.ServiceState{Name: "mariadb", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}
func (s *InstallService) InstallNginx() (*domain.ServiceState, error) {
	if err := s.pm.Install("nginx"); err != nil {
		return nil, err
	}
	_ = s.pm.Enable("nginx")
	state := &domain.ServiceState{Name: "nginx", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}
