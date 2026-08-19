package main

import (
	"context"
	"net/http"
	"os"

	"domainradar/internal/adapter"
	adapterAlibaba "domainradar/internal/adapter/alibaba"
	adapterCloudflare "domainradar/internal/adapter/cloudflare"
	adapterGodaddy "domainradar/internal/adapter/godaddy"
	adapterNamecheap "domainradar/internal/adapter/namecheap"
	adapterTencent "domainradar/internal/adapter/tencent"
	"domainradar/internal/alert"
	"domainradar/internal/audit"
	"domainradar/internal/auth"
	"domainradar/internal/config"
	"domainradar/internal/crypto"
	"domainradar/internal/dashboard"
	"domainradar/internal/domain"
	"domainradar/internal/domainmgmt"
	"domainradar/internal/monitor"
	"domainradar/internal/notification"
	"domainradar/internal/sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	defer logger.Sync()

	// Set Gin mode from environment
	mode := os.Getenv("GIN_MODE")
	if mode == "" {
		mode = gin.DebugMode
	}
	gin.SetMode(mode)

	// Initialize database
	db, err := domain.NewDatabaseFromEnv(logger)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	// Initialize crypto service
	cryptoService, err := crypto.NewCryptoServiceFromEnv()
	if err != nil {
		logger.Warn("Failed to initialize crypto service, credential encryption disabled", zap.Error(err))
	}

	// Initialize OIDC provider (may be nil if not configured)
	var oidcProvider *auth.OIDCProvider
	oidcIssuer := os.Getenv("OIDC_ISSUER_URL")
	if oidcIssuer != "" && os.Getenv("OIDC_CLIENT_ID") != "" {
		provider, err := auth.NewOIDCProviderFromEnv(context.Background())
		if err != nil {
			logger.Warn("Failed to initialize OIDC provider, falling back to dev mode", zap.Error(err))
		} else {
			oidcProvider = provider
		}
	}

	devMode := oidcProvider == nil
	if devMode {
		logger.Info("OIDC not configured — running in DEV MODE with local admin login")
	} else {
		logger.Info("OIDC configured — running in SSO mode")
	}

	// Initialize services
	sm := auth.NewSessionManager(0) // 0 = default 24h TTL
	auditService := audit.NewService(db, logger)

	// Initialize handlers
	authHandler := auth.NewAuthHandler(oidcProvider, sm, db, logger)
	userHandler := auth.NewUserHandler(db, logger)
	dashboardHandler := dashboard.NewDashboardHandler(db, logger)
	domainHandler := domainmgmt.NewDomainHandler(db, auditService, logger)
	alertHandler := alert.NewAlertHandler(db, logger)
	monitorHandler := monitor.NewMonitorHandler(db, logger)

	// Registrar handler with adapter registry (register all supported adapters)
	adapterRegistry := adapter.NewAdapterRegistry()
	adapterRegistry.Register(adapterGodaddy.New())
	adapterRegistry.Register(adapterCloudflare.New())
	adapterRegistry.Register(adapterAlibaba.New())
	adapterRegistry.Register(adapterNamecheap.New())
	adapterRegistry.Register(adapterTencent.New())
	// Sync scheduler for manual and automatic domain sync
	syncScheduler := sync.NewSyncScheduler(db, adapterRegistry, cryptoService, logger)
	registrarHandler := adapter.NewRegistrarHandler(db, cryptoService, adapterRegistry, auditService, logger, adapter.WithSyncTrigger(syncScheduler))

	// Notification handler with channel registry
	channelRegistry := notification.NewChannelRegistry()
	notificationHandler := notification.NewNotificationHandler(db, cryptoService, channelRegistry, auditService, logger)

	// Rules handler
	rulesHandler := config.NewRulesHandler(db, logger)

	// Start alert scheduler (health score updates + expiration alerts)
	alertScheduler := alert.NewAlertScheduler(db, logger)
	alertScheduler.Start(context.Background())

	// Initialize router
	router := setupRouter(
		sm,
		authHandler,
		userHandler,
		dashboardHandler,
		domainHandler,
		alertHandler,
		monitorHandler,
		registrarHandler,
		notificationHandler,
		rulesHandler,
	)

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("Starting DomainRadar API server", zap.String("port", port))
	if err := router.Run(":" + port); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
}

func setupRouter(
	sm *auth.SessionManager,
	authHandler *auth.AuthHandler,
	userHandler *auth.UserHandler,
	dashboardHandler *dashboard.DashboardHandler,
	domainHandler *domainmgmt.DomainHandler,
	alertHandler *alert.AlertHandler,
	monitorHandler *monitor.MonitorHandler,
	registrarHandler *adapter.RegistrarHandler,
	notificationHandler *notification.NotificationHandler,
	rulesHandler *config.RulesHandler,
) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// System health endpoint (no auth required)
		v1.GET("/system/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
			})
		})

		// Auth routes (no auth required)
		authHandler.RegisterRoutes(v1)

		// Protected routes — require authentication
		protected := v1.Group("")
		protected.Use(auth.AuthMiddleware(sm))
		{
			// Dashboard and reports (includes /audit-logs)
			dashboardHandler.RegisterRoutes(protected)

			// Domains, Tags, Groups
			domainHandler.RegisterRoutes(protected)

			// Alerts
			alertHandler.RegisterRoutes(protected)

			// Monitoring
			monitorHandler.RegisterRoutes(protected)

			// Registrars
			registrarHandler.RegisterRoutes(protected)

			// Notifications
			notificationHandler.RegisterRoutes(protected)

			// User management
			userHandler.RegisterRoutes(protected)

			// Configuration
			rulesHandler.RegisterRoutes(protected)
		}
	}

	return router
}
