package certcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CertScheduler periodically checks all enabled certificate monitors.
// AlertDispatchFunc is a callback for webhook notification dispatch.
type AlertDispatchFunc func(alert *domain.Alert)

type CertScheduler struct {
	db             *gorm.DB
	logger         *zap.Logger
	interval       time.Duration
	OnAlertCreated AlertDispatchFunc
}

// NewCertScheduler creates a new CertScheduler.
// interval specifies how often checks run (default 6 hours if 0).
func NewCertScheduler(db *gorm.DB, logger *zap.Logger, interval time.Duration) *CertScheduler {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &CertScheduler{db: db, logger: logger, interval: interval}
}

// Start launches the certificate scheduler as a background goroutine.
func (s *CertScheduler) Start(ctx context.Context) {
	// Run immediately on startup
	if err := s.RunAllChecks(ctx); err != nil {
		s.logger.Error("Initial certificate check failed", zap.Error(err))
	}

	// Then run periodically
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RunAllChecks(context.Background()); err != nil {
					s.logger.Error("Periodic certificate check failed", zap.Error(err))
				}
			}
		}
	}()

	s.logger.Info("Certificate scheduler started", zap.Duration("interval", s.interval))
}

// RunAllChecks iterates all enabled certificate monitors, checks each, and stores results.
func (s *CertScheduler) RunAllChecks(ctx context.Context) error {
	var monitors []domain.CertificateMonitor
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&monitors).Error; err != nil {
		return fmt.Errorf("failed to query monitors: %w", err)
	}

	now := time.Now()
	checked := 0
	alertsCreated := 0

	for _, m := range monitors {
		result, err := CheckEndpoint(m.Endpoint, 15*time.Second)
		if err != nil {
			result = &CertResult{Error: err.Error()}
		}

		// Store the check result
		sansJSON, _ := json.Marshal(result.SANs)
		chainJSON, _ := json.Marshal(result.Chain)
		check := domain.CertificateCheck{
			DomainID:      m.DomainID,
			MonitorID:     m.ID,
			Subject:       result.Subject,
			Issuer:        result.Issuer,
			ValidFrom:     result.ValidFrom,
			ValidTo:       result.ValidTo,
			DaysRemaining: result.DaysRemaining,
			ChainComplete: result.ChainComplete,
			SANs:          string(sansJSON),
			Chain:         string(chainJSON),
			SerialNumber:  result.SerialNumber,
			ConnectedIP:   result.ConnectedIP,
			SNI:           result.SNI,
			DNSResolveMs:  result.DNSResolveMs,
			HandshakeMs:   result.HandshakeMs,
			TotalMs:       result.TotalMs,
			TLSVersion:    result.TLSVersion,
			CipherSuite:   result.CipherSuite,
			Error:         result.Error,
			CheckedAt:     now,
		}

		// Keep only last 10 records per monitor
		var oldChecks []domain.CertificateCheck
		s.db.WithContext(ctx).Where("monitor_id = ?", m.ID).Order("checked_at DESC").Offset(10).Find(&oldChecks)
		for _, old := range oldChecks {
			s.db.WithContext(ctx).Delete(&old)
		}

		// Update monitor timestamps
		nextCheck := now.Add(s.interval)
		s.db.WithContext(ctx).Model(&m).Updates(map[string]interface{}{
			"last_checked_at": now,
			"next_check_at":   nextCheck,
		})

		if err := s.db.WithContext(ctx).Create(&check).Error; err != nil {
			s.logger.Error("failed to save cert check", zap.String("endpoint", m.Endpoint), zap.Error(err))
			continue
		}
		checked++

		// Generate alert if certificate is expiring or has errors
		if created := s.maybeCreateAlert(ctx, m, result, now); created {
			alertsCreated++
		}
	}

	s.logger.Info("Certificate check completed",
		zap.Int("monitors_checked", checked),
		zap.Int("alerts_created", alertsCreated),
	)
	return nil
}

// maybeCreateAlert creates an alert if the certificate is expiring soon or has errors.
func (s *CertScheduler) maybeCreateAlert(ctx context.Context, monitor domain.CertificateMonitor, result *CertResult, now time.Time) bool {
	var alertType string
	var severity string
	var message string

	switch {
	case result.Error != "":
		alertType = "certificate_error"
		severity = "warning"
		message = fmt.Sprintf("证书检测失败 (%s): %s", monitor.Endpoint, result.Error)
	case result.DaysRemaining < 0:
		alertType = "certificate_expired"
		severity = "critical"
		message = fmt.Sprintf("SSL证书已过期 (%s), 过期 %d 天", monitor.Endpoint, -result.DaysRemaining)
	case result.DaysRemaining <= 7:
		alertType = "certificate_expiring"
		severity = "critical"
		message = fmt.Sprintf("SSL证书将在 %d 天后过期 (%s)", result.DaysRemaining, monitor.Endpoint)
	case result.DaysRemaining <= 30:
		alertType = "certificate_expiring"
		severity = "warning"
		message = fmt.Sprintf("SSL证书将在 %d 天后过期 (%s)", result.DaysRemaining, monitor.Endpoint)
	default:
		return false // No alert needed
	}

	// Check for existing unacknowledged alert of same type for this domain
	var existing domain.Alert
	err := s.db.WithContext(ctx).Where(
		"domain_id = ? AND alert_type = ? AND acknowledged = ?",
		monitor.DomainID, alertType, false,
	).First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		daysRemaining := result.DaysRemaining
		alert := domain.Alert{
			DomainID:      monitor.DomainID,
			AlertType:     alertType,
			Severity:      severity,
			Message:       message,
			DaysRemaining: &daysRemaining,
			Acknowledged:  false,
			GeneratedAt:   now,
		}
		if err := s.db.WithContext(ctx).Create(&alert).Error; err != nil {
			s.logger.Error("failed to create certificate alert",
				zap.String("endpoint", monitor.Endpoint),
				zap.Error(err),
			)
			return false
		}
		if s.OnAlertCreated != nil {
			s.OnAlertCreated(&alert)
		}
		return true
	}

	return false
}
