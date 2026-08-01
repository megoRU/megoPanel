package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

func getRAMUsage() (used float64, total float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 1.5, 4.0 // fallback
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
		return 1.5, 4.0
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
		return 4.2, 10.0 // fallback
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

var StartTime = time.Now()

func readUptime() string {
	return time.Since(StartTime).Round(time.Second).String()
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
	repo     repository.ServiceRepository
	pm       platform.PackageManager
	settings repository.SettingRepository
}

func NewInstallService(repo repository.ServiceRepository, pm platform.PackageManager, settings repository.SettingRepository) *InstallService {
	return &InstallService{repo: repo, pm: pm, settings: settings}
}
func (s *InstallService) Status(name string) (*domain.ServiceState, error) { return s.repo.Get(name) }
func (s *InstallService) InstallMariaDB(remote bool, rootPassword string) (*domain.ServiceState, error) {
	if err := s.pm.Install("mariadb-server"); err != nil {
		return nil, err
	}
	if remote {
		_ = os.WriteFile("/etc/mysql/mariadb.conf.d/60-megopanel.cnf", []byte("[mysqld]\nbind-address=0.0.0.0\n"), 0644)
	}
	_ = s.pm.Enable("mariadb")
	_ = s.pm.Restart("mariadb")

	if rootPassword != "" {
		_ = s.settings.Set("mariadb_root_password", rootPassword)
		time.Sleep(1 * time.Second)
		sqlCmd := fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;", strings.ReplaceAll(rootPassword, "'", "\\'"))
		cmd := exec.Command("mysql", "-u", "root", "-e", sqlCmd)
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("mariadb", "-u", "root", "-e", sqlCmd)
			_ = cmd.Run()
		}
	}

	state := &domain.ServiceState{Name: "mariadb", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}
func (s *InstallService) InstallNginx() (*domain.ServiceState, error) {
	if err := s.pm.Install("nginx"); err != nil {
		return nil, err
	}
	_ = os.Remove("/etc/nginx/sites-enabled/default")
	_ = s.pm.Enable("nginx")
	_ = s.pm.Restart("nginx")
	state := &domain.ServiceState{Name: "nginx", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}

func (s *InstallService) InstallPhpMyAdmin() (*domain.ServiceState, error) {
	packages := []string{"nginx", "php-fpm", "php-mysql", "php-mbstring", "php-xml", "php-curl", "php-zip", "php-gd", "tar", "curl"}
	for _, packageName := range packages {
		if err := s.pm.Install(packageName); err != nil {
			return nil, err
		}
	}

	if err := os.RemoveAll("/var/www/phpmyadmin"); err != nil {
		return nil, fmt.Errorf("failed to clean phpmyadmin directory: %w", err)
	}
	if err := os.MkdirAll("/var/www/phpmyadmin", 0755); err != nil {
		return nil, fmt.Errorf("failed to create phpmyadmin directory: %w", err)
	}
	archivePath := "/tmp/phpmyadmin-5.2.1-all-languages.tar.gz"
	downloadCmd := exec.Command("curl", "--fail", "--show-error", "--location", "--connect-timeout", "15", "--retry", "3", "--retry-delay", "2", "--output", archivePath, "https://files.phpmyadmin.net/phpMyAdmin/5.2.1/phpMyAdmin-5.2.1-all-languages.tar.gz")
	if output, err := downloadCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to download phpmyadmin archive: %w: %s", err, strings.TrimSpace(string(output)))
	}
	defer os.Remove(archivePath)
	extractCmd := exec.Command("tar", "-xzf", archivePath, "--strip-components=1", "-C", "/var/www/phpmyadmin")
	if output, err := extractCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to extract phpmyadmin archive: %w: %s", err, strings.TrimSpace(string(output)))
	}

	blowfishSecret, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate phpmyadmin secret: %w", err)
	}
	if len(blowfishSecret) > 32 {
		blowfishSecret = blowfishSecret[:32]
	}

	pmaConfig := `<?php
$cfg['blowfish_secret'] = '` + blowfishSecret + `';
$i = 0;
$i++;

if (session_status() === PHP_SESSION_NONE) {
    session_start();
}

$autologin = false;
if (isset($_COOKIE['pma_autologin_token'])) {
    $token = preg_replace('/[^a-f0-9]/', '', $_COOKIE['pma_autologin_token']);
    $token_file = '/var/www/phpmyadmin/token_' . $token;
    if ($token && file_exists($token_file)) {
        $mtime = filemtime($token_file);
        if (time() - $mtime < 30) {
            $content = file_get_contents($token_file);
            $parts = explode(':', $content, 2);
            if (count($parts) === 2) {
                $_SESSION['pma_autologin_user'] = $parts[0];
                $_SESSION['pma_autologin_pass'] = $parts[1];
            }
        }
        @unlink($token_file);
    }
    setcookie('pma_autologin_token', '', time() - 3600, '/');
}

if (isset($_SESSION['pma_autologin_user']) && isset($_SESSION['pma_autologin_pass'])) {
    $cfg['Servers'][$i]['auth_type'] = 'config';
    $cfg['Servers'][$i]['user'] = $_SESSION['pma_autologin_user'];
    $cfg['Servers'][$i]['password'] = $_SESSION['pma_autologin_pass'];
} else {
    $cfg['Servers'][$i]['auth_type'] = 'cookie';
}
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
`
	if err := os.WriteFile("/var/www/phpmyadmin/config.inc.php", []byte(pmaConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin configuration: %w", err)
	}

	autologinPHP := `<?php
if (isset($_GET['token'])) {
    $token = preg_replace('/[^a-f0-9]/', '', $_GET['token']);
    if ($token && file_exists('/var/www/phpmyadmin/token_' . $token)) {
        setcookie('pma_autologin_token', $token, 0, '/');
    }
}
$db = isset($_GET['db']) ? urlencode($_GET['db']) : '';
header('Location: index.php' . ($db ? '?db=' . $db : ''));
exit;
`
	if err := os.WriteFile("/var/www/phpmyadmin/autologin.php", []byte(autologinPHP), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin autologin script: %w", err)
	}
	chownCmd := exec.Command("chown", "-R", "www-data:www-data", "/var/www/phpmyadmin")
	if output, err := chownCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to set phpmyadmin ownership: %w: %s", err, strings.TrimSpace(string(output)))
	}

	phpServiceName := "php-fpm"
	if _, err := exec.LookPath("systemctl"); err == nil {
		svcCmd := exec.Command("sh", "-c", "systemctl list-unit-files | grep -E '^php[0-9.]+-fpm\\.service' | head -n 1 | awk '{print $1}'")
		output, err := svcCmd.Output()
		if err == nil && len(strings.TrimSpace(string(output))) > 0 {
			phpServiceName = strings.TrimSuffix(strings.TrimSpace(string(output)), ".service")
		}
	}
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
    listen 8080 default_server;
    listen [::]:8080 default_server;
    root /var/www/phpmyadmin;
    index index.php index.html index.htm;
    server_name _;
    location / {
        try_files $uri $uri/ =404;
    }
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:` + socketPath + `;
    }
}`
	if err := os.MkdirAll("/etc/nginx/sites-available", 0755); err != nil {
		return nil, fmt.Errorf("failed to create nginx sites-available directory: %w", err)
	}
	if err := os.MkdirAll("/etc/nginx/sites-enabled", 0755); err != nil {
		return nil, fmt.Errorf("failed to create nginx sites-enabled directory: %w", err)
	}
	if err := os.WriteFile("/etc/nginx/sites-available/phpmyadmin", []byte(nginxConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin nginx configuration: %w", err)
	}
	if err := os.Remove("/etc/nginx/sites-enabled/phpmyadmin"); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to replace phpmyadmin nginx symlink: %w", err)
	}
	if err := os.Symlink("/etc/nginx/sites-available/phpmyadmin", "/etc/nginx/sites-enabled/phpmyadmin"); err != nil {
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
	healthCheckCmd := exec.Command("curl", "--fail", "--silent", "--show-error", "--location", "--max-time", "10", "--output", "/dev/null", "http://127.0.0.1:8080/")
	if output, err := healthCheckCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("phpmyadmin did not respond after installation: %w: %s", err, strings.TrimSpace(string(output)))
	}

	state := &domain.ServiceState{Name: "phpmyadmin", Installed: true, UpdatedAt: time.Now()}
	return state, s.repo.Save(state)
}
