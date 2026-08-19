package monitor

import (
	"context"
	"math"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// Health score weights.
	WeightExpiration  = 30
	WeightCertificate = 25
	WeightUptime      = 25
	WeightEmail       = 20
)

// HealthScoreCalculator computes and updates domain health scores.
type HealthScoreCalculator struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewHealthScoreCalculator creates a new HealthScoreCalculator.
func NewHealthScoreCalculator(db *gorm.DB, logger *zap.Logger) *HealthScoreCalculator {
	return &HealthScoreCalculator{
		db:     db,
		logger: logger,
	}
}

// Start launches a background goroutine for periodic health score calculation.
func (h *HealthScoreCalculator) Start(ctx context.Context) {
	go h.run(ctx)
	h.logger.Info("Health score calculator started")
}

// run is the main loop for the health score calculator.
func (h *HealthScoreCalculator) run(ctx context.Context) {
	// Run immediately on start.
	h.runCycle(ctx)

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("Health score calculator stopped")
			return
		case <-ticker.C:
			h.runCycle(ctx)
		}
	}
}

// runCycle recalculates health scores for all domains.
func (h *HealthScoreCalculator) runCycle(ctx context.Context) {
	cycleCtx, cancel := context.WithTimeout(ctx, MonitorCycleDuration)
	defer cancel()

	var domains []domain.NormalizedDomain
	if err := h.db.WithContext(cycleCtx).Find(&domains).Error; err != nil {
		h.logger.Error("failed to load domains for health score calculation", zap.Error(err))
		return
	}

	for _, d := range domains {
		select {
		case <-cycleCtx.Done():
			return
		default:
		}

		score := h.calculateForDomain(cycleCtx, d)
		if score != d.HealthScore {
			h.db.WithContext(cycleCtx).Model(&d).Update("health_score", score)
		}
	}
}

// calculateForDomain computes the health score for a single domain.
func (h *HealthScoreCalculator) calculateForDomain(ctx context.Context, d domain.NormalizedDomain) int {
	expirationScore := h.getExpirationScore(d)
	certScore := h.getCertificateScore(ctx, d.ID)
	uptimeScore := h.getUptimeScore(ctx, d)
	emailScore := h.getEmailScore(ctx, d.ID)

	return CalculateHealthScore(expirationScore, certScore, uptimeScore, emailScore)
}

// CalculateHealthScore computes the weighted health score from component scores.
// Each component is 0-100, weighted: expiration(30) + certificate(25) + uptime(25) + email(20).
// Result is clamped to [0, 100].
func CalculateHealthScore(expirationScore, certificateScore, uptimeScore, emailScore float64) int {
	weighted := expirationScore*float64(WeightExpiration)/100.0 +
		certificateScore*float64(WeightCertificate)/100.0 +
		uptimeScore*float64(WeightUptime)/100.0 +
		emailScore*float64(WeightEmail)/100.0

	score := int(math.Round(weighted))
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// getExpirationScore returns a 0-100 score based on expiration proximity.
func (h *HealthScoreCalculator) getExpirationScore(d domain.NormalizedDomain) float64 {
	if d.ExpirationDate == nil {
		return 100 // No expiration date — assume healthy.
	}

	daysRemaining := time.Until(*d.ExpirationDate).Hours() / 24
	return ExpirationProximityScore(daysRemaining)
}

// ExpirationProximityScore returns a 0-100 score based on days until expiration.
// Exported for testing.
func ExpirationProximityScore(daysRemaining float64) float64 {
	switch {
	case daysRemaining <= 0:
		return 0
	case daysRemaining <= 7:
		return 20
	case daysRemaining <= 14:
		return 40
	case daysRemaining <= 30:
		return 60
	case daysRemaining <= 90:
		return 80
	default:
		return 100
	}
}

// getCertificateScore returns a 0-100 score based on latest certificate check.
func (h *HealthScoreCalculator) getCertificateScore(ctx context.Context, domainID uint) float64 {
	var check domain.CertificateCheck
	err := h.db.WithContext(ctx).
		Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		First(&check).Error
	if err != nil {
		return 100 // No certificate check yet — assume healthy.
	}

	return CertificateValidityScore(check.DaysRemaining, check.ChainComplete)
}

// CertificateValidityScore returns a 0-100 score based on certificate status.
// Exported for testing.
func CertificateValidityScore(daysRemaining int, chainComplete bool) float64 {
	var score float64

	switch {
	case daysRemaining <= 0:
		score = 0
	case daysRemaining <= 7:
		score = 30
	case daysRemaining <= 14:
		score = 50
	case daysRemaining <= 30:
		score = 70
	default:
		score = 100
	}

	if !chainComplete && score > 0 {
		score -= 20
	}

	if score < 0 {
		return 0
	}
	return score
}

// getUptimeScore returns a 0-100 score based on recent uptime.
func (h *HealthScoreCalculator) getUptimeScore(ctx context.Context, d domain.NormalizedDomain) float64 {
	if d.WebsiteURL == "" {
		return 100 // No website URL — not monitored, assume healthy.
	}

	// Get checks from last 24 hours.
	since := time.Now().Add(-24 * time.Hour)
	var checks []domain.HealthCheck
	h.db.WithContext(ctx).
		Where("domain_id = ? AND checked_at >= ?", d.ID, since).
		Find(&checks)

	if len(checks) == 0 {
		return 100 // No checks yet.
	}

	threshold := d.ResponseTimeThresholdMs
	if threshold <= 0 {
		threshold = DefaultResponseTimeThresholdMs
	}

	successful := 0
	for _, check := range checks {
		if check.FailureCategory == "" && check.HTTPStatusCode >= 200 && check.HTTPStatusCode < 300 && check.ResponseTimeMs <= threshold {
			successful++
		}
	}

	return CalculateUptime(len(checks), successful)
}

// getEmailScore returns a 0-100 score based on latest email compliance check.
func (h *HealthScoreCalculator) getEmailScore(ctx context.Context, domainID uint) float64 {
	var check domain.EmailCheck
	err := h.db.WithContext(ctx).
		Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		First(&check).Error
	if err != nil {
		return 100 // No email check yet — assume healthy.
	}

	return float64(check.ComplianceScore)
}
