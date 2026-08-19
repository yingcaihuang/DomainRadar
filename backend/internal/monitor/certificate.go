package monitor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// CertCheckInterval is the minimum time between certificate checks.
	CertCheckInterval = 24 * time.Hour

	// CertCriticalAlertDelay is the maximum delay for critical certificate alerts.
	CertCriticalAlertDelay = 5 * time.Minute

	// TLS connection timeout.
	TLSDialTimeout = 10 * time.Second
)

// Certificate alert thresholds (days before expiration).
var CertAlertThresholds = []int{30, 14, 7, 3, 1}

// CertificateMonitor performs periodic SSL/TLS certificate checks on domains.
type CertificateMonitor struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewCertificateMonitor creates a new CertificateMonitor instance.
func NewCertificateMonitor(db *gorm.DB, logger *zap.Logger) *CertificateMonitor {
	return &CertificateMonitor{
		db:     db,
		logger: logger,
	}
}

// Start launches a background goroutine for certificate monitoring.
func (m *CertificateMonitor) Start(ctx context.Context) {
	go m.run(ctx)
	m.logger.Info("Certificate monitor started")
}

// run is the main loop for the certificate monitor.
func (m *CertificateMonitor) run(ctx context.Context) {
	// Run immediately on start.
	m.runCycle(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Certificate monitor stopped")
			return
		case <-ticker.C:
			m.runCycle(ctx)
		}
	}
}

// runCycle checks all domains with website URLs for certificate status.
func (m *CertificateMonitor) runCycle(ctx context.Context) {
	cycleCtx, cancel := context.WithTimeout(ctx, MonitorCycleDuration)
	defer cancel()

	var domains []domain.NormalizedDomain
	if err := m.db.WithContext(cycleCtx).
		Where("website_url != '' AND website_url IS NOT NULL").
		Find(&domains).Error; err != nil {
		m.logger.Error("failed to load domains for certificate monitoring", zap.Error(err))
		return
	}

	now := time.Now()
	for _, d := range domains {
		select {
		case <-cycleCtx.Done():
			m.logger.Warn("certificate monitoring cycle timeout")
			return
		default:
		}

		if !m.isCertCheckDue(cycleCtx, d, now) {
			continue
		}

		m.checkCertificate(cycleCtx, d, now)
	}
}

// isCertCheckDue determines if a domain is due for certificate checking (once per day).
func (m *CertificateMonitor) isCertCheckDue(ctx context.Context, d domain.NormalizedDomain, now time.Time) bool {
	var lastCheck domain.CertificateCheck
	err := m.db.WithContext(ctx).
		Where("domain_id = ?", d.ID).
		Order("checked_at DESC").
		First(&lastCheck).Error
	if err != nil {
		return true
	}
	return now.Sub(lastCheck.CheckedAt) >= CertCheckInterval
}

// CertCheckResult holds the results of a TLS certificate inspection.
type CertCheckResult struct {
	Issuer        string
	Subject       string
	ValidFrom     time.Time
	ValidTo       time.Time
	ChainComplete bool
	SerialNumber  string
	DaysRemaining int
	Error         error

	// Critical issues.
	HostnameMismatch bool
	InvalidChain     bool
	IsRevoked        bool
}

// checkCertificate performs a TLS connection to retrieve and inspect the certificate.
func (m *CertificateMonitor) checkCertificate(ctx context.Context, d domain.NormalizedDomain, now time.Time) {
	host := extractHost(d.WebsiteURL)
	if host == "" {
		m.logger.Warn("cannot determine host from website URL",
			zap.String("domain", d.DomainName),
			zap.String("url", d.WebsiteURL))
		return
	}

	result := m.performCertCheck(host, d.DomainName)

	if result.Error != nil {
		// Log connection failure, retry next scheduled check without generating cert-specific alert.
		m.logger.Warn("certificate check connection failed",
			zap.String("domain", d.DomainName),
			zap.Error(result.Error))
		return
	}

	// Save the certificate check result.
	certCheck := domain.CertificateCheck{
		DomainID:      d.ID,
		Issuer:        result.Issuer,
		Subject:       result.Subject,
		ValidFrom:     result.ValidFrom,
		ValidTo:       result.ValidTo,
		ChainComplete: result.ChainComplete,
		SerialNumber:  result.SerialNumber,
		DaysRemaining: result.DaysRemaining,
		CheckedAt:     now,
	}

	if err := m.db.WithContext(ctx).Create(&certCheck).Error; err != nil {
		m.logger.Error("failed to save certificate check",
			zap.String("domain", d.DomainName),
			zap.Error(err))
		return
	}

	// Check for critical issues (invalid chain, hostname mismatch, revocation).
	if result.InvalidChain || result.HostnameMismatch || result.IsRevoked {
		m.generateCriticalCertAlert(ctx, d, result, now)
		return
	}

	// Check for renewal detection.
	m.detectRenewal(ctx, d, certCheck)

	// Check for expiration alerts.
	m.evaluateCertExpiration(ctx, d, result, now)
}

// performCertCheck connects to the host via TLS and inspects the certificate.
func (m *CertificateMonitor) performCertCheck(host, domainName string) CertCheckResult {
	result := CertCheckResult{}

	addr := net.JoinHostPort(host, "443")
	dialer := &net.Dialer{Timeout: TLSDialTimeout}

	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		InsecureSkipVerify: true, // We verify manually to report specific issues.
	})
	if err != nil {
		result.Error = fmt.Errorf("TLS dial failed: %w", err)
		return result
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		result.Error = fmt.Errorf("no certificates presented by %s", host)
		return result
	}

	leaf := certs[0]
	result.Issuer = leaf.Issuer.CommonName
	result.Subject = leaf.Subject.CommonName
	result.ValidFrom = leaf.NotBefore
	result.ValidTo = leaf.NotAfter
	result.SerialNumber = leaf.SerialNumber.String()
	result.DaysRemaining = int(math.Floor(time.Until(leaf.NotAfter).Hours() / 24))

	// Verify hostname.
	if err := leaf.VerifyHostname(domainName); err != nil {
		// Also try the host directly if different from domain name.
		if err2 := leaf.VerifyHostname(host); err2 != nil {
			result.HostnameMismatch = true
		}
	}

	// Verify certificate chain.
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}

	opts := x509.VerifyOptions{
		DNSName:       domainName,
		Intermediates: intermediates,
	}

	if _, err := leaf.Verify(opts); err != nil {
		// Distinguish between chain issues and hostname issues.
		optsNoHost := x509.VerifyOptions{
			Intermediates: intermediates,
		}
		if _, err2 := leaf.Verify(optsNoHost); err2 != nil {
			result.InvalidChain = true
		}
		result.ChainComplete = false
	} else {
		result.ChainComplete = true
	}

	return result
}

// generateCriticalCertAlert creates a critical alert for certificate issues.
func (m *CertificateMonitor) generateCriticalCertAlert(ctx context.Context, d domain.NormalizedDomain, result CertCheckResult, now time.Time) {
	// Don't duplicate if a cert critical alert already exists and is not acknowledged.
	var existingAlert domain.Alert
	err := m.db.WithContext(ctx).
		Where("domain_id = ? AND alert_type = ? AND severity = ? AND acknowledged = ?",
			d.ID, "certificate", "critical", false).
		First(&existingAlert).Error
	if err == nil {
		return
	}

	var message string
	switch {
	case result.HostnameMismatch:
		message = fmt.Sprintf("Certificate for %s has a hostname mismatch (subject: %s)", d.DomainName, result.Subject)
	case result.InvalidChain:
		message = fmt.Sprintf("Certificate for %s has an invalid chain (issuer: %s)", d.DomainName, result.Issuer)
	case result.IsRevoked:
		message = fmt.Sprintf("Certificate for %s has been revoked", d.DomainName)
	}

	alert := domain.Alert{
		DomainID:       d.ID,
		AlertType:      "certificate",
		Severity:       "critical",
		Message:        message,
		DeliveryStatus: "pending",
		GeneratedAt:    now,
	}

	if err := m.db.WithContext(ctx).Create(&alert).Error; err != nil {
		m.logger.Error("failed to create critical certificate alert",
			zap.String("domain", d.DomainName),
			zap.Error(err))
		return
	}

	m.logger.Warn("critical certificate alert created",
		zap.String("domain", d.DomainName),
		zap.Bool("hostname_mismatch", result.HostnameMismatch),
		zap.Bool("invalid_chain", result.InvalidChain),
		zap.Bool("revoked", result.IsRevoked))
}

// detectRenewal checks if the certificate was renewed compared to the previous check.
func (m *CertificateMonitor) detectRenewal(ctx context.Context, d domain.NormalizedDomain, current domain.CertificateCheck) {
	var previous domain.CertificateCheck
	err := m.db.WithContext(ctx).
		Where("domain_id = ? AND id != ?", d.ID, current.ID).
		Order("checked_at DESC").
		First(&previous).Error
	if err != nil {
		// No previous check to compare against.
		return
	}

	// Renewal detected if valid_to date OR serial number changed.
	renewed := !current.ValidTo.Equal(previous.ValidTo) || current.SerialNumber != previous.SerialNumber
	if !renewed {
		return
	}

	m.logger.Info("certificate renewal detected",
		zap.String("domain", d.DomainName),
		zap.String("old_serial", previous.SerialNumber),
		zap.String("new_serial", current.SerialNumber),
		zap.Time("old_valid_to", previous.ValidTo),
		zap.Time("new_valid_to", current.ValidTo))

	// Clear any active certificate alerts for this domain.
	m.db.WithContext(ctx).
		Model(&domain.Alert{}).
		Where("domain_id = ? AND alert_type = ? AND acknowledged = ?", d.ID, "certificate", false).
		Updates(map[string]interface{}{
			"acknowledged":    true,
			"acknowledged_at": time.Now(),
		})
}

// evaluateCertExpiration generates certificate expiration alerts at configured thresholds.
func (m *CertificateMonitor) evaluateCertExpiration(ctx context.Context, d domain.NormalizedDomain, result CertCheckResult, now time.Time) {
	for _, threshold := range CertAlertThresholds {
		if result.DaysRemaining != threshold {
			continue
		}

		// Check if alert already exists for this threshold.
		var count int64
		m.db.WithContext(ctx).
			Model(&domain.Alert{}).
			Where("domain_id = ? AND alert_type = ? AND days_remaining = ?",
				d.ID, "certificate", threshold).
			Count(&count)
		if count > 0 {
			continue
		}

		severity := CalculateCertSeverity(result.DaysRemaining)
		daysRemaining := result.DaysRemaining

		alert := domain.Alert{
			DomainID:       d.ID,
			AlertType:      "certificate",
			Severity:       severity,
			Message:        fmt.Sprintf("SSL certificate for %s expires in %d days (issuer: %s)", d.DomainName, daysRemaining, result.Issuer),
			DaysRemaining:  &daysRemaining,
			DeliveryStatus: "pending",
			GeneratedAt:    now,
		}

		if err := m.db.WithContext(ctx).Create(&alert).Error; err != nil {
			m.logger.Error("failed to create certificate expiration alert",
				zap.String("domain", d.DomainName),
				zap.Error(err))
		}
	}
}

// CalculateCertSeverity determines severity for certificate expiration alerts
// using the same tiering as domain expiration.
func CalculateCertSeverity(daysRemaining int) string {
	switch {
	case daysRemaining < 0:
		return "expired"
	case daysRemaining <= 7:
		return "critical"
	case daysRemaining <= 30:
		return "warning"
	default:
		return "informational"
	}
}

// DetectCertRenewal returns true if the certificate was renewed (valid_to or serial changed).
func DetectCertRenewal(current, previous domain.CertificateCheck) bool {
	return !current.ValidTo.Equal(previous.ValidTo) || current.SerialNumber != previous.SerialNumber
}

// extractHost extracts the hostname from a URL string.
func extractHost(rawURL string) string {
	// Simple extraction — strip scheme and path.
	host := rawURL
	if idx := indexOf(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	if idx := indexOf(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	if idx := indexOf(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return host
}

func indexOf(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
