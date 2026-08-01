package http

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/service"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type RouterDeps struct {
	Config    *config.Config
	Auth      *service.AuthService
	Dashboard *service.DashboardService
	Install   *service.InstallService
	DB        *gorm.DB
}

func NewRouter(d RouterDeps) *gin.Engine {
	if d.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders(), cors.New(cors.Config{AllowOrigins: []string{d.Config.Server.FrontendURL}, AllowCredentials: true, AllowHeaders: []string{"Content-Type", "X-CSRF-Token"}, AllowMethods: []string{"GET", "POST", "DELETE", "OPTIONS"}}))
	api := r.Group("/api/v1")
	api.GET("/setup/status", func(c *gin.Context) { ok, err := d.Auth.IsConfigured(); respond(c, ok, gin.H{"configured": ok}, err) })
	api.POST("/setup/admin", func(c *gin.Context) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if bind(c, &req) {
			return
		}
		if err := d.Auth.CreateAdmin(req.Username, req.Password); err != nil {
			respond(c, false, nil, err)
			return
		}
		access, refresh, err := d.Auth.Login(req.Username, req.Password)
		if err == nil {
			setCookie(c, "access_token", access, d.Config.AccessTTL().Seconds(), d.Config.Security.CookieSecure)
			setCookie(c, "refresh_token", refresh, d.Config.RefreshTTL().Seconds(), d.Config.Security.CookieSecure)
			setCookie(c, "csrf_token", "megopanel-csrf", d.Config.RefreshTTL().Seconds(), false)
		}
		respond(c, false, gin.H{"ok": true}, err)
	})
	api.POST("/auth/login", func(c *gin.Context) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if bind(c, &req) {
			return
		}
		access, refresh, err := d.Auth.Login(req.Username, req.Password)
		if err == nil {
			setCookie(c, "access_token", access, d.Config.AccessTTL().Seconds(), d.Config.Security.CookieSecure)
			setCookie(c, "refresh_token", refresh, d.Config.RefreshTTL().Seconds(), d.Config.Security.CookieSecure)
			setCookie(c, "csrf_token", "megopanel-csrf", d.Config.RefreshTTL().Seconds(), false)
		}
		respond(c, false, gin.H{"ok": true}, err)
	})
	api.POST("/auth/refresh", func(c *gin.Context) {
		refresh, err := c.Cookie("refresh_token")
		if err != nil || refresh == "" {
			c.JSON(401, gin.H{"error": "missing refresh token"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	api.POST("/auth/logout", func(c *gin.Context) {
		setCookie(c, "access_token", "", -1, d.Config.Security.CookieSecure)
		setCookie(c, "refresh_token", "", -1, d.Config.Security.CookieSecure)
		c.JSON(200, gin.H{"ok": true})
	})
	protected := api.Group("")
	protected.Use(authMiddleware(d.Auth), csrfMiddleware())
	protected.GET("/auth/me", func(c *gin.Context) { c.JSON(200, gin.H{"authenticated": true}) })
	protected.GET("/dashboard", func(c *gin.Context) { c.JSON(200, d.Dashboard.Stats()) })
	protected.GET("/websites", func(c *gin.Context) {
		var sites []domain.Website
		if err := d.DB.Find(&sites).Error; err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, sites)
	})
	protected.POST("/websites", func(c *gin.Context) {
		var req struct {
			Domain    string `json:"domain"`
			IPAddress string `json:"ipAddress"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if net.ParseIP(req.IPAddress) == nil {
			c.JSON(400, gin.H{"error": "valid IP address is required"})
			return
		}

		// Check for duplicate domain
		var count int64
		if err := d.DB.Model(&domain.Website{}).Where("domain = ?", req.Domain).Count(&count).Error; err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if count > 0 {
			c.JSON(400, gin.H{"error": "Website with this domain is already added"})
			return
		}

		path := "/var/www/" + req.Domain
		site := domain.Website{Domain: req.Domain, IPAddress: req.IPAddress, Path: path}
		if err := d.DB.Create(&site).Error; err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, site)
	})
	protected.DELETE("/websites/:id", func(c *gin.Context) {
		id := c.Param("id")
		if err := d.DB.Delete(&domain.Website{}, id).Error; err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	protected.GET("/databases", func(c *gin.Context) {
		var passwordSetting domain.Setting
		var rootPassword string
		if err := d.DB.First(&passwordSetting, "key = ?", "mariadb_root_password").Error; err == nil {
			rootPassword = passwordSetting.Value
		}

		var args []string
		if rootPassword != "" {
			args = append(args, "-u", "root", "-p"+rootPassword, "-N", "-e", "SHOW DATABASES;")
		} else {
			args = append(args, "-u", "root", "-N", "-e", "SHOW DATABASES;")
		}

		cmd := exec.Command("mysql", args...)
		output, err := cmd.Output()
		if err != nil {
			cmd = exec.Command("mariadb", args...)
			output, err = cmd.Output()
		}
		if err != nil {
			c.JSON(400, gin.H{"error": "failed to list databases: " + err.Error()})
			return
		}

		lines := strings.Split(string(output), "\n")
		var dbs []string
		for _, line := range lines {
			db := strings.TrimSpace(line)
			if db == "" {
				continue
			}
			if db == "information_schema" || db == "mysql" || db == "performance_schema" || db == "sys" || db == "test" || db == "tmp" {
				continue
			}
			dbs = append(dbs, db)
		}
		c.JSON(200, dbs)
	})

	protected.POST("/databases", func(c *gin.Context) {
		var req struct {
			Name     string `json:"name"`
			Charset  string `json:"charset"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		if req.Name == "" {
			c.JSON(400, gin.H{"error": "Database name cannot be empty"})
			return
		}
		matched, _ := regexp.MatchString("^[a-zA-Z0-9_]+$", req.Name)
		if !matched {
			c.JSON(400, gin.H{"error": "Database name can only contain letters, numbers, and underscores"})
			return
		}

		charset := "utf8"
		if req.Charset == "utf8mb4" || req.Charset == "cp1251" || req.Charset == "latin1" {
			charset = req.Charset
		}

		createCmd := "CREATE DATABASE `" + req.Name + "` CHARACTER SET " + charset + ";"
		if req.Password != "" {
			escapedPassword := strings.ReplaceAll(req.Password, "'", "\\'")
			createCmd += " CREATE USER IF NOT EXISTS '" + req.Name + "'@'localhost' IDENTIFIED BY '" + escapedPassword + "';"
			createCmd += " GRANT ALL PRIVILEGES ON `" + req.Name + "`.* TO '" + req.Name + "'@'localhost';"
			createCmd += " FLUSH PRIVILEGES;"
		}

		var passwordSetting domain.Setting
		var rootPassword string
		if err := d.DB.First(&passwordSetting, "key = ?", "mariadb_root_password").Error; err == nil {
			rootPassword = passwordSetting.Value
		}

		var args []string
		if rootPassword != "" {
			args = append(args, "-u", "root", "-p"+rootPassword, "-e", createCmd)
		} else {
			args = append(args, "-u", "root", "-e", createCmd)
		}

		cmd := exec.Command("mysql", args...)
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("mariadb", args...)
			if err := cmd.Run(); err != nil {
				c.JSON(400, gin.H{"error": "failed to create database: " + err.Error()})
				return
			}
		}
		if req.Password != "" {
			_ = d.DB.Save(&domain.Setting{Key: "db_password_" + req.Name, Value: req.Password})
		}
		c.JSON(200, gin.H{"ok": true, "name": req.Name})
	})

	protected.DELETE("/databases/:name", func(c *gin.Context) {
		name := c.Param("name")

		matched, _ := regexp.MatchString("^[a-zA-Z0-9_]+$", name)
		if !matched {
			c.JSON(400, gin.H{"error": "invalid database name"})
			return
		}

		dropCmd := "DROP DATABASE `" + name + "`; DROP USER IF EXISTS '" + name + "'@'localhost';"

		var passwordSetting domain.Setting
		var rootPassword string
		if err := d.DB.First(&passwordSetting, "key = ?", "mariadb_root_password").Error; err == nil {
			rootPassword = passwordSetting.Value
		}

		var args []string
		if rootPassword != "" {
			args = append(args, "-u", "root", "-p"+rootPassword, "-e", dropCmd)
		} else {
			args = append(args, "-u", "root", "-e", dropCmd)
		}

		cmd := exec.Command("mysql", args...)
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("mariadb", args...)
			if err := cmd.Run(); err != nil {
				c.JSON(400, gin.H{"error": "failed to delete database: " + err.Error()})
				return
			}
		}
		_ = d.DB.Delete(&domain.Setting{}, "key = ?", "db_password_"+name)
		c.JSON(200, gin.H{"ok": true})
	})

	protected.POST("/databases/autologin", func(c *gin.Context) {
		var req struct {
			DB string `json:"db"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		// Verify phpmyadmin directory exists
		if _, err := os.Stat("/var/www/phpmyadmin"); os.IsNotExist(err) {
			c.JSON(400, gin.H{"error": "phpMyAdmin is not installed"})
			return
		}

		// Get root password
		var passwordSetting domain.Setting
		var rootPassword string
		if err := d.DB.First(&passwordSetting, "key = ?", "mariadb_root_password").Error; err == nil {
			rootPassword = passwordSetting.Value
		}

		dbUser := "root"
		dbPassword := rootPassword
		if req.DB != "" {
			var dbPasswordSetting domain.Setting
			if err := d.DB.First(&dbPasswordSetting, "key = ?", "db_password_"+req.DB).Error; err == nil {
				dbUser = req.DB
				dbPassword = dbPasswordSetting.Value
			}
		}

		// Cleanup old tokens
		if files, err := os.ReadDir("/var/www/phpmyadmin"); err == nil {
			for _, f := range files {
				if strings.HasPrefix(f.Name(), "token_") {
					info, err := f.Info()
					if err == nil && time.Since(info.ModTime()) > 60*time.Second {
						_ = os.Remove("/var/www/phpmyadmin/" + f.Name())
					}
				}
			}
		}

		// Generate random token
		tokenBytes := make([]byte, 16)
		if _, err := rand.Read(tokenBytes); err != nil {
			c.JSON(500, gin.H{"error": "failed to generate token"})
			return
		}
		token := hex.EncodeToString(tokenBytes)

		// Create token file
		tokenPath := "/var/www/phpmyadmin/token_" + token
		tokenContent := dbUser + ":" + dbPassword
		_ = os.WriteFile(tokenPath, []byte(tokenContent), 0644)
		_ = os.Chown(tokenPath, 33, 33)

		// Rewrite config.inc.php and autologin.php to ensure they are up to date!
		blowfishBytes := make([]byte, 16)
		_, _ = rand.Read(blowfishBytes)
		blowfishSecret := hex.EncodeToString(blowfishBytes)

		pmaConfig := `<?php
$cfg['blowfish_secret'] = '` + blowfishSecret + `';
$i = 0;
$i++;

$autologin = false;
$user = 'root';
$password = '';

if (isset($_COOKIE['pma_autologin_token'])) {
    $token = preg_replace('/[^a-f0-9]/', '', $_COOKIE['pma_autologin_token']);
    $token_file = '/var/www/phpmyadmin/token_' . $token;
    if ($token && file_exists($token_file)) {
        $mtime = filemtime($token_file);
        if (time() - $mtime < 30) {
            $content = file_get_contents($token_file);
            $parts = explode(':', $content, 2);
            if (count($parts) === 2) {
                $user = $parts[0];
                $password = $parts[1];
                $autologin = true;
            }
        }
        @unlink($token_file);
    }
    setcookie('pma_autologin_token', '', time() - 3600, '/');
}

if ($autologin) {
    $cfg['Servers'][$i]['auth_type'] = 'config';
    $cfg['Servers'][$i]['user'] = $user;
    $cfg['Servers'][$i]['password'] = $password;
} else {
    $cfg['Servers'][$i]['auth_type'] = 'cookie';
}
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
`
		_ = os.WriteFile("/var/www/phpmyadmin/config.inc.php", []byte(pmaConfig), 0644)
		_ = os.Chown("/var/www/phpmyadmin/config.inc.php", 33, 33)

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
		_ = os.WriteFile("/var/www/phpmyadmin/autologin.php", []byte(autologinPHP), 0644)
		_ = os.Chown("/var/www/phpmyadmin/autologin.php", 33, 33)

		c.JSON(200, gin.H{"token": token, "db": req.DB})
	})

	protected.GET("/install/:name/status", func(c *gin.Context) { state, err := d.Install.Status(c.Param("name")); respond(c, false, state, err) })
	protected.POST("/install/mariadb", func(c *gin.Context) {
		var req struct {
			RemoteAccess bool   `json:"remoteAccess"`
			RootPassword string `json:"rootPassword"`
		}
		_ = c.ShouldBindJSON(&req)
		state, err := d.Install.InstallMariaDB(req.RemoteAccess, req.RootPassword)
		respond(c, false, state, err)
	})
	protected.POST("/install/nginx", func(c *gin.Context) { state, err := d.Install.InstallNginx(); respond(c, false, state, err) })
	protected.POST("/install/phpmyadmin", func(c *gin.Context) {
		state, err := d.Install.InstallPhpMyAdmin()
		respond(c, false, state, err)
	})
	protected.GET("/ws", func(c *gin.Context) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(gin.H{"type": "hello"})
	})
	r.Static("/assets", "templates/frontend/assets")
	r.StaticFile("/favicon.ico", "templates/frontend/favicon.ico")
	r.NoRoute(func(c *gin.Context) {
		c.File("templates/frontend/index.html")
	})
	return r
}
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
func authMiddleware(a *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("access_token")
		if err != nil || a.Verify(token) != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
func csrfMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" {
			c.Next()
			return
		}
		if c.GetHeader("X-CSRF-Token") != "megopanel-csrf" {
			c.AbortWithStatusJSON(403, gin.H{"error": "csrf validation failed"})
			return
		}
		c.Next()
	}
}
func setCookie(c *gin.Context, n, v string, age float64, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(n, v, int(age), "/", "", secure, true)
}
func bind(c *gin.Context, target interface{}) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return true
	}
	return false
}
func respond(c *gin.Context, _ bool, data interface{}, err error) {
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}
