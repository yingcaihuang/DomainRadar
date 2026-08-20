package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"domainradar/internal/adapter"
	adapterAlibaba "domainradar/internal/adapter/alibaba"
	adapterCloudflare "domainradar/internal/adapter/cloudflare"
	adapterGodaddy "domainradar/internal/adapter/godaddy"
	adapterNamecheap "domainradar/internal/adapter/namecheap"
	adapterTencent "domainradar/internal/adapter/tencent"
	"domainradar/internal/alert"
	"domainradar/internal/audit"
	"domainradar/internal/auth"
	"domainradar/internal/certcheck"
	"domainradar/internal/config"
	"domainradar/internal/crypto"
	"domainradar/internal/dashboard"
	"domainradar/internal/domain"
	"domainradar/internal/domainmgmt"
	"domainradar/internal/emailcheck"
	"domainradar/internal/monitor"
	"domainradar/internal/notification"
	"domainradar/internal/sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
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

	// Seed default admin user if no users exist
	seedDefaultAdmin(db, logger)

	// Initialize handlers
	authHandler := auth.NewAuthHandler(oidcProvider, sm, db, logger)

	// Try to initialize OIDC from database config (overrides env-based provider)
	auth.InitializeOIDCFromDB(db, authHandler, logger)

	userHandler := auth.NewUserHandler(db, logger)
	ssoConfigHandler := auth.NewSSOConfigHandler(db, logger, authHandler)
	groupMappingHandler := auth.NewGroupMappingHandler(db, logger)
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

	// Rules handlers
	rulesHandler := config.NewRulesHandler(db, logger)
	emailRulesHandler := config.NewEmailRulesHandler(db)

	// Certificate monitoring handler
	certHandler := certcheck.NewCertHandler(db, logger)

	// Email monitoring handler
	emailHandler := emailcheck.NewEmailHandler(db, logger)

	// Start alert scheduler (health score updates + expiration alerts)
	alertScheduler := alert.NewAlertScheduler(db, logger)
	alertScheduler.Start(context.Background())

	// Start certificate scheduler (periodic TLS checks)
	certScheduler := certcheck.NewCertScheduler(db, logger, 0)
	certScheduler.Start(context.Background())

	// Start email monitoring scheduler (periodic email DNS checks)
	emailScheduler := emailcheck.NewEmailScheduler(db, logger, 0)
	emailScheduler.Start(context.Background())

	// Initialize router
	router := setupRouter(
		sm,
		authHandler,
		userHandler,
		ssoConfigHandler,
		groupMappingHandler,
		dashboardHandler,
		domainHandler,
		alertHandler,
		monitorHandler,
		registrarHandler,
		notificationHandler,
		rulesHandler,
		certHandler,
		emailHandler,
		emailRulesHandler,
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
	ssoConfigHandler *auth.SSOConfigHandler,
	groupMappingHandler *auth.GroupMappingHandler,
	dashboardHandler *dashboard.DashboardHandler,
	domainHandler *domainmgmt.DomainHandler,
	alertHandler *alert.AlertHandler,
	monitorHandler *monitor.MonitorHandler,
	registrarHandler *adapter.RegistrarHandler,
	notificationHandler *notification.NotificationHandler,
	rulesHandler *config.RulesHandler,
	certHandler *certcheck.CertHandler,
	emailHandler *emailcheck.EmailHandler,
	emailRulesHandler *config.EmailRulesHandler,
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
		if authHandler != nil {
			authHandler.RegisterRoutes(v1)
		}

		// Protected routes — require authentication
		protected := v1.Group("")
		if sm != nil {
			protected.Use(auth.AuthMiddleware(sm))
		}
		{
			// Dashboard and reports (includes /audit-logs)
			if dashboardHandler != nil {
				dashboardHandler.RegisterRoutes(protected)
			}

			// Domains, Tags, Groups
			if domainHandler != nil {
				domainHandler.RegisterRoutes(protected)
			}

			// Alerts
			if alertHandler != nil {
				alertHandler.RegisterRoutes(protected)
			}

			// Monitoring
			if monitorHandler != nil {
				monitorHandler.RegisterRoutes(protected)
			}

			// Registrars
			if registrarHandler != nil {
				registrarHandler.RegisterRoutes(protected)
			}

			// Notifications
			if notificationHandler != nil {
				notificationHandler.RegisterRoutes(protected)
			}

			// User management
			if userHandler != nil {
				userHandler.RegisterRoutes(protected)
			}

			// Configuration
			if rulesHandler != nil {
				rulesHandler.RegisterRoutes(protected)
			emailRulesHandler.RegisterRoutes(protected)
			}

			// SSO Configuration (admin only)
			if ssoConfigHandler != nil {
				ssoConfigHandler.RegisterRoutes(protected)
			}

			// Group Mappings (admin only)
			if groupMappingHandler != nil {
				groupMappingHandler.RegisterRoutes(protected)
			}

			// Certificate monitoring
			if certHandler != nil {
				certHandler.RegisterRoutes(protected)
			}

			// Email monitoring
			if emailHandler != nil {
				emailHandler.RegisterRoutes(protected)
			}
		}
	}

	return router
}

// seedDefaultAdmin creates a default admin user if no users exist in the database.
// This enables first-deploy login with admin/admin123.
func seedDefaultAdmin(db *gorm.DB, logger *zap.Logger) {
	var count int64
	if err := db.Model(&domain.User{}).Count(&count).Error; err != nil {
		logger.Warn("Failed to check user count for seeding", zap.Error(err))
		return
	}

	if count > 0 {
		return // Users already exist, skip seeding
	}

	// Hash the default password
	hash, err := auth.HashPassword("admin123")
	if err != nil {
		logger.Error("Failed to hash default admin password", zap.Error(err))
		return
	}

	now := time.Now()
	user := domain.User{
		ExternalID:         "admin",
		Email:              "admin@localhost",
		DisplayName:        "Administrator",
		PasswordHash:       hash,
		AuthSource:         "local",
		MustChangePassword: true,
		LastLoginAt:        &now,
	}

	if err := db.Create(&user).Error; err != nil {
		logger.Error("Failed to create default admin user", zap.Error(err))
		return
	}

	// Assign admin role
	role := domain.UserRole{
		UserID: user.ID,
		Role:   auth.RoleAdmin,
	}
	if err := db.Create(&role).Error; err != nil {
		logger.Error("Failed to assign admin role to default user", zap.Error(err))
		return
	}

	logger.Info("Default admin user created",
		zap.String("username", "admin"),
		zap.String("password", "admin123"),
		zap.String("note", "Change this password on first login!"),
	)
}
