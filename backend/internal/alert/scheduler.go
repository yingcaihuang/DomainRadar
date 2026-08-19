package alert

import (
	"context"
	"fmt"
	"math"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AlertScheduler scans domains and generates expiration alerts + updates health scores.
type AlertScheduler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAlertScheduler creates a new AlertScheduler.
func NewAlertScheduler(db *gorm.DB, logger *zap.Logger) *AlertScheduler {
	return &AlertScheduler{db: db, logger: logger}
}

// RunExpirationCheck scans all domains and:
// 1. Updates health_score based on days until expiration
// 2. Creates alert records for expired / expiring domains
func (s *AlertScheduler) RunExpirationCheck(ctx context.Context) error {
	var domains []domain.NormalizedDomain
	if err := s.db.WithContext(ctx).
		Where("expiration_date IS NOT NULL").
		Find(&domains).Error; err != nil {
		return fmt.Errorf("failed to query domains: %w", err)
	}

	now := time.Now()
	for _, d := range domains {
		if d.ExpirationDate == nil {
			continue
		}

		daysRemaining := int(math.Ceil(d.ExpirationDate.Sub(now).Hours() / 24))
		healthScore := calculateHealthScore(daysRemaining)

		// Update health score
		s.db.WithContext(ctx).Model(&d).Update("health_score", healthScore)

		// Determine if alert is needed
		var severity string
		var alertType string
		switch {
		case daysRemaining < 0:
			severity = "critical"
			alertType = "domain_expired"
		case daysRemaining <= 7:
			severity = "critical"
			alertType = "domain_expiring_7d"
		case daysRemaining <= 30:
			severity = "warning"
			alertType = "domain_expiring_30d"
		default:
			continue // No alert needed
		}

		// Check if we already have an unacknowledged alert for this domain + type
		var existing domain.Alert
		result := s.db.WithContext(ctx).Where(
			"domain_id = ? AND alert_type = ? AND acknowledged = ?",
			d.ID, alertType, false,
		).First(&existing)

		if result.Error == gorm.ErrRecordNotFound {
			// Create new alert
			alert := domain.Alert{
				DomainID:     d.ID,
				AlertType:    alertType,
				Severity:     severity,
				Message:      formatAlertMessage(d.DomainName, daysRemaining),
				DaysRemaining: &daysRemaining,
				Acknowledged: false,
				GeneratedAt:  now,
			}
			if err := s.db.WithContext(ctx).Create(&alert).Error; err != nil {
				s.logger.Error("failed to create alert", zap.String("domain", d.DomainName), zap.Error(err))
			}
		}
	}

	s.logger.Info("Expiration check completed", zap.Int("domains_checked", len(domains)))
	return nil
}

// calculateHealthScore returns a score 0-100 based on days until expiration.
func calculateHealthScore(daysRemaining int) int {
	switch {
	case daysRemaining < 0:
		return 0 // expired
	case daysRemaining <= 7:
		return 20
	case daysRemaining <= 30:
		return 50
	case daysRemaining <= 90:
		return 75
	default:
		return 100
	}
}

// formatAlertMessage generates a human-readable alert message.
func formatAlertMessage(domainName string, daysRemaining int) string {
	if daysRemaining < 0 {
		return fmt.Sprintf("域名 %s 已过期 %d 天", domainName, -daysRemaining)
	}
	if daysRemaining == 0 {
		return fmt.Sprintf("域名 %s 今天到期", domainName)
	}
	return fmt.Sprintf("域名 %s 将在 %d 天后到期", domainName, daysRemaining)
}

// Start launches the alert scheduler as a background goroutine, running checks periodically.
func (s *AlertScheduler) Start(ctx context.Context) {
	// Run immediately on startup
	if err := s.RunExpirationCheck(ctx); err != nil {
		s.logger.Error("Initial expiration check failed", zap.Error(err))
	}

	// Then run every 6 hours
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RunExpirationCheck(context.Background()); err != nil {
					s.logger.Error("Periodic expiration check failed", zap.Error(err))
				}
			}
		}
	}()

	s.logger.Info("Alert scheduler started (checks every 6 hours)")
}
