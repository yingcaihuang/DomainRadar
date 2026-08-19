package domain

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DatabaseConfig holds connection pool settings for the database.
type DatabaseConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DefaultDatabaseConfig returns sensible default connection pool settings.
func DefaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 5 * time.Minute,
	}
}

// NewDatabase creates a new GORM database connection with configured connection pooling.
// The databaseURL should be a PostgreSQL connection string (e.g. "postgres://user:pass@host:5432/db?sslmode=disable").
func NewDatabase(databaseURL string) (*gorm.DB, error) {
	return NewDatabaseWithConfig(databaseURL, DefaultDatabaseConfig())
}

// NewDatabaseWithConfig creates a new GORM database connection with the provided configuration.
func NewDatabaseWithConfig(databaseURL string, config DatabaseConfig) (*gorm.DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	// Configure GORM logger level based on environment
	logLevel := logger.Warn
	if os.Getenv("GIN_MODE") == "debug" || os.Getenv("GIN_MODE") == "" {
		logLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)

	return db, nil
}

// AutoMigrate runs GORM auto-migration for all models.
// This should only be used in development environments.
func AutoMigrate(db *gorm.DB, log *zap.Logger) error {
	if log != nil {
		log.Info("Running database auto-migration")
	}

	models := AllModels()
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto-migration failed: %w", err)
	}

	if log != nil {
		log.Info("Database auto-migration completed successfully")
	}
	return nil
}

// NewDatabaseFromEnv creates a database connection using the DATABASE_URL environment variable.
// It also runs auto-migration if AUTO_MIGRATE=true is set.
func NewDatabaseFromEnv(log *zap.Logger) (*gorm.DB, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		// Build URL from individual PostgreSQL environment variables as fallback
		host := getEnvOrDefault("POSTGRES_HOST", "localhost")
		port := getEnvOrDefault("POSTGRES_PORT", "5432")
		user := getEnvOrDefault("POSTGRES_USER", "domainradar")
		password := getEnvOrDefault("POSTGRES_PASSWORD", "domainradar_secret")
		dbName := getEnvOrDefault("POSTGRES_DB", "domainradar")
		sslMode := getEnvOrDefault("POSTGRES_SSLMODE", "disable")

		databaseURL = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user, password, host, port, dbName, sslMode,
		)
	}

	if log != nil {
		log.Info("Connecting to database", zap.String("host", maskDSN(databaseURL)))
	}

	db, err := NewDatabase(databaseURL)
	if err != nil {
		return nil, err
	}

	// Run auto-migration in development mode
	if os.Getenv("AUTO_MIGRATE") == "true" {
		if err := AutoMigrate(db, log); err != nil {
			return nil, err
		}
	}

	if log != nil {
		log.Info("Database connection established successfully")
	}

	return db, nil
}

// getEnvOrDefault returns the environment variable value or the default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// maskDSN masks the password in a database connection string for logging.
func maskDSN(dsn string) string {
	// Simple approach: just indicate we have a DSN without exposing credentials
	if len(dsn) > 20 {
		return dsn[:20] + "..."
	}
	return "***"
}
