package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// EmailCheckInterval is the minimum time between email service checks.
	EmailCheckInterval = 24 * time.Hour

	// MX port check timeout.
	MXPortTimeout = 10 * time.Second

	// MX port check retries.
	MXPortRetries = 2
)

// EmailMonitor performs periodic email service compliance checks on domains.
type EmailMonitor struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewEmailMonitor creates a new EmailMonitor instance.
func NewEmailMonitor(db *gorm.DB, logger *zap.Logger) *EmailMonitor {
	return &EmailMonitor{
		db:     db,
		logger: logger,
	}
}

// Start launches a background goroutine for email monitoring.
func (m *EmailMonitor) Start(ctx context.Context) {
	go m.run(ctx)
	m.logger.Info("Email monitor started")
}

// run is the main loop for the email monitor.
func (m *EmailMonitor) run(ctx context.Context) {
	// Run immediately on start.
	m.runCycle(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Email monitor stopped")
			return
		case <-ticker.C:
			m.runCycle(ctx)
		}
	}
}

// runCycle checks all email-enabled domains for email service compliance.
func (m *EmailMonitor) runCycle(ctx context.Context) {
	cycleCtx, cancel := context.WithTimeout(ctx, MonitorCycleDuration)
	defer cancel()

	var domains []domain.NormalizedDomain
	if err := m.db.WithContext(cycleCtx).
		Where("email_enabled = ?", true).
		Find(&domains).Error; err != nil {
		m.logger.Error("failed to load domains for email monitoring", zap.Error(err))
		return
	}

	now := time.Now()
	for _, d := range domains {
		select {
		case <-cycleCtx.Done():
			m.logger.Warn("email monitoring cycle timeout")
			return
		default:
		}

		if !m.isEmailCheckDue(cycleCtx, d, now) {
			continue
		}

		m.checkEmail(cycleCtx, d, now)
	}
}

// isEmailCheckDue determines if a domain is due for email checking (once per day).
func (m *EmailMonitor) isEmailCheckDue(ctx context.Context, d domain.NormalizedDomain, now time.Time) bool {
	var lastCheck domain.EmailCheck
	err := m.db.WithContext(ctx).
		Where("domain_id = ?", d.ID).
		Order("checked_at DESC").
		First(&lastCheck).Error
	if err != nil {
		return true
	}
	return now.Sub(lastCheck.CheckedAt) >= EmailCheckInterval
}

// EmailCheckResult holds the results of an email service compliance check.
type EmailCheckResult struct {
	MXRecords       []string
	SPFValid        bool
	DKIMValid       bool
	DMARCValid      bool
	ComplianceScore int
	MXChanged       bool
	MXPortReachable bool
}

// checkEmail performs email service compliance checks for a domain.
func (m *EmailMonitor) checkEmail(ctx context.Context, d domain.NormalizedDomain, now time.Time) {
	domainName := d.DomainName

	// Query MX records.
	mxRecords := queryMXRecords(domainName)

	// Check SPF, DKIM, DMARC records.
	spfValid := checkSPF(domainName)
	dkimValid := checkDKIM(domainName)
	dmarcValid := checkDMARC(domainName)

	// Calculate compliance score.
	complianceScore := CalculateEmailComplianceScore(len(mxRecords) > 0, spfValid, dkimValid, dmarcValid)

	// Check MX change from previous.
	mxJSON, _ := json.Marshal(mxRecords)
	mxChanged := m.detectMXChange(ctx, d.ID, string(mxJSON))

	// Get previous MX records for storage.
	var previousMX string
	var lastCheck domain.EmailCheck
	if err := m.db.WithContext(ctx).
		Where("domain_id = ?", d.ID).
		Order("checked_at DESC").
		First(&lastCheck).Error; err == nil {
		previousMX = lastCheck.MXRecords
	}

	// Save email check result.
	emailCheck := domain.EmailCheck{
		DomainID:        d.ID,
		MXRecords:       string(mxJSON),
		SPFValid:        spfValid,
		DKIMValid:       dkimValid,
		DMARCValid:      dmarcValid,
		ComplianceScore: complianceScore,
		MXPrevious:      previousMX,
		MXChanged:       mxChanged,
		CheckedAt:       now,
	}

	if err := m.db.WithContext(ctx).Create(&emailCheck).Error; err != nil {
		m.logger.Error("failed to save email check",
			zap.String("domain", domainName),
			zap.Error(err))
		return
	}

	// Generate alerts for compliance issues.
	m.evaluateEmailCompliance(ctx, d, mxRecords, spfValid, dkimValid, dmarcValid, mxChanged, now)
}

// queryMXRecords looks up MX records for a domain.
func queryMXRecords(domainName string) []string {
	mxRecords, err := net.LookupMX(domainName)
	if err != nil {
		return nil
	}

	var records []string
	for _, mx := range mxRecords {
		records = append(records, fmt.Sprintf("%s:%d", mx.Host, mx.Pref))
	}
	return records
}

// checkSPF validates SPF record presence and basic syntax.
func checkSPF(domainName string) bool {
	records, err := net.LookupTXT(domainName)
	if err != nil {
		return false
	}

	for _, record := range records {
		if strings.HasPrefix(strings.ToLower(record), "v=spf1") {
			return true
		}
	}
	return false
}

// checkDKIM checks for DKIM record presence.
// Checks the common default selector "default._domainkey".
func checkDKIM(domainName string) bool {
	selectors := []string{"default", "google", "selector1", "selector2", "k1", "dkim"}
	for _, sel := range selectors {
		records, err := net.LookupTXT(sel + "._domainkey." + domainName)
		if err != nil {
			continue
		}
		for _, record := range records {
			if strings.Contains(strings.ToLower(record), "v=dkim1") ||
				strings.Contains(strings.ToLower(record), "p=") {
				return true
			}
		}
	}
	return false
}

// checkDMARC validates DMARC record presence and basic syntax.
func checkDMARC(domainName string) bool {
	records, err := net.LookupTXT("_dmarc." + domainName)
	if err != nil {
		return false
	}

	for _, record := range records {
		if strings.HasPrefix(strings.ToLower(record), "v=dmarc1") {
			return true
		}
	}
	return false
}

// CalculateEmailComplianceScore calculates the email compliance score (0-100).
// Equal weighting: MX(25) + SPF(25) + DKIM(25) + DMARC(25).
func CalculateEmailComplianceScore(hasMX, spfValid, dkimValid, dmarcValid bool) int {
	score := 0
	if hasMX {
		score += 25
	}
	if spfValid {
		score += 25
	}
	if dkimValid {
		score += 25
	}
	if dmarcValid {
		score += 25
	}
	return score
}

// detectMXChange checks if MX records have changed from the previous check.
func (m *EmailMonitor) detectMXChange(ctx context.Context, domainID uint, currentMX string) bool {
	var lastCheck domain.EmailCheck
	err := m.db.WithContext(ctx).
		Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		First(&lastCheck).Error
	if err != nil {
		// No previous check — no change.
		return false
	}

	return lastCheck.MXRecords != currentMX
}

// evaluateEmailCompliance generates alerts for email compliance issues.
func (m *EmailMonitor) evaluateEmailCompliance(ctx context.Context, d domain.NormalizedDomain, mxRecords []string, spfValid, dkimValid, dmarcValid, mxChanged bool, now time.Time) {
	// Generate compliance warnings for missing records.
	var missing []string
	if !spfValid {
		missing = append(missing, "SPF")
	}
	if !dkimValid {
		missing = append(missing, "DKIM")
	}
	if !dmarcValid {
		missing = append(missing, "DMARC")
	}

	if len(missing) > 0 {
		m.createEmailComplianceAlert(ctx, d, missing, now)
	}

	// Generate MX change alert.
	if mxChanged {
		m.createMXChangeAlert(ctx, d, now)
	}

	// Check MX host reachability on port 25.
	if len(mxRecords) > 0 {
		m.checkMXReachability(ctx, d, mxRecords, now)
	}
}

// createEmailComplianceAlert creates a warning alert for missing email security records.
func (m *EmailMonitor) createEmailComplianceAlert(ctx context.Context, d domain.NormalizedDomain, missing []string, now time.Time) {
	message := fmt.Sprintf("Domain %s is missing email security records: %s", d.DomainName, strings.Join(missing, ", "))

	// Check for existing unacknowledged compliance alert.
	var count int64
	m.db.WithContext(ctx).
		Model(&domain.Alert{}).
		Where("domain_id = ? AND alert_type = ? AND severity = ? AND acknowledged = ?",
			d.ID, "email", "warning", false).
		Count(&count)
	if count > 0 {
		return
	}

	alert := domain.Alert{
		DomainID:       d.ID,
		AlertType:      "email",
		Severity:       "warning",
		Message:        message,
		DeliveryStatus: "pending",
		GeneratedAt:    now,
	}

	if err := m.db.WithContext(ctx).Create(&alert).Error; err != nil {
		m.logger.Error("failed to create email compliance alert",
			zap.String("domain", d.DomainName),
			zap.Error(err))
	}
}

// createMXChangeAlert creates a warning alert when MX records change.
func (m *EmailMonitor) createMXChangeAlert(ctx context.Context, d domain.NormalizedDomain, now time.Time) {
	alert := domain.Alert{
		DomainID:       d.ID,
		AlertType:      "email",
		Severity:       "warning",
		Message:        fmt.Sprintf("MX records for %s have changed since the last check", d.DomainName),
		DeliveryStatus: "pending",
		GeneratedAt:    now,
	}

	if err := m.db.WithContext(ctx).Create(&alert).Error; err != nil {
		m.logger.Error("failed to create MX change alert",
			zap.String("domain", d.DomainName),
			zap.Error(err))
	}
}

// checkMXReachability checks if MX hosts respond on port 25.
func (m *EmailMonitor) checkMXReachability(ctx context.Context, d domain.NormalizedDomain, mxRecords []string, now time.Time) {
	for _, record := range mxRecords {
		// Parse host from "host:priority" format.
		parts := strings.SplitN(record, ":", 2)
		host := parts[0]
		// Remove trailing dot from MX hostname.
		host = strings.TrimSuffix(host, ".")

		reachable := checkPort25(host)
		if !reachable {
			// Create critical alert for unreachable MX host.
			var count int64
			m.db.WithContext(ctx).
				Model(&domain.Alert{}).
				Where("domain_id = ? AND alert_type = ? AND severity = ? AND acknowledged = ? AND message LIKE ?",
					d.ID, "email", "critical", false, "%"+host+"%").
				Count(&count)
			if count > 0 {
				continue
			}

			alert := domain.Alert{
				DomainID:       d.ID,
				AlertType:      "email",
				Severity:       "critical",
				Message:        fmt.Sprintf("MX host %s for domain %s is not responding on port 25", host, d.DomainName),
				DeliveryStatus: "pending",
				GeneratedAt:    now,
			}

			if err := m.db.WithContext(ctx).Create(&alert).Error; err != nil {
				m.logger.Error("failed to create MX unreachable alert",
					zap.String("domain", d.DomainName),
					zap.String("mx_host", host),
					zap.Error(err))
			}
		}
	}
}

// checkPort25 checks if a host responds on TCP port 25 with retries.
func checkPort25(host string) bool {
	for i := 0; i <= MXPortRetries; i++ {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "25"), MXPortTimeout)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}

// DetectMXChange returns true if two MX record sets are different.
// Exported for testing.
func DetectMXChange(currentMX, previousMX string) bool {
	return currentMX != previousMX
}
