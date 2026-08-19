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

// AlertSeverity constants representing proximity-based alert levels.
const (
	SeverityInformational = "informational" // 90-31 days
	SeverityWarning       = "warning"       // 30-8 days
	SeverityCritical      = "critical"      // 7-0 days
	SeverityExpired       = "expired"       // past due
)

// AlertEngine evaluates domain expiration dates and generates alert records.
type AlertEngine struct {
	db         *gorm.DB
	logger     *zap.Logger
	thresholds []int
}

// NewAlertEngine creates a new AlertEngine with default thresholds.
func NewAlertEngine(db *gorm.DB, logger *zap.Logger) *AlertEngine {
	return &AlertEngine{
		db:         db,
		logger:     logger,
		thresholds: DefaultThresholds(),
	}
}

// DefaultThresholds returns the default alert threshold days.
func DefaultThresholds() []int {
	return []int{90, 30, 14, 7, 3, 1}
}

// SetThresholds allows configuring custom alert thresholds.
func (e *AlertEngine) SetThresholds(thresholds []int) {
	e.thresholds = thresholds
}

// CalculateSeverity determines the alert severity level based on days remaining
// until expiration and whether auto-renew is enabled.
//
// Base severity from proximity:
//   - daysRemaining < 0: "expired"
//   - daysRemaining 0-7: "critical"
//   - daysRemaining 8-30: "warning"
//   - daysRemaining 31-90: "informational"
//
// Escalation: if auto-renew is disabled and within 30 days, warning escalates to critical.
func CalculateSeverity(daysRemaining int, autoRenew bool) string {
	var severity string
	switch {
	case daysRemaining < 0:
		severity = SeverityExpired
	case daysRemaining <= 7:
		severity = SeverityCritical
	case daysRemaining <= 30:
		severity = SeverityWarning
	default:
		severity = SeverityInformational
	}

	// Escalate severity by one level if auto-renew is disabled and within 30 days
	if !autoRenew && daysRemaining <= 30 && severity == SeverityWarning {
		severity = SeverityCritical
	}

	return severity
}

// RunExpirationCheck evaluates all domains with non-nil expiration dates,
// calculates days remaining, determines severity, checks if a threshold is hit,
// and creates alert records for new threshold crossings.
// The function enforces a 10-minute context timeout.
func (e *AlertEngine) RunExpirationCheck(ctx context.Context) error {
	// Enforce 10-minute timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	e.logger.Info("starting expiration check cycle")

	// Load all domains with non-nil expiration dates
	var domains []domain.NormalizedDomain
	if err := e.db.WithContext(ctx).
		Where("expiration_date IS NOT NULL").
		Find(&domains).Error; err != nil {
		e.logger.Error("failed to load domains for expiration check", zap.Error(err))
		return fmt.Errorf("loading domains: %w", err)
	}

	e.logger.Info("loaded domains for expiration check", zap.Int("count", len(domains)))

	now := time.Now()
	var alertsCreated int

	for _, d := range domains {
		select {
		case <-ctx.Done():
			e.logger.Warn("expiration check aborted due to timeout",
				zap.Int("domains_processed", alertsCreated),
				zap.Error(ctx.Err()))
			return fmt.Errorf("expiration check timeout: %w", ctx.Err())
		default:
		}

		// Calculate days remaining (floor to whole days)
		daysRemaining := int(math.Floor(time.Until(*d.ExpirationDate).Hours() / 24))

		// Check if days remaining matches any configured threshold
		if !e.isThresholdHit(daysRemaining, now, *d.ExpirationDate) {
			continue
		}

		// Calculate severity
		severity := CalculateSeverity(daysRemaining, d.AutoRenew)

		// Check if alert already exists for this domain + threshold (avoid duplicates)
		if e.alertExists(ctx, d.ID, daysRemaining) {
			continue
		}

		// Create alert record
		alert := domain.Alert{
			DomainID:       d.ID,
			AlertType:      "expiration",
			Severity:       severity,
			Message:        e.buildAlertMessage(d, daysRemaining, severity),
			DaysRemaining:  &daysRemaining,
			DeliveryStatus: "pending",
			GeneratedAt:    now,
		}

		if err := e.db.WithContext(ctx).Create(&alert).Error; err != nil {
			e.logger.Error("failed to create alert",
				zap.String("domain", d.DomainName),
				zap.Error(err))
			continue
		}

		alertsCreated++
		e.logger.Info("alert created",
			zap.String("domain", d.DomainName),
			zap.Int("days_remaining", daysRemaining),
			zap.String("severity", severity))
	}

	e.logger.Info("expiration check cycle completed",
		zap.Int("alerts_created", alertsCreated),
		zap.Int("domains_evaluated", len(domains)))

	return nil
}

// Start launches a background goroutine that runs the expiration check every 24 hours.
func (e *AlertEngine) Start(ctx context.Context) {
	go func() {
		// Run immediately on start
		if err := e.RunExpirationCheck(ctx); err != nil {
			e.logger.Error("initial expiration check failed", zap.Error(err))
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				e.logger.Info("alert engine stopped")
				return
			case <-ticker.C:
				if err := e.RunExpirationCheck(ctx); err != nil {
					e.logger.Error("expiration check failed", zap.Error(err))
				}
			}
		}
	}()
}

// isThresholdHit checks if the days remaining for a domain matches any configured threshold.
// A threshold is considered "hit" if the days remaining is less than or equal to the threshold
// AND greater than the next smaller threshold (or the domain is past due).
func (e *AlertEngine) isThresholdHit(daysRemaining int, now time.Time, expirationDate time.Time) bool {
	for _, threshold := range e.thresholds {
		if daysRemaining == threshold {
			return true
		}
	}
	// Also trigger for expired domains (past due)
	if daysRemaining < 0 {
		// Only trigger once at day -1 (first day past expiration)
		return daysRemaining == -1
	}
	return false
}

// alertExists checks if an alert has already been generated for a specific domain
// and threshold (days remaining) to avoid duplicates.
func (e *AlertEngine) alertExists(ctx context.Context, domainID uint, daysRemaining int) bool {
	var count int64
	e.db.WithContext(ctx).
		Model(&domain.Alert{}).
		Where("domain_id = ? AND alert_type = ? AND days_remaining = ?",
			domainID, "expiration", daysRemaining).
		Count(&count)
	return count > 0
}

// buildAlertMessage constructs a human-readable alert message.
func (e *AlertEngine) buildAlertMessage(d domain.NormalizedDomain, daysRemaining int, severity string) string {
	if daysRemaining < 0 {
		return fmt.Sprintf("Domain %s (registrar: %s) has expired. Expiration date: %s.",
			d.DomainName, d.RegistrarIdentifier, d.ExpirationDate.Format("2006-01-02"))
	}
	return fmt.Sprintf("Domain %s (registrar: %s) expires in %d days on %s. Severity: %s.",
		d.DomainName, d.RegistrarIdentifier, daysRemaining,
		d.ExpirationDate.Format("2006-01-02"), severity)
}
