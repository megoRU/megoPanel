package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/platform"
	"mego-panel/backend/internal/repository"
)

type AuthService struct {
	admins         repository.AdminRepository
	settings       repository.SettingRepository
	cfg            *config.Config
	setupMu        sync.Mutex
	refreshMu      sync.Mutex
	refreshSessions map[string]refreshSession
	setupTokenPath string
}

type refreshSession struct {
	adminID   uint
	expiresAt time.Time
}

func NewAuthService(admins repository.AdminRepository, settings repository.SettingRepository, cfg *config.Config) *AuthService {
	setupTokenPath := os.Getenv("MEGOPANEL_SETUP_TOKEN_FILE")
	if setupTokenPath == "" {
		setupTokenPath = "/etc/megopanel/setup.token"
	}
	return &AuthService{admins: admins, settings: settings, cfg: cfg, refreshSessions: make(map[string]refreshSession), setupTokenPath: setupTokenPath}
}
func (s *AuthService) IsConfigured() (bool, error) {
	count, err := s.admins.Count()
	return count > 0, err
}
func (s *AuthService) CreateAdmin(username string, password string, suppliedSetupToken string) error {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 80 {
		return errors.New("username must contain 1 to 80 characters")
	}
	if len(password) < 12 {
		return errors.New("password must contain at least 12 characters")
	}
	count, err := s.admins.Count()
	if err != nil {
		return fmt.Errorf("check administrator setup: %w", err)
	}
	if count > 0 {
		return errors.New("administrator setup is already complete")
	}
	setupToken, err := os.ReadFile(s.setupTokenPath)
	expectedSetupToken := strings.TrimSpace(string(setupToken))
	if err != nil || expectedSetupToken == "" || len(expectedSetupToken) != len(suppliedSetupToken) ||
		subtle.ConstantTimeCompare([]byte(expectedSetupToken), []byte(suppliedSetupToken)) != 1 {
		return errors.New("invalid one-time setup token")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.admins.Create(&domain.Admin{Username: username, PasswordHash: string(hash)}); err != nil {
		return err
	}
	if err := s.settings.Set("configured", "true"); err != nil {
		return err
	}
	_ = os.Remove(s.setupTokenPath)
	return nil
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
	refresh, err := s.createRefresh(admin.ID)
	return access, refresh, err
}

func (s *AuthService) Refresh(refreshToken string) (string, string, error) {
	if refreshToken == "" {
		return "", "", errors.New("unauthorized")
	}
	hash := sha256.Sum256([]byte(refreshToken))
	key := hex.EncodeToString(hash[:])
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	session, ok := s.refreshSessions[key]
	delete(s.refreshSessions, key)
	if !ok || time.Now().After(session.expiresAt) {
		return "", "", errors.New("unauthorized")
	}
	access, err := s.token(session.adminID, s.cfg.AccessTTL())
	if err != nil {
		return "", "", err
	}
	rotated, err := s.createRefreshLocked(session.adminID)
	return access, rotated, err
}

func (s *AuthService) RevokeRefresh(refreshToken string) {
	if refreshToken == "" {
		return
	}
	hash := sha256.Sum256([]byte(refreshToken))
	s.refreshMu.Lock()
	delete(s.refreshSessions, hex.EncodeToString(hash[:]))
	s.refreshMu.Unlock()
}

func (s *AuthService) createRefresh(adminID uint) (string, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.createRefreshLocked(adminID)
}

func (s *AuthService) createRefreshLocked(adminID uint) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(token))
	now := time.Now()
	for key, session := range s.refreshSessions {
		if now.After(session.expiresAt) {
			delete(s.refreshSessions, key)
		}
	}
	s.refreshSessions[hex.EncodeToString(hash[:])] = refreshSession{adminID: adminID, expiresAt: now.Add(s.cfg.RefreshTTL())}
	return token, nil
}
func (s *AuthService) token(id uint, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{"sub": strconv.Itoa(int(id)), "exp": time.Now().Add(ttl).Unix(), "iat": time.Now().Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.Security.JWTSecret))
}
func (s *AuthService) Verify(tokenString string) error {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.cfg.Security.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
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

func getRAMUsage() (used float64, total float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var memTotal, memAvailable float64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseFloat(fields[1], 64)
				memTotal = val / (1024 * 1024) // Convert kB to GB
			}
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseFloat(fields[1], 64)
				memAvailable = val / (1024 * 1024) // Convert kB to GB
			}
		}
	}
	if memTotal == 0 {
		return 0, 0
	}
	if memAvailable == 0 {
		// fallback using MemFree
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemFree:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					val, _ := strconv.ParseFloat(fields[1], 64)
					memAvailable = val / (1024 * 1024)
				}
			}
		}
	}
	used = memTotal - memAvailable
	return used, memTotal
}

func getDiskUsage() (used float64, total float64) {
	var stat syscall.Statfs_t
	err := syscall.Statfs("/", &stat)
	if err != nil {
		return 0, 0
	}
	all := float64(stat.Blocks) * float64(stat.Bsize)
	free := float64(stat.Bfree) * float64(stat.Bsize)
	used = all - free
	return used / (1024 * 1024 * 1024), all / (1024 * 1024 * 1024)
}

func findPHPFpmSocket(phpServiceName string) string {
	paths := []string{"/run/php", "/var/run/php"}
	for _, p := range paths {
		files, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasPrefix(f.Name(), "php") && strings.HasSuffix(f.Name(), "-fpm.sock") {
				return p + "/" + f.Name()
			}
		}
	}
	// Try to construct from phpServiceName (e.g. php8.3-fpm -> /run/php/php8.3-fpm.sock)
	if strings.HasPrefix(phpServiceName, "php") && strings.HasSuffix(phpServiceName, "-fpm") {
		return "/run/php/" + phpServiceName + ".sock"
	}
	return "/run/php/php-fpm.sock" // fallback
}

func (s *DashboardService) Stats() domain.DashboardStats {
	hostname, _ := os.Hostname()
	osVersion := readFirst("/etc/os-release", "PRETTY_NAME=")
	ramUsed, ramTotal := getRAMUsage()
	diskUsed, diskTotal := getDiskUsage()
	var ramPct, diskPct float64
	if ramTotal > 0 {
		ramPct = (ramUsed / ramTotal) * 100
	}
	if diskTotal > 0 {
		diskPct = (diskUsed / diskTotal) * 100
	}
	return domain.DashboardStats{
		CPUUsage:  sampleCPU(),
		RAMUsage:  ramPct,
		DiskUsage: diskPct,
		RAMUsed:   ramUsed,
		RAMTotal:  ramTotal,
		DiskUsed:  diskUsed,
		DiskTotal: diskTotal,
		Uptime:    readUptime(),
		OSVersion: osVersion,
		Hostname:  hostname,
	}
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
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return "unknown"
	}
	return (time.Duration(seconds * float64(time.Second))).Round(time.Second).String()
}

var cpuSampleMu sync.Mutex
var previousCPUTotal uint64
var previousCPUIdle uint64

func sampleCPU() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	var cpuLine string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			cpuLine = line
			break
		}
	}
	total, idle, ok := parseCPUStat(cpuLine)
	if !ok {
		return 0
	}
	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()
	if previousCPUTotal == 0 || total <= previousCPUTotal || idle < previousCPUIdle {
		previousCPUTotal, previousCPUIdle = total, idle
		return 0
	}
	totalDelta := total - previousCPUTotal
	idleDelta := idle - previousCPUIdle
	previousCPUTotal, previousCPUIdle = total, idle
	usage := (1 - float64(idleDelta)/float64(totalDelta)) * 100
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func parseCPUStat(line string) (total, idle uint64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for index, field := range fields[1:] {
		if index >= 8 {
			break
		}
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += value
		if index == 3 || index == 4 {
			idle += value
		}
	}
	return total, idle, total > 0
}

type InstallService struct {
	repo     repository.ServiceRepository
	pm       platform.PackageManager
	settings repository.SettingRepository
}

func NewInstallService(repo repository.ServiceRepository, pm platform.PackageManager, settings repository.SettingRepository) *InstallService {
	return &InstallService{repo: repo, pm: pm, settings: settings}
}
func isNginxInstalled() bool {
	if _, err := os.Stat("/usr/sbin/nginx"); err == nil {
		return true
	}
	if _, err := os.Stat("/usr/bin/nginx"); err == nil {
		return true
	}
	if _, err := exec.LookPath("nginx"); err == nil {
		return true
	}
	return false
}

func isMariaDBInstalled() bool {
	if _, err := os.Stat("/usr/sbin/mariadbd"); err == nil {
		return true
	}
	if _, err := os.Stat("/usr/sbin/mysqld"); err == nil {
		return true
	}
	if _, err := exec.LookPath("mysql"); err == nil {
		return true
	}
	if _, err := exec.LookPath("mariadb"); err == nil {
		return true
	}
	return false
}

func isPhpMyAdminInstalled() bool {
	if _, err := os.Stat("/var/www/phpmyadmin/config.inc.php"); err == nil {
		return true
	}
	return false
}

func (s *InstallService) Status(name string) (*domain.ServiceState, error) {
	installed := false
	switch name {
	case "nginx":
		installed = isNginxInstalled()
	case "mariadb":
		installed = isMariaDBInstalled()
	case "phpmyadmin":
		installed = isPhpMyAdminInstalled()
	default:
		return nil, errors.New("unknown service")
	}

	state, err := s.repo.Get(name)
	if err != nil {
		return nil, fmt.Errorf("read %s status: %w", name, err)
	}
	state.Installed = installed
	state.UpdatedAt = time.Now()
	if err := s.repo.Save(state); err != nil {
		return nil, fmt.Errorf("save %s status: %w", name, err)
	}
	return state, nil
}
func (s *InstallService) InstallMariaDB(ctx context.Context, remote bool, rootPassword string) (*domain.ServiceState, error) {
	if !isMariaDBInstalled() {
		return nil, errors.New("MariaDB is not installed on the system. Please run install.sh")
	}
	if remote {
		return nil, errors.New("remote MariaDB access is disabled; configure a restricted network policy before exposing the database")
	}
	if err := s.pm.Enable("mariadb"); err != nil {
		return nil, fmt.Errorf("enable MariaDB: %w", err)
	}
	if err := s.ensureMariaDBAdmin(ctx, rootPassword); err != nil {
		return nil, fmt.Errorf("configure MegoPanel MariaDB administrator: %w", err)
	}

	state := &domain.ServiceState{Name: "mariadb", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}

// EnsureMariaDBAdmin provisions one dedicated panel account for database
// management and phpMyAdmin. It deliberately avoids using MariaDB's root
// account for ordinary panel operations.
func (s *InstallService) EnsureMariaDBAdmin(ctx context.Context) error {
	return s.ensureMariaDBAdmin(ctx, "")
}

func (s *InstallService) ensureMariaDBAdmin(ctx context.Context, suppliedRootPassword string) error {
	if !isMariaDBInstalled() {
		return nil
	}
	password, exists, err := s.settings.Get("mariadb_admin_password")
	if err != nil {
		return fmt.Errorf("read panel database credentials: %w", err)
	}
	if exists && password != "" {
		if _, err := platform.MariaDBQuery(ctx, "megopanel", password, "SELECT 1;"); err == nil {
			if _, legacy, getErr := s.settings.Get("mariadb_root_password"); getErr == nil && legacy {
				if err := s.settings.Delete("mariadb_root_password"); err != nil {
					return fmt.Errorf("remove legacy root credentials from panel settings: %w", err)
				}
			}
			return nil
		}
	}
	legacyRootPassword, legacyExists, err := s.settings.Get("mariadb_root_password")
	if err != nil {
		return fmt.Errorf("read MariaDB root credentials: %w", err)
	}
	rootPassword := suppliedRootPassword
	if rootPassword == "" && legacyExists {
		rootPassword = legacyRootPassword
	}
	password, err = randomToken()
	if err != nil {
		return fmt.Errorf("generate panel database password: %w", err)
	}
	passwordLiteral, err := platform.MariaDBStringLiteral(password)
	if err != nil {
		return err
	}
	query := "CREATE USER IF NOT EXISTS 'megopanel'@'localhost' IDENTIFIED VIA mysql_native_password USING PASSWORD(" + passwordLiteral + "); " +
		"ALTER USER 'megopanel'@'localhost' IDENTIFIED VIA mysql_native_password USING PASSWORD(" + passwordLiteral + "); " +
		"GRANT ALL PRIVILEGES ON *.* TO 'megopanel'@'localhost' WITH GRANT OPTION; FLUSH PRIVILEGES;"
	if _, err := platform.MariaDBQuery(ctx, "root", rootPassword, query); err != nil {
		return errors.New("could not provision the panel database account with the supplied MariaDB root credentials or local Unix socket authentication")
	}
	if err := s.settings.Set("mariadb_admin_password", password); err != nil {
		return fmt.Errorf("save panel database credentials: %w", err)
	}
	if legacyExists {
		if err := s.settings.Delete("mariadb_root_password"); err != nil {
			return fmt.Errorf("remove legacy root credentials from panel settings: %w", err)
		}
	}
	return nil
}
func (s *InstallService) InstallNginx() (*domain.ServiceState, error) {
	if !isNginxInstalled() {
		return nil, errors.New("Nginx is not installed on the system. Please run install.sh")
	}
	if err := s.pm.Enable("nginx"); err != nil {
		return nil, fmt.Errorf("enable Nginx: %w", err)
	}
	state := &domain.ServiceState{Name: "nginx", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}

func (s *InstallService) EnsureAutologinFiles(port string) error {
	// If phpMyAdmin is not installed, there is nothing to manage. Refuse to
	// follow a symlink because this service runs as root.
	basePath := "/var/www/phpmyadmin"
	baseInfo, err := os.Lstat(basePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("phpMyAdmin directory must be a real directory")
	}
	if err := exec.Command("chown", "--no-dereference", "-R", "root:www-data", basePath).Run(); err != nil {
		return fmt.Errorf("set phpMyAdmin files owner: %w", err)
	}
	if err := exec.Command("chmod", "-R", "go-w", basePath).Run(); err != nil {
		return fmt.Errorf("secure phpMyAdmin file permissions: %w", err)
	}
	if err := os.Chmod(basePath, 0755); err != nil {
		return fmt.Errorf("secure phpMyAdmin directory permissions: %w", err)
	}

	autologinPath := basePath + "/autologin.php"
	signonPath := basePath + "/signon.php"
	configPath := basePath + "/config.inc.php"

	autologinPHP := getAutologinPHP()
	signonPHP := getSignonPHP(port)
	writeManaged := func(path, contents string, legacyMarkers ...string) (bool, error) {
		info, statErr := os.Lstat(path)
		if statErr == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return false, fmt.Errorf("refusing to replace non-regular managed file %s", path)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return false, statErr
		}
		current, err := os.ReadFile(path)
		if err == nil && string(current) == contents {
			return false, nil
		}
		if err == nil {
			managed := false
			for _, marker := range legacyMarkers {
				managed = managed || strings.Contains(string(current), marker)
			}
			if !managed {
				return false, nil
			}
		} else if !os.IsNotExist(err) {
			return false, err
		}
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			return false, err
		}
		if err := os.Chmod(path, 0640); err != nil {
			return false, err
		}
		if err := exec.Command("chown", "root:www-data", path).Run(); err != nil {
			return false, err
		}
		return true, nil
	}

	if _, err := writeManaged(autologinPath, autologinPHP, "MegoPanel sign-on v2", "signon.php?token="); err != nil {
		return err
	}
	if _, err := writeManaged(signonPath, signonPHP,
		"MegoPanel sign-on bridge",
		"Token is missing or invalid.",
		"expired after 15 seconds",
	); err != nil {
		return err
	}

	if err := ensurePMASignonConfig(configPath); err != nil {
		return fmt.Errorf("ensure phpMyAdmin sign-on config: %w", err)
	}
	if err := exec.Command("chown", "root:www-data", configPath).Run(); err != nil {
		return fmt.Errorf("set phpMyAdmin config owner: %w", err)
	}

	_ = writeAuto
	_ = writeSignon
	return nil
}

func (s *InstallService) InstallPhpMyAdmin(port string) (*domain.ServiceState, error) {
	if !isPhpMyAdminInstalled() {
		return nil, errors.New("phpMyAdmin is not installed on the system. Please run install.sh")
	}

	phpServiceName := "php-fpm"
	if _, err := exec.LookPath("systemctl"); err == nil {
		svcCmd := exec.Command("sh", "-c", "systemctl list-unit-files | grep -E '^php[0-9.]+-fpm\\.service' | head -n 1 | awk '{print $1}'")
		output, err := svcCmd.Output()
		if err == nil && len(strings.TrimSpace(string(output))) > 0 {
			phpServiceName = strings.TrimSuffix(strings.TrimSpace(string(output)), ".service")
		}
	}
	resetFailedCmd := exec.Command("systemctl", "reset-failed", phpServiceName)
	_ = resetFailedCmd.Run()
	if err := s.pm.Enable(phpServiceName); err != nil {
		return nil, err
	}
	if err := s.pm.Restart(phpServiceName); err != nil {
		return nil, err
	}

	socketPath := findPHPFpmSocket(phpServiceName)
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("php-fpm socket %s is not available: %w", socketPath, err)
	}
	nginxConfig := `server {
    listen 8080;
    listen [::]:8080;
    root /var/www/phpmyadmin;
    index index.php index.html index.htm;
    server_name _;
    location / {
        try_files $uri $uri/ =404;
    }
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:` + socketPath + `;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }
}`
	if err := os.MkdirAll("/etc/nginx/sites-available", 0755); err != nil {
		return nil, fmt.Errorf("failed to create nginx sites-available directory: %w", err)
	}
	if err := os.MkdirAll("/etc/nginx/sites-enabled", 0755); err != nil {
		return nil, fmt.Errorf("failed to create nginx sites-enabled directory: %w", err)
	}
	nginxSitePath := "/etc/nginx/sites-available/phpmyadmin"
	if info, err := os.Lstat(nginxSitePath); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("refusing to replace a non-regular Nginx phpMyAdmin config")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect Nginx phpMyAdmin config: %w", err)
	}
	if err := os.WriteFile(nginxSitePath, []byte(nginxConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin nginx configuration: %w", err)
	}
	if err := os.Remove("/etc/nginx/sites-enabled/phpmyadmin"); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to replace phpmyadmin nginx symlink: %w", err)
	}
	if err := os.Symlink(nginxSitePath, "/etc/nginx/sites-enabled/phpmyadmin"); err != nil {
		return nil, fmt.Errorf("failed to enable phpmyadmin nginx site: %w", err)
	}
	nginxTestCmd := exec.Command("nginx", "-t")
	if output, err := nginxTestCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("nginx configuration test failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := s.pm.Enable("nginx"); err != nil {
		return nil, err
	}
	if err := s.pm.Restart("nginx"); err != nil {
		return nil, err
	}

	if err := s.EnsureAutologinFiles(port); err != nil {
		return nil, fmt.Errorf("failed to configure phpmyadmin autologin scripts: %w", err)
	}

	healthCheckCmd := exec.Command("curl", "--fail", "--silent", "--show-error", "--max-time", "10", "--output", "/dev/null", "http://127.0.0.1:8080/")
	if output, err := healthCheckCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("phpmyadmin did not respond after installation: %w: %s", err, strings.TrimSpace(string(output)))
	}

	state := &domain.ServiceState{Name: "phpmyadmin", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}

//go:embed templates/signon.php
var phpMyAdminSignonTemplate embed.FS

// getAutologinPHP deliberately refuses bearer tokens in query strings.
func getAutologinPHP() string {
	return `<?php
// MegoPanel sign-on v2 never accepts bearer tokens in query strings.
header('Cache-Control: no-store, private');
header('Referrer-Policy: no-referrer');
header('Location: ./');
exit;
`
}

func getSignonPHP(port string) string {
	if !regexp.MustCompile(`^[0-9]{1,5}$`).MatchString(port) {
		port = "8888"
	}
	contents, err := phpMyAdminSignonTemplate.ReadFile("templates/signon.php")
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(contents), "__MEGOPANEL_API_PORT__", port)
}

func ensurePMASignonConfig(path string) error {
	info, statErr := os.Lstat(path)
	if statErr == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("phpMyAdmin config must be a regular file")
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	config := string(current)
	managed := `
if (!isset($i)) { $i = 1; }
$cfg['Servers'][$i]['auth_type'] = 'signon';
$cfg['Servers'][$i]['SignonSession'] = 'SignonSession';
$cfg['Servers'][$i]['SignonURL'] = 'signon.php';
$cfg['Servers'][$i]['LogoutURL'] = 'signon.php?action=logout';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['AllowNoPassword'] = false;
`
	if os.IsNotExist(err) {
		secret, secretErr := randomToken()
		if secretErr != nil {
			return secretErr
		}
		config = "<?php\n$cfg['blowfish_secret'] = '" + secret[:32] + "';\n$i = 1;\n" + managed
	} else {
		if !strings.Contains(config, "<?php") {
			return errors.New("phpMyAdmin config does not contain a PHP opening tag")
		}
		var additions strings.Builder
		if !regexp.MustCompile(`\$cfg\[['\"]blowfish_secret['\"]\]`).MatchString(config) {
			secret, secretErr := randomToken()
			if secretErr != nil {
				return secretErr
			}
			fmt.Fprintf(&additions, "\n$cfg['blowfish_secret'] = '%s';\n", secret[:32])
		}
		for _, required := range []string{
			"$cfg['Servers'][$i]['auth_type'] = 'signon';",
			"$cfg['Servers'][$i]['SignonSession'] = 'SignonSession';",
			"$cfg['Servers'][$i]['SignonURL'] = 'signon.php';",
			"$cfg['Servers'][$i]['LogoutURL'] = 'signon.php?action=logout';",
			"$cfg['Servers'][$i]['host'] = 'localhost';",
			"$cfg['Servers'][$i]['AllowNoPassword'] = false;",
		} {
			if !strings.Contains(config, required) {
				fmt.Fprintf(&additions, "\n%s\n", required)
			}
		}
		if additions.Len() > 0 {
			backupPath := path + ".megopanel.bak"
			if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
				if writeErr := os.WriteFile(backupPath, current, 0600); writeErr != nil {
					return fmt.Errorf("backup existing config: %w", writeErr)
				}
			}
			offset := strings.LastIndex(config, "?>")
			if offset >= 0 {
				config = config[:offset] + additions.String() + config[offset:]
			} else {
				config += additions.String()
			}
		}
	}
	if err := os.WriteFile(path, []byte(config), 0640); err != nil {
		return err
	}
	if err := os.Chmod(path, 0640); err != nil {
		return err
	}
	return nil
}
