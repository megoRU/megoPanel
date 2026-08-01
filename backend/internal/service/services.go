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
	if err := s.pm.InstallMany(packages); err != nil {
		return nil, err
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

$cfg['Servers'][$i]['auth_type'] = 'signon';
$cfg['Servers'][$i]['SignonSession'] = 'SignonSession';
$cfg['Servers'][$i]['SignonURL'] = 'signon.php';
$cfg['Servers'][$i]['LogoutURL'] = 'signon.php?action=logout';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
`
	if err := os.WriteFile("/var/www/phpmyadmin/config.inc.php", []byte(pmaConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin configuration: %w", err)
	}

	autologinPHP := `<?php
if (isset($_GET['token'])) {
    $token = preg_replace('/[^a-zA-Z0-9-]/', '', $_GET['token']);
    header('Location: signon.php?token=' . $token);
    exit;
}
header('Location: index.php');
exit;
`
	if err := os.WriteFile("/var/www/phpmyadmin/autologin.php", []byte(autologinPHP), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin autologin script: %w", err)
	}

	signonPHP := `<?php
session_name('SignonSession');
session_start();

if (isset($_GET['action']) && $_GET['action'] === 'logout') {
    unset($_SESSION['PMA_single_signon_user']);
    unset($_SESSION['PMA_single_signon_password']);
    unset($_SESSION['PMA_single_signon_host']);
    session_destroy();
    header('Location: http://' . $_SERVER['HTTP_HOST'] . ':8888/');
    exit;
}

$token = isset($_GET['token']) ? preg_replace('/[^a-zA-Z0-9-]/', '', $_GET['token']) : '';

if (empty($token)) {
    show_error_page("Token is missing or invalid.", "Please try logging in to phpMyAdmin again from the MegoPanel dashboard.");
}

$apiUrl = "http://127.0.0.1:8888/internal/phpmyadmin/token?token=" . urlencode($token);

$ch = curl_init();
curl_setopt($ch, CURLOPT_URL, $apiUrl);
curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
curl_setopt($ch, CURLOPT_TIMEOUT, 5);
$response = curl_exec($ch);
$httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
curl_close($ch);

if ($httpCode !== 200 || !$response) {
    $ctx = stream_context_create([
        'http' => [
            'timeout' => 5,
            'ignore_errors' => true
        ]
    ]);
    $response = @file_get_contents($apiUrl, false, $ctx);
    $httpCode = 0;
    if (isset($http_response_header[0])) {
        preg_match('{HTTP\/\S+\s+(\d+)}', $http_response_header[0], $matches);
        if (isset($matches[1])) {
            $httpCode = intval($matches[1]);
        }
    }
}

if ($httpCode !== 200 || !$response) {
    show_error_page("Authentication failed.", "The autologin token is invalid, expired (expired after 15 seconds), or already used. Please go back to the MegoPanel dashboard and try again.");
}

$data = json_decode($response, true);
if (!$data || !isset($data['username']) || !isset($data['password'])) {
    show_error_page("Malformed API response.", "The authentication service returned an invalid response format.");
}

$_SESSION['PMA_single_signon_user'] = $data['username'];
$_SESSION['PMA_single_signon_password'] = $data['password'];
$_SESSION['PMA_single_signon_host'] = 'localhost';

header('Location: index.php');
exit;

function show_error_page($title, $message) {
    header("HTTP/1.1 403 Forbidden");
    ?>
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>phpMyAdmin Autologin Error</title>
        <style>
            body {
                background: #0f0f11;
                color: #e4e4e7;
                font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
                display: flex;
                align-items: center;
                justify-content: center;
                min-height: 100vh;
                margin: 0;
            }
            .card {
                background: #18181b;
                border: 1px solid #27272a;
                border-radius: 8px;
                padding: 32px;
                max-width: 480px;
                width: 100%;
                box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4);
                text-align: center;
            }
            .icon {
                color: #ef4444;
                font-size: 48px;
                margin-bottom: 16px;
            }
            h1 {
                font-size: 20px;
                font-weight: 600;
                margin: 0 0 12px 0;
                color: #ffffff;
            }
            p {
                font-size: 14px;
                color: #a1a1aa;
                line-height: 1.5;
                margin: 0 0 24px 0;
            }
            .btn {
                display: inline-block;
                background: #ffffff;
                color: #0f0f11;
                font-weight: 600;
                font-size: 13px;
                text-decoration: none;
                padding: 10px 20px;
                border-radius: 6px;
                transition: background 0.15s ease;
            }
            .btn:hover {
                background: #e4e4e7;
            }
        </style>
    </head>
    <body>
        <div class="card">
            <div class="icon">⚠️</div>
            <h1><?php echo htmlspecialchars($title); ?></h1>
            <p><?php echo htmlspecialchars($message); ?></p>
            <a href="http://<?php echo htmlspecialchars($_SERVER['HTTP_HOST']); ?>:8888/" class="btn">Back to Dashboard</a>
        </div>
    </body>
    </html>
    <?php
    exit;
}
`
	if err := os.WriteFile("/var/www/phpmyadmin/signon.php", []byte(signonPHP), 0644); err != nil {
		return nil, fmt.Errorf("failed to write phpmyadmin signon script: %w", err)
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
