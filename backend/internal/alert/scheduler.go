package alert

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AlertDispatchFunc is a callback to send webhook notifications for new alerts.
type AlertDispatchFunc func(alert *domain.Alert)

// AlertScheduler scans domains and generates expiration alerts + updates health scores.
type AlertScheduler struct {
	db             *gorm.DB
	logger         *zap.Logger
	OnAlertCreated AlertDispatchFunc
}

// NewAlertScheduler creates a new AlertScheduler.
func NewAlertScheduler(db *gorm.DB, logger *zap.Logger) *AlertScheduler {
	return &AlertScheduler{db: db, logger: logger}
}

// RunExpirationCheck scans all domains and:
// 1. Updates health_score based on days until expiration
// 2. Creates alert records for expired / expiring domains
// muteTagNames are tag names that suppress alerts for a domain.
var muteTagNames = []string{"不续费", "no-renew", "muted", "ignore"}

func (s *AlertScheduler) RunExpirationCheck(ctx context.Context) error {
	var domains []domain.NormalizedDomain
	if err := s.db.WithContext(ctx).
		Preload("Tags").
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

		// Auto-resolve stale alerts whose condition has cleared (e.g. after a
		// renewal). Without this, unacknowledged alerts would linger forever
		// and the dedup check below would suppress future alerts of the same
		// type — e.g. a renewed domain's old "expired" alert would block next
		// year's expiration alert.
		s.resolveClearedAlerts(ctx, d.ID, daysRemaining)

		// Skip alert if domain has a mute tag (e.g. "不续费")
		if hasMuteTag(d.Tags) {
			continue
		}

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
				DomainID:      &d.ID,
				AlertType:     alertType,
				Severity:      severity,
				Message:       formatAlertMessage(d.DomainName, daysRemaining),
				DaysRemaining: &daysRemaining,
				Acknowledged:  false,
				GeneratedAt:   now,
			}
			if err := s.db.WithContext(ctx).Create(&alert).Error; err != nil {
				s.logger.Error("failed to create alert", zap.String("domain", d.DomainName), zap.Error(err))
			} else if s.OnAlertCreated != nil {
				s.OnAlertCreated(&alert)
			}
		}
	}

	s.logger.Info("Expiration check completed", zap.Int("domains_checked", len(domains)))
	return nil
}

// clearedAlertTypes returns the expiration alert types whose condition no
// longer holds for the given days remaining: "expired" clears once the
// domain is no longer expired, the 7d/30d warnings clear once the domain is
// more than 7/30 days from expiry.
func clearedAlertTypes(daysRemaining int) []string {
	var types []string
	if daysRemaining >= 0 {
		types = append(types, "domain_expired")
	}
	if daysRemaining > 7 {
		types = append(types, "domain_expiring_7d")
	}
	if daysRemaining > 30 {
		types = append(types, "domain_expiring_30d")
	}
	return types
}

// resolveClearedAlerts acknowledges unacknowledged expiration alerts of the
// given domain whose condition has cleared (auto-close on renewal).
func (s *AlertScheduler) resolveClearedAlerts(ctx context.Context, domainID uint, daysRemaining int) {
	types := clearedAlertTypes(daysRemaining)
	if len(types) == 0 {
		return
	}
	if err := s.db.WithContext(ctx).Model(&domain.Alert{}).
		Where("domain_id = ? AND alert_type IN ? AND acknowledged = ?", domainID, types, false).
		Updates(map[string]interface{}{"acknowledged": true, "acknowledged_at": time.Now()}).Error; err != nil {
		s.logger.Error("failed to auto-resolve cleared alerts",
			zap.Uint("domain_id", domainID),
			zap.Error(err),
		)
	}
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

// hasMuteTag checks if the domain has a tag that suppresses alerts.
func hasMuteTag(tags []domain.Tag) bool {
	for _, tag := range tags {
		for _, mute := range muteTagNames {
			if strings.EqualFold(tag.Name, mute) {
				return true
			}
		}
	}
	return false
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
