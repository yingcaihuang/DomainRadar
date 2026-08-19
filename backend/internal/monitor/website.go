package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// Default check interval for website health checks.
	DefaultCheckIntervalMinutes = 5

	// Default response time threshold in milliseconds.
	DefaultResponseTimeThresholdMs = 10000

	// Connection timeout for health checks.
	ConnectionTimeout = 30 * time.Second

	// Maximum number of redirects to follow.
	MaxRedirects = 10

	// Consecutive failures required to trigger a downtime alert.
	ConsecutiveFailuresForAlert = 3

	// Maximum duration for a single monitoring cycle.
	MonitorCycleDuration = 10 * time.Minute
)

// FailureCategory constants for categorizing health check failures.
const (
	FailureCategoryDNS          = "dns"
	FailureCategoryConnectivity = "connectivity"
	FailureCategoryHTTPError    = "http_error"
)

// WebsiteMonitor performs periodic HTTP health checks on domains with associated website URLs.
type WebsiteMonitor struct {
	db     *gorm.DB
	logger *zap.Logger
	client *http.Client
}

// NewWebsiteMonitor creates a new WebsiteMonitor instance.
func NewWebsiteMonitor(db *gorm.DB, logger *zap.Logger) *WebsiteMonitor {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		DialContext: (&net.Dialer{
			Timeout: ConnectionTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout:  ConnectionTimeout,
		DisableKeepAlives:      true,
	}

	client := &http.Client{
		Timeout:   ConnectionTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", MaxRedirects)
			}
			return nil
		},
	}

	return &WebsiteMonitor{
		db:     db,
		logger: logger,
		client: client,
	}
}

// Start launches a background goroutine that periodically performs website health checks.
func (m *WebsiteMonitor) Start(ctx context.Context) {
	go m.run(ctx)
	m.logger.Info("Website monitor started")
}

// run is the main loop for the website monitor background goroutine.
func (m *WebsiteMonitor) run(ctx context.Context) {
	// Run immediately on start.
	m.runCycle(ctx)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Website monitor stopped")
			return
		case <-ticker.C:
			m.runCycle(ctx)
		}
	}
}

// runCycle checks which domains are due for health checks and performs them.
func (m *WebsiteMonitor) runCycle(ctx context.Context) {
	cycleCtx, cancel := context.WithTimeout(ctx, MonitorCycleDuration)
	defer cancel()

	// Load all domains with a website URL configured.
	var domains []domain.NormalizedDomain
	if err := m.db.WithContext(cycleCtx).
		Where("website_url != '' AND website_url IS NOT NULL").
		Find(&domains).Error; err != nil {
		m.logger.Error("failed to load domains for website monitoring", zap.Error(err))
		return
	}

	now := time.Now()
	for _, d := range domains {
		select {
		case <-cycleCtx.Done():
			m.logger.Warn("website monitoring cycle timeout")
			return
		default:
		}

		// Check if this domain is due for a health check.
		if !m.isDue(cycleCtx, d, now) {
			continue
		}

		m.checkDomain(cycleCtx, d, now)
	}
}

// isDue checks whether a domain is due for its next health check based on its check interval.
func (m *WebsiteMonitor) isDue(ctx context.Context, d domain.NormalizedDomain, now time.Time) bool {
	interval := d.CheckIntervalMinutes
	if interval <= 0 {
		interval = DefaultCheckIntervalMinutes
	}

	// Get the most recent health check for this domain.
	var lastCheck domain.HealthCheck
	err := m.db.WithContext(ctx).
		Where("domain_id = ?", d.ID).
		Order("checked_at DESC").
		First(&lastCheck).Error
	if err != nil {
		// No previous check found — it's due.
		return true
	}

	return now.Sub(lastCheck.CheckedAt) >= time.Duration(interval)*time.Minute
}

// checkDomain performs a single health check for the given domain.
func (m *WebsiteMonitor) checkDomain(ctx context.Context, d domain.NormalizedDomain, now time.Time) {
	result := m.performCheck(ctx, d.WebsiteURL)

	// Record the health check result.
	healthCheck := domain.HealthCheck{
		DomainID:        d.ID,
		HTTPStatusCode:  result.StatusCode,
		ResponseTimeMs:  result.ResponseTimeMs,
		SSLValid:        result.SSLValid,
		SSLExpiry:       result.SSLExpiry,
		RedirectChain:   strings.Join(result.RedirectChain, " -> "),
		CheckType:       "http",
		FailureCategory: result.FailureCategory,
		FailureDetail:   result.FailureDetail,
		CheckedAt:       now,
	}

	if err := m.db.WithContext(ctx).Create(&healthCheck).Error; err != nil {
		m.logger.Error("failed to save health check result",
			zap.String("domain", d.DomainName),
			zap.Error(err))
		return
	}

	// Check for downtime condition: 3 consecutive failures.
	m.evaluateDowntime(ctx, d, result, now)
}

// CheckResult holds the outcome of a single HTTP health check.
type CheckResult struct {
	StatusCode      int
	ResponseTimeMs  int
	SSLValid        bool
	SSLExpiry       *time.Time
	RedirectChain   []string
	FailureCategory string
	FailureDetail   string
	IsSuccess       bool
}

// performCheck executes an HTTP GET request and collects the result.
func (m *WebsiteMonitor) performCheck(ctx context.Context, websiteURL string) CheckResult {
	result := CheckResult{}

	// Validate URL.
	parsedURL, err := url.Parse(websiteURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		result.FailureCategory = FailureCategoryConnectivity
		result.FailureDetail = fmt.Sprintf("invalid URL: %s", websiteURL)
		return result
	}

	// Track redirects.
	var redirectChain []string
	redirectChain = append(redirectChain, websiteURL)

	client := &http.Client{
		Timeout:   ConnectionTimeout,
		Transport: m.client.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", MaxRedirects)
			}
			redirectChain = append(redirectChain, req.URL.String())
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, websiteURL, nil)
	if err != nil {
		result.FailureCategory = FailureCategoryConnectivity
		result.FailureDetail = fmt.Sprintf("failed to create request: %v", err)
		return result
	}
	req.Header.Set("User-Agent", "DomainRadar-HealthCheck/1.0")

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	result.ResponseTimeMs = int(elapsed.Milliseconds())
	result.RedirectChain = redirectChain

	if err != nil {
		result.FailureCategory = classifyError(err)
		result.FailureDetail = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// Check SSL certificate if HTTPS.
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		result.SSLValid = time.Now().Before(cert.NotAfter) && time.Now().After(cert.NotBefore)
		result.SSLExpiry = &cert.NotAfter
	}

	// Determine success: 2xx status code.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.IsSuccess = true
	} else {
		result.FailureCategory = FailureCategoryHTTPError
		result.FailureDetail = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return result
}

// classifyError categorizes network errors into DNS or connectivity failures.
func classifyError(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	// Check for DNS resolution errors.
	var dnsErr *net.DNSError
	if isType(err, &dnsErr) {
		return FailureCategoryDNS
	}
	if strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "dns") ||
		strings.Contains(errStr, "lookup") {
		return FailureCategoryDNS
	}

	// All other network errors are connectivity issues.
	return FailureCategoryConnectivity
}

// isType checks if err or any wrapped error matches the target type.
func isType[T error](err error, target *T) bool {
	for err != nil {
		if _, ok := err.(T); ok {
			return true
		}
		unwrap, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = unwrap.Unwrap()
	}
	return false
}

// evaluateDowntime checks if the domain has had ConsecutiveFailuresForAlert consecutive
// failures and generates/clears downtime alerts accordingly.
func (m *WebsiteMonitor) evaluateDowntime(ctx context.Context, d domain.NormalizedDomain, currentResult CheckResult, now time.Time) {
	threshold := d.ResponseTimeThresholdMs
	if threshold <= 0 {
		threshold = DefaultResponseTimeThresholdMs
	}

	// Determine if the current check is a failure (non-2xx or exceeded threshold).
	isFailure := !currentResult.IsSuccess || (currentResult.IsSuccess && currentResult.ResponseTimeMs > threshold)

	// Get the last N checks for this domain to evaluate consecutive failures.
	var recentChecks []domain.HealthCheck
	m.db.WithContext(ctx).
		Where("domain_id = ?", d.ID).
		Order("checked_at DESC").
		Limit(ConsecutiveFailuresForAlert).
		Find(&recentChecks)

	if !isFailure {
		// Recovery: check if there was a previous downtime alert that should be cleared.
		m.handleRecovery(ctx, d, recentChecks, now)
		return
	}

	// Count consecutive failures (including the current one already saved).
	consecutiveFailures := countConsecutiveFailures(recentChecks, threshold)

	if consecutiveFailures >= ConsecutiveFailuresForAlert {
		// Check if a downtime alert already exists for this domain (not acknowledged).
		var existingAlert domain.Alert
		err := m.db.WithContext(ctx).
			Where("domain_id = ? AND alert_type = ? AND acknowledged = ?", d.ID, "downtime", false).
			First(&existingAlert).Error
		if err == nil {
			// Alert already exists, don't duplicate.
			return
		}

		// Generate downtime alert.
		alert := domain.Alert{
			DomainID:       d.ID,
			AlertType:      "downtime",
			Severity:       "critical",
			Message:        m.buildDowntimeMessage(d, currentResult),
			DeliveryStatus: "pending",
			GeneratedAt:    now,
		}

		if err := m.db.WithContext(ctx).Create(&alert).Error; err != nil {
			m.logger.Error("failed to create downtime alert",
				zap.String("domain", d.DomainName),
				zap.Error(err))
			return
		}

		m.logger.Info("downtime alert created",
			zap.String("domain", d.DomainName),
			zap.String("failure_category", currentResult.FailureCategory))
	}
}

// handleRecovery generates a recovery notification if the domain was previously in downtime.
func (m *WebsiteMonitor) handleRecovery(ctx context.Context, d domain.NormalizedDomain, recentChecks []domain.HealthCheck, now time.Time) {
	// Check if there's an active downtime alert.
	var activeAlert domain.Alert
	err := m.db.WithContext(ctx).
		Where("domain_id = ? AND alert_type = ? AND acknowledged = ?", d.ID, "downtime", false).
		First(&activeAlert).Error
	if err != nil {
		// No active downtime alert, nothing to recover from.
		return
	}

	// Calculate total downtime duration.
	downtimeDuration := now.Sub(activeAlert.GeneratedAt)

	// Acknowledge the downtime alert.
	m.db.WithContext(ctx).Model(&activeAlert).Updates(map[string]interface{}{
		"acknowledged":    true,
		"acknowledged_at": now,
	})

	// Create a recovery alert.
	recoveryAlert := domain.Alert{
		DomainID:       d.ID,
		AlertType:      "downtime",
		Severity:       "informational",
		Message:        fmt.Sprintf("Domain %s recovered. Total downtime: %s.", d.DomainName, formatDuration(downtimeDuration)),
		DeliveryStatus: "pending",
		GeneratedAt:    now,
	}

	if err := m.db.WithContext(ctx).Create(&recoveryAlert).Error; err != nil {
		m.logger.Error("failed to create recovery alert",
			zap.String("domain", d.DomainName),
			zap.Error(err))
		return
	}

	m.logger.Info("recovery alert created",
		zap.String("domain", d.DomainName),
		zap.Duration("downtime", downtimeDuration))
}

// countConsecutiveFailures counts consecutive failed checks from the most recent.
func countConsecutiveFailures(checks []domain.HealthCheck, threshold int) int {
	count := 0
	for _, check := range checks {
		isFail := check.FailureCategory != "" || check.HTTPStatusCode < 200 || check.HTTPStatusCode >= 300 || check.ResponseTimeMs > threshold
		if isFail {
			count++
		} else {
			break
		}
	}
	return count
}

// buildDowntimeMessage constructs a human-readable downtime alert message.
func (m *WebsiteMonitor) buildDowntimeMessage(d domain.NormalizedDomain, result CheckResult) string {
	switch result.FailureCategory {
	case FailureCategoryDNS:
		return fmt.Sprintf("Domain %s is experiencing a DNS resolution failure: %s", d.DomainName, result.FailureDetail)
	case FailureCategoryConnectivity:
		return fmt.Sprintf("Domain %s is unreachable due to connectivity issues: %s", d.DomainName, result.FailureDetail)
	case FailureCategoryHTTPError:
		return fmt.Sprintf("Domain %s returned HTTP error: %s", d.DomainName, result.FailureDetail)
	default:
		if result.ResponseTimeMs > 0 {
			return fmt.Sprintf("Domain %s response time exceeded threshold: %dms", d.DomainName, result.ResponseTimeMs)
		}
		return fmt.Sprintf("Domain %s is experiencing downtime", d.DomainName)
	}
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%d hours", hours)
}

// CalculateUptime calculates the uptime percentage for a domain over a given period.
// Returns a percentage rounded to two decimal places.
func CalculateUptime(totalChecks, successfulChecks int) float64 {
	if totalChecks == 0 {
		return 100.00
	}
	percentage := float64(successfulChecks) / float64(totalChecks) * 100
	// Round to 2 decimal places.
	return float64(int(percentage*100)) / 100
}

// DetectDowntime returns true if there are N or more consecutive failures.
func DetectDowntime(checks []domain.HealthCheck, threshold int, requiredConsecutive int) bool {
	return countConsecutiveFailures(checks, threshold) >= requiredConsecutive
}

// CalculateDowntimeDuration returns the duration between the first failure
// and the first subsequent success in a sequence of checks.
// Checks must be sorted by CheckedAt ascending.
func CalculateDowntimeDuration(checks []domain.HealthCheck, threshold int) time.Duration {
	if len(checks) == 0 {
		return 0
	}

	var firstFailure *time.Time
	var firstRecovery *time.Time

	for _, check := range checks {
		isFail := check.FailureCategory != "" || check.HTTPStatusCode < 200 || check.HTTPStatusCode >= 300 || check.ResponseTimeMs > threshold
		if isFail {
			if firstFailure == nil {
				t := check.CheckedAt
				firstFailure = &t
			}
		} else {
			if firstFailure != nil && firstRecovery == nil {
				t := check.CheckedAt
				firstRecovery = &t
			}
		}
	}

	if firstFailure == nil {
		return 0
	}
	if firstRecovery == nil {
		// Still down — measure from first failure to now.
		return time.Since(*firstFailure)
	}
	return firstRecovery.Sub(*firstFailure)
}

// ClassifyFailure returns the failure category for a given error.
// Exported for testing purposes.
func ClassifyFailure(err error) string {
	return classifyError(err)
}
