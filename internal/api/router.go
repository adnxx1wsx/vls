package api

import (
	"net/http"

	"vless-audit/internal/model"
	"vless-audit/internal/store"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// NewRouter creates the Gin engine with all routes.
func NewRouter(s *store.Store, connChan <-chan *model.Connection, staticFS http.FileSystem, authToken, xrayConfigPath, xrayBinPath, registerSecret, accessLogPath string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.Default())

	// Auth middleware.
	if authToken != "" {
		r.Use(AuthMiddleware(authToken))
	}

	broker := NewSSEBroker()
	BridgeConnToSSE(connChan, broker)
	StartPeriodicSSE(s, broker, 5)

	h := &Handler{
		Store:          s,
		ConnChan:       connChan,
		SSEBroker:      broker,
		XrayConfigPath:  xrayConfigPath,
		XrayBinPath:     xrayBinPath,
		RegisterSecret:  registerSecret,
	}

	// Public: login + self-service registration.
	r.POST("/api/auth/login", LoginHandler(authToken))
	r.POST("/api/register", h.RegisterHandler)
	r.GET("/api/register/:email", h.RegisterLookup)

	// API routes.
	api := r.Group("/api")
	{
		api.GET("/stats/realtime", h.RealtimeStats)
		api.GET("/traffic/summary", h.TrafficSummary)
		api.GET("/traffic/top", h.TopUsers)
		api.GET("/targets/top", h.TopTargets)
		api.GET("/connections", h.Connections)
		api.GET("/users", h.UsersList)
		api.GET("/users/:email/timeline", h.UserTimeline)
		api.GET("/users/:email/traffic", h.UserTraffic)
		api.GET("/online", h.OnlineUsers)
		api.GET("/heatmap", h.HourlyHeatmap)
		api.GET("/events/stream", h.EventsStream)
		// Admin: user management.
		api.GET("/admin/users", h.AdminListUsers)
		api.POST("/admin/users", h.AdminCreateUser)
		api.DELETE("/admin/users/:email", h.AdminDeleteUser)
		// Export.
		api.GET("/export/connections", ExportConnectionsCSV(s))
		// Logs.
		api.GET("/logs/access", TailAccessLog(accessLogPath))
		api.GET("/stats/storage", h.StorageStats)
		// Client app telemetry.
		api.POST("/client/register", h.DeviceRegister)
		api.POST("/client/report", h.ReportTelemetry)
		api.GET("/client/devices", h.ListDevices)
		api.GET("/client/devices/:device_id/reports", h.DeviceReports)
	}

	// Static files (SPA) — accessible without auth (login page is here).
	if staticFS != nil {
		r.GET("/", func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, "/app/")
		})
		r.GET("/app/*filepath", func(c *gin.Context) {
			c.Request.URL.Path = c.Param("filepath")
			ServeStatic(staticFS)(c)
		})
	}

	return r
}
