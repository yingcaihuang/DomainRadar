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
	seedDefaultTagsAndGroups(db, logger)

	// Initialize handlers
	authHandler := auth.NewAuthHandler(oidcProvider, sm, db, logger)

	// Try to initialize OIDC from database config (overrides env-based provider)
	auth.InitializeOIDCFromDB(db, authHandler, logger)

	userHandler := auth.NewUserHandler(db, logger)
	ssoConfigHandler := auth.NewSSOConfigHandler(db, logger, authHandler)
	groupMappingHandler := auth.NewGroupMappingHandler(db, logger)
	dashboardHandler := dashboard.NewDashboardHandler(db, logger)
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
	if cryptoService != nil {
		syncScheduler.Start(context.Background())
	} else {
		logger.Warn("Automatic registrar sync disabled: credential encryption service is not configured")
	}
	registrarHandler := adapter.NewRegistrarHandler(db, cryptoService, adapterRegistry, auditService, logger, adapter.WithSyncTrigger(syncScheduler))

	// WHOIS scheduler (daily) — refreshes manually added domains only.
	whoisScheduler := domainmgmt.NewWhoisScheduler(db, logger)

	domainHandler := domainmgmt.NewDomainHandler(db, auditService, logger,
		domainmgmt.WithAdapterRegistry(adapterRegistry),
		domainmgmt.WithCryptoService(cryptoService),
		domainmgmt.WithWhoisScheduler(whoisScheduler),
	)

	// Notification handler with channel registry
	channelRegistry := notification.NewChannelRegistry()
	channelRegistry.Register("webhook", func(config *notification.ChannelConfig) notification.NotificationChannel {
		return notification.NewWebhookChannel(config)
	})
	channelRegistry.Register("cne_alarm", func(config *notification.ChannelConfig) notification.NotificationChannel {
		return notification.NewCNEAlarmChannel(config)
	})
	notificationHandler := notification.NewNotificationHandler(db, cryptoService, channelRegistry, auditService, logger)

	// Rules handlers
	rulesHandler := config.NewRulesHandler(db, logger)
	emailRulesHandler := config.NewEmailRulesHandler(db)

	// Certificate monitoring handler
	certHandler := certcheck.NewCertHandler(db, logger)

	// Email monitoring handler
	emailHandler := emailcheck.NewEmailHandler(db, logger)

	// Create notification dispatcher for webhook alerts
	alertDispatcher := notification.NewAlertDispatcher(db, cryptoService, channelRegistry, logger)

	// Sync failure alerts go through the same dispatch chain
	syncScheduler.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}

	// Start alert scheduler (health score updates + expiration alerts)
	alertScheduler := alert.NewAlertScheduler(db, logger)
	alertScheduler.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}
	alertScheduler.Start(context.Background())

	// Start certificate scheduler (periodic TLS checks)
	certScheduler := certcheck.NewCertScheduler(db, logger, 0)
	certScheduler.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}
	certScheduler.Start(context.Background())

	// Start email monitoring scheduler (periodic email DNS checks)
	emailScheduler := emailcheck.NewEmailScheduler(db, logger, 0)
	emailScheduler.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}
	emailScheduler.Start(context.Background())

	// Start website health monitor (periodic HTTP checks + downtime alerts)
	websiteMonitor := monitor.NewWebsiteMonitor(db, logger)
	websiteMonitor.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}
	websiteMonitor.Start(context.Background())

	// Start WHOIS scheduler (daily)
	whoisScheduler.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}
	whoisScheduler.Start(context.Background())

	// Start service monitor scheduler (periodic probe checks)
	monitorScheduler := monitor.NewMonitorScheduler(db, logger)
	monitorScheduler.OnAlertCreated = func(a *domain.Alert) {
		alertDispatcher.DispatchAlert(a)
	}
	monitorScheduler.Start(context.Background())

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
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
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

// seedDefaultTagsAndGroups creates default tags and groups if they don't exist.
func seedDefaultTagsAndGroups(db *gorm.DB, logger *zap.Logger) {
	// Default tags
	defaultTags := []string{"不续费", "no-renew", "muted", "ignore", "AI"}
	for _, name := range defaultTags {
		tag := domain.Tag{Name: name}
		if err := db.Where("name = ?", name).FirstOrCreate(&tag).Error; err != nil {
			logger.Warn("Failed to seed tag", zap.String("name", name), zap.Error(err))
		}
	}

	// Default groups
	defaultGroups := []string{"MAP2.0", "AWS"}
	for _, name := range defaultGroups {
		group := domain.Group{Name: name, Level: 0}
		if err := db.Where("name = ?", name).FirstOrCreate(&group).Error; err != nil {
			logger.Warn("Failed to seed group", zap.String("name", name), zap.Error(err))
		}
	}

	logger.Info("Default tags and groups seeded")

	// Seed default CNE alarm channel if not exists
	var cneCount int64
	db.Model(&domain.NotificationChannel{}).Where("channel_type = ?", "cne_alarm").Count(&cneCount)
	if cneCount == 0 {
		// Encrypt the default config
		masterKey := os.Getenv("DOMAINRADAR_MASTER_KEY")
		if masterKey != "" {
			cryptoSvc, cryptoErr := crypto.NewCryptoService(masterKey)
			if cryptoErr == nil {
				configJSON := `{"api_url":"https://cne.verycloud.cn/monitor/api/v1/alarms","api_key":"4d3c2b1a-9f8e-47d6-a5c4-123456789abc","alarm_code":"2809"}`
				encrypted, err := cryptoSvc.Encrypt(configJSON)
				if err == nil {
					ch := domain.NotificationChannel{
						ChannelType:     "cne_alarm",
						Name:            "CNE 告警平台",
						ConfigEncrypted: encrypted,
						Status:          "active",
					}
					if err := db.Create(&ch).Error; err == nil {
						logger.Info("Default CNE alarm channel created")
					}
				}
			}
		}
	}
}

// Build trigger: 1787227235
