package http

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/domain"
	"mego-panel/backend/internal/platform"
	"mego-panel/backend/internal/service"
)

type RouterDeps struct {
	Config    *config.Config
	Auth      *service.AuthService
	Dashboard *service.DashboardService
	Install   *service.InstallService
	Update    *service.UpdateService
	Backups   *service.BackupService
	Websites  *service.WebsiteService
	DB        *gorm.DB
	Autologin *service.AutologinStore
}

var databaseNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

func NewRouter(d RouterDeps) *gin.Engine {
	if d.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders(), cors.New(cors.Config{AllowOrigins: []string{d.Config.Server.FrontendURL}, AllowCredentials: true, AllowHeaders: []string{"Content-Type", "X-CSRF-Token"}, AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}}))
	loginAttempts := newLoginLimiter(10, 15*time.Minute)

	r.POST("/internal/phpmyadmin/token", func(c *gin.Context) {
		remoteIP := requestRemoteIP(c.Request.RemoteAddr)
		parsedIP := net.ParseIP(remoteIP)
		if parsedIP == nil || !parsedIP.IsLoopback() {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if bind(c, &req) {
			return
		}
		if !regexp.MustCompile(`^[0-9a-fA-F-]{36}$`).MatchString(req.Token) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sign-on token"})
			return
		}
		if d.Autologin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sign-on is unavailable"})
			return
		}
		pmaToken, err := d.Autologin.RetrieveAndConsume(req.Token)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "sign-on token is invalid or expired"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		c.JSON(http.StatusOK, gin.H{
			"username": pmaToken.MySQLUsername,
			"password": pmaToken.MySQLPassword,
		})
	})

	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	api.Use(issueCSRFCookie(d.Config))
	api.GET("/setup/status", func(c *gin.Context) { ok, err := d.Auth.IsConfigured(); respond(c, ok, gin.H{"configured": ok}, err) })
	api.POST("/setup/admin", csrfMiddleware(d.Config), func(c *gin.Context) {
		var req struct {
			Username   string `json:"username"`
			Password   string `json:"password"`
			SetupToken string `json:"setupToken"`
		}
		if bind(c, &req) {
			return
		}
		if err := d.Auth.CreateAdmin(req.Username, req.Password, req.SetupToken); err != nil {
			respond(c, false, nil, err)
			return
		}
		access, refresh, err := d.Auth.Login(req.Username, req.Password)
		if err == nil {
			err = setAuthCookies(c, d.Config, access, refresh)
		}
		respond(c, false, gin.H{"ok": true}, err)
	})
	api.POST("/auth/login", csrfMiddleware(d.Config), func(c *gin.Context) {
		clientIP := requestRemoteIP(c.Request.RemoteAddr)
		if allowed, retry := loginAttempts.allow(clientIP, time.Now()); !allowed {
			seconds := int(retry / time.Second)
			if retry%time.Second != 0 {
				seconds++
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts; try again later"})
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if bind(c, &req) {
			return
		}
		access, refresh, err := d.Auth.Login(req.Username, req.Password)
		if err == nil {
			err = setAuthCookies(c, d.Config, access, refresh)
			loginAttempts.succeeded(clientIP)
		} else {
			loginAttempts.failed(clientIP, time.Now())
		}
		respond(c, false, gin.H{"ok": true}, err)
	})
	api.POST("/auth/refresh", csrfMiddleware(d.Config), func(c *gin.Context) {
		refresh, err := c.Cookie("refresh_token")
		if err != nil || refresh == "" {
			c.JSON(401, gin.H{"error": "missing refresh token"})
			return
		}
		access, rotatedRefresh, err := d.Auth.Refresh(refresh)
		if err != nil {
			setCookie(c, "access_token", "", -1, d.Config.Security.CookieSecure)
			setCookie(c, "refresh_token", "", -1, d.Config.Security.CookieSecure)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token is invalid or expired"})
			return
		}
		if err := setAuthCookies(c, d.Config, access, rotatedRefresh); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not refresh session"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	api.POST("/auth/logout", csrfMiddleware(d.Config), func(c *gin.Context) {
		refresh, _ := c.Cookie("refresh_token")
		d.Auth.RevokeRefresh(refresh)
		setCookie(c, "access_token", "", -1, d.Config.Security.CookieSecure)
		setCookie(c, "refresh_token", "", -1, d.Config.Security.CookieSecure)
		c.SetCookie("csrf_token", "", -1, "/", "", d.Config.Security.CookieSecure, false)
		c.JSON(200, gin.H{"ok": true})
	})
	protected := api.Group("")
	protected.Use(authMiddleware(d.Auth), csrfMiddleware(d.Config))
	protected.GET("/auth/me", func(c *gin.Context) { c.JSON(200, gin.H{"authenticated": true}) })
	protected.GET("/dashboard", func(c *gin.Context) { c.JSON(200, d.Dashboard.Stats()) })
	protected.GET("/websites", func(c *gin.Context) {
		sites, err := d.Websites.List()
		respond(c, false, sites, err)
	})
	protected.POST("/websites", func(c *gin.Context) {
		var req struct {
			Domain    string `json:"domain"`
			IPAddress string `json:"ipAddress"`
		}
		if bind(c, &req) {
			return
		}
		site, err := d.Websites.Create(c.Request.Context(), req.Domain, req.IPAddress)
		respond(c, false, site, err)
	})
	protected.DELETE("/websites/:id", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil || id == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid website id"})
			return
		}
		if err := d.Websites.Delete(c.Request.Context(), id); err != nil {
			if err.Error() == "website not found" {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			respond(c, false, nil, err)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	protected.GET("/databases", func(c *gin.Context) {
		password, err := panelMariaDBPassword(d.DB)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "panel database account is not configured"})
			return
		}
		output, err := platform.MariaDBQuery(c.Request.Context(), "megopanel", password, "SHOW DATABASES;")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to list databases"})
			return
		}

		lines := strings.Split(string(output), "\n")
		var dbs []string
		for _, line := range lines {
			db := strings.TrimSpace(line)
			if db == "" {
				continue
			}
			if isProtectedMariaDBDatabase(db) {
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
		if bind(c, &req) {
			return
		}

		if !databaseNamePattern.MatchString(req.Name) || isProtectedMariaDBDatabase(req.Name) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or protected database name"})
			return
		}

		charset := "utf8"
		if req.Charset != "" && req.Charset != "utf8" && req.Charset != "utf8mb4" && req.Charset != "cp1251" && req.Charset != "latin1" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported database character set"})
			return
		}
		if req.Charset == "utf8mb4" || req.Charset == "cp1251" || req.Charset == "latin1" {
			charset = req.Charset
		}
		if len(req.Password) > 0 && (len(req.Password) < 12 || len(req.Password) > 256) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database password must contain 12 to 256 bytes"})
			return
		}

		password, err := panelMariaDBPassword(d.DB)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "panel database account is not configured"})
			return
		}
		createCmd := "CREATE DATABASE `" + req.Name + "` CHARACTER SET " + charset + ";"
		if req.Password != "" {
			passwordLiteral, err := platform.MariaDBStringLiteral(req.Password)
			if err != nil {
				c.JSON(400, gin.H{"error": "invalid database password"})
				return
			}
			account := "'" + req.Name + "'@'localhost'"
			createCmd += " CREATE USER IF NOT EXISTS " + account + " IDENTIFIED VIA mysql_native_password USING PASSWORD(" + passwordLiteral + ");"
			createCmd += " ALTER USER " + account + " IDENTIFIED VIA mysql_native_password USING PASSWORD(" + passwordLiteral + ");"
			createCmd += " GRANT ALL PRIVILEGES ON `" + req.Name + "`.* TO '" + req.Name + "'@'localhost';"
			createCmd += " FLUSH PRIVILEGES;"
		}
		if _, err := platform.MariaDBQuery(c.Request.Context(), "megopanel", password, createCmd); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create database"})
			return
		}
		if req.Password != "" {
			if err := d.DB.Save(&domain.Setting{Key: "db_password_" + req.Name, Value: req.Password}).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "database created but credentials could not be saved"})
				return
			}
		}
		c.JSON(200, gin.H{"ok": true, "name": req.Name})
	})

	protected.DELETE("/databases/:name", func(c *gin.Context) {
		name := c.Param("name")

		if !databaseNamePattern.MatchString(name) || isProtectedMariaDBDatabase(name) {
			c.JSON(400, gin.H{"error": "invalid database name"})
			return
		}
		password, err := panelMariaDBPassword(d.DB)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "panel database account is not configured"})
			return
		}
		if _, err := platform.MariaDBQuery(c.Request.Context(), "megopanel", password, "DROP DATABASE IF EXISTS `"+name+"`;"); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to delete database"})
			return
		}
		_, accountErr := platform.MariaDBQuery(c.Request.Context(), "megopanel", password, "DROP USER IF EXISTS '"+name+"'@'localhost';")
		if err := d.DB.Delete(&domain.Setting{}, "key = ?", "db_password_"+name).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database deleted but saved credentials could not be removed"})
			return
		}
		if accountErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "database was deleted but its database account could not be removed"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	issueAutologin := func(c *gin.Context) {
		var req struct {
			DB string `json:"db"`
		}
		if bind(c, &req) {
			return
		}
		if req.DB != "" && (!databaseNamePattern.MatchString(req.DB) || isProtectedMariaDBDatabase(req.DB)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid database name"})
			return
		}
		state, err := d.Install.Status("phpmyadmin")
		if err != nil || !state.Installed {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "phpMyAdmin is not installed"})
			return
		}
		dbUser := "megopanel"
		var passwordSetting domain.Setting
		settingKey := "mariadb_admin_password"
		if req.DB != "" {
			dbUser = req.DB
			settingKey = "db_password_" + req.DB
		}
		credentialErr := d.DB.First(&passwordSetting, "key = ?", settingKey).Error
		if req.DB != "" && (credentialErr != nil || passwordSetting.Value == "") {
			dbUser = "megopanel"
			settingKey = "mariadb_admin_password"
			passwordSetting = domain.Setting{}
			credentialErr = d.DB.First(&passwordSetting, "key = ?", settingKey).Error
		}
		if credentialErr != nil || passwordSetting.Value == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "database credentials are not configured"})
			return
		}
		if _, err := platform.MariaDBQuery(c.Request.Context(), dbUser, passwordSetting.Value, "SELECT 1;"); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MariaDB credentials are no longer valid; configure database access again"})
			return
		}
		if d.Autologin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "phpMyAdmin sign-on is unavailable"})
			return
		}
		token, err := platform.NewUUIDv7()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create sign-on request"})
			return
		}
		now := time.Now()
		expiresAt := now.Add(time.Minute)
		userID := extractUserID(c, d.Config.Security.JWTSecret)
		d.Autologin.Save(token, &service.PMAAutologinToken{
			UserID:        userID,
			MySQLUsername: dbUser,
			MySQLPassword: passwordSetting.Value,
			CreatedAt:     now,
			ExpiresAt:     expiresAt,
		})
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"url": "/phpmyadmin/signon.php#" + token})
	}
	protected.POST("/databases/autologin", issueAutologin)
	protected.POST("/phpmyadmin/autologin", issueAutologin)
	if d.Backups != nil {
		protected.GET("/backups/settings", func(c *gin.Context) {
			settings, err := d.Backups.Settings()
			respond(c, false, settings, err)
		})
		protected.PUT("/backups/settings", func(c *gin.Context) {
			var request service.BackupSettings
			if bind(c, &request) {
				return
			}
			settings, err := d.Backups.UpdateSettings(request)
			respond(c, false, settings, err)
		})
		protected.GET("/backups", func(c *gin.Context) {
			backups, err := d.Backups.List()
			respond(c, false, backups, err)
		})
		protected.GET("/backups/operations", func(c *gin.Context) {
			operations, err := d.Backups.Operations()
			respond(c, false, operations, err)
		})
		protected.POST("/backups", func(c *gin.Context) {
			var request service.BackupOptions
			if bind(c, &request) {
				return
			}
			backup, err := d.Backups.Create(c.Request.Context(), request)
			respond(c, false, backup, err)
		})
		protected.GET("/backups/:id/download", func(c *gin.Context) {
			path, err := d.Backups.DownloadPath(c.Param("id"))
			if err != nil {
				respond(c, false, nil, err)
				return
			}
			c.FileAttachment(path, c.Param("id")+".zip")
		})
		protected.POST("/backups/:id/verify", func(c *gin.Context) {
			backup, err := d.Backups.Verify(c.Param("id"))
			respond(c, false, backup, err)
		})
		protected.DELETE("/backups/:id", func(c *gin.Context) {
			respond(c, false, gin.H{"ok": true}, d.Backups.Delete(c.Param("id")))
		})
		protected.POST("/backups/:id/restore", func(c *gin.Context) {
			var request struct {
				Confirm bool `json:"confirm"`
			}
			if bind(c, &request) {
				return
			}
			if !request.Confirm {
				c.JSON(http.StatusBadRequest, gin.H{"error": "restore confirmation is required"})
				return
			}
			result, err := d.Backups.Restore(c.Request.Context(), c.Param("id"))
			respond(c, false, result, err)
		})
	}

	protected.GET("/update/status", func(c *gin.Context) {
		status, err := d.Update.CheckUpdate(c.Request.Context())
		respond(c, false, status, err)
	})
	protected.POST("/update/upgrade", func(c *gin.Context) {
		err := d.Update.Upgrade(c.Request.Context())
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	protected.GET("/install/:name/status", func(c *gin.Context) { state, err := d.Install.Status(c.Param("name")); respond(c, false, state, err) })
	protected.POST("/install/mariadb", func(c *gin.Context) {
		var req struct {
			RemoteAccess bool   `json:"remoteAccess"`
			RootPassword string `json:"rootPassword"`
		}
		if bind(c, &req) {
			return
		}
		if len(req.RootPassword) > 1024 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database password is too long"})
			return
		}
		state, err := d.Install.InstallMariaDB(c.Request.Context(), req.RemoteAccess, req.RootPassword)
		respond(c, false, state, err)
	})
	protected.POST("/install/nginx", func(c *gin.Context) { state, err := d.Install.InstallNginx(); respond(c, false, state, err) })
	protected.POST("/install/phpmyadmin", func(c *gin.Context) {
		port := "8888"
		if parts := strings.Split(d.Config.Server.Address, ":"); len(parts) > 0 && parts[len(parts)-1] != "" {
			port = parts[len(parts)-1]
		}
		state, err := d.Install.InstallPhpMyAdmin(port)
		respond(c, false, state, err)
	})
	protected.GET("/ws", func(c *gin.Context) {
		upgrader := websocket.Upgrader{}
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
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/internal/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.File("templates/frontend/index.html")
	})
	return r
}
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ws: wss:")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
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
func csrfMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		cookie, err := c.Cookie("csrf_token")
		header := c.GetHeader("X-CSRF-Token")
		if err != nil || !validCSRFCookie(cookie, cfg.Security.CSRFSecret) || len(cookie) != len(header) || subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
			c.AbortWithStatusJSON(403, gin.H{"error": "csrf validation failed"})
			return
		}
		c.Next()
	}
}
func issueCSRFCookie(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := c.Cookie("csrf_token"); err != nil || !validCSRFCookie(token, cfg.Security.CSRFSecret) {
			csrf, err := newCSRFCookie(cfg.Security.CSRFSecret)
			if err == nil {
				c.SetSameSite(http.SameSiteLaxMode)
				c.SetCookie("csrf_token", csrf, int(cfg.RefreshTTL().Seconds()), "/", "", cfg.Security.CookieSecure, false)
			}
		}
		c.Next()
	}
}
func setCookie(c *gin.Context, n, v string, age float64, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(n, v, int(age), "/", "", secure, true)
}
func setAuthCookies(c *gin.Context, cfg *config.Config, access, refresh string) error {
	csrf, err := newCSRFCookie(cfg.Security.CSRFSecret)
	if err != nil {
		return err
	}
	setCookie(c, "access_token", access, cfg.AccessTTL().Seconds(), cfg.Security.CookieSecure)
	setCookie(c, "refresh_token", refresh, cfg.RefreshTTL().Seconds(), cfg.Security.CookieSecure)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("csrf_token", csrf, int(cfg.RefreshTTL().Seconds()), "/", "", cfg.Security.CookieSecure, false)
	return nil
}

func newCSRFCookie(secret string) (string, error) {
	if len(secret) < 32 {
		return "", errors.New("CSRF signing secret must contain at least 32 bytes")
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	nonceText := hex.EncodeToString(nonce)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(nonceText))
	return nonceText + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func validCSRFCookie(token, secret string) bool {
	if len(secret) < 32 {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(parts[0]) != 64 || len(parts[1]) != 64 {
		return false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return false
	}
	suppliedMAC, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	return hmac.Equal(suppliedMAC, mac.Sum(nil))
}
func bind(c *gin.Context, target interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
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
func extractUserID(c *gin.Context, jwtSecret string) string {
	tokenStr, err := c.Cookie("access_token")
	if err != nil {
		return "unknown"
	}
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return "unknown"
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if sub, ok := claims["sub"].(string); ok {
			return sub
		}
	}
	return "unknown"
}

func requestRemoteIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remoteAddr, "[]")
}

func isProtectedMariaDBDatabase(name string) bool {
	switch strings.ToLower(name) {
	case "information_schema", "mysql", "performance_schema", "sys":
		return true
	default:
		return false
	}
}

func panelMariaDBPassword(db *gorm.DB) (string, error) {
	var setting domain.Setting
	if err := db.First(&setting, "key = ?", "mariadb_admin_password").Error; err != nil {
		return "", err
	}
	if setting.Value == "" {
		return "", gorm.ErrRecordNotFound
	}
	return setting.Value, nil
}
