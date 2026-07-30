package http

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"mego-panel/backend/internal/config"
	"mego-panel/backend/internal/service"
	"net/http"
)

type RouterDeps struct {
	Config    *config.Config
	Auth      *service.AuthService
	Dashboard *service.DashboardService
	Install   *service.InstallService
}

func NewRouter(d RouterDeps) *gin.Engine {
	if d.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders(), cors.New(cors.Config{AllowOrigins: []string{d.Config.Server.FrontendURL}, AllowCredentials: true, AllowHeaders: []string{"Content-Type", "X-CSRF-Token"}, AllowMethods: []string{"GET", "POST", "OPTIONS"}}))
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
	protected.GET("/install/:name/status", func(c *gin.Context) { state, err := d.Install.Status(c.Param("name")); respond(c, false, state, err) })
	protected.POST("/install/mariadb", func(c *gin.Context) {
		var req struct {
			RemoteAccess bool `json:"remoteAccess"`
		}
		_ = c.ShouldBindJSON(&req)
		state, err := d.Install.InstallMariaDB(req.RemoteAccess)
		respond(c, false, state, err)
	})
	protected.POST("/install/nginx", func(c *gin.Context) { state, err := d.Install.InstallNginx(); respond(c, false, state, err) })
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
