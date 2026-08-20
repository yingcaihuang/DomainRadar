package emailcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// EmailScheduler periodically checks all enabled email monitors.
type EmailScheduler struct {
	db       *gorm.DB
	logger   *zap.Logger
	interval time.Duration
}

// NewEmailScheduler creates a new EmailScheduler.
// interval specifies how often checks run (default 30 minutes if 0).
func NewEmailScheduler(db *gorm.DB, logger *zap.Logger, interval time.Duration) *EmailScheduler {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	return &EmailScheduler{db: db, logger: logger, interval: interval}
}

// Start launches the email scheduler as a background goroutine.
func (s *EmailScheduler) Start(ctx context.Context) {
	go func() {
		// Wait a short time before first run to allow startup to complete
		time.Sleep(10 * time.Second)

		if err := s.RunAllChecks(ctx); err != nil {
			s.logger.Error("Initial email check failed", zap.Error(err))
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RunAllChecks(context.Background()); err != nil {
					s.logger.Error("Periodic email check failed", zap.Error(err))
				}
			}
		}
	}()

	s.logger.Info("Email scheduler started", zap.Duration("interval", s.interval))
}

// RunAllChecks iterates all enabled email monitors due for checking and runs checks.
func (s *EmailScheduler) RunAllChecks(ctx context.Context) error {
	now := time.Now()

	var monitors []domain.EmailMonitor
	if err := s.db.WithContext(ctx).
		Where("enabled = ? AND (next_check_at IS NULL OR next_check_at <= ?)", true, now).
		Preload("Domain").
		Find(&monitors).Error; err != nil {
		return fmt.Errorf("failed to query email monitors: %w", err)
	}

	if len(monitors) == 0 {
		return nil
	}

	checked := 0
	alertsCreated := 0

	for _, m := range monitors {
		domainName := m.Domain.DomainName
		if domainName == "" {
			continue
		}

		// Build config
		config := EmailCheckConfig{}
		if m.DKIMSelectors != "" {
			config.DKIMSelectors = splitSelectors(m.DKIMSelectors)
		}
		if m.MailServerIPs != "" {
			config.MailServerIPs = splitSelectors(m.MailServerIPs)
		}

		// Run the check with a per-domain timeout
		report := RunEmailCheck(domainName, config)

		// Store result
		detailsJSON, _ := json.Marshal(report.Details)
		checkResult := domain.EmailCheckResult{
			DomainID:    m.DomainID,
			MonitorID:   m.ID,
			TotalScore:  report.TotalScore,
			Grade:       report.Grade,
			MXScore:     report.MXScore,
			SPFScore:    report.SPFScore,
			DKIMScore:   report.DKIMScore,
			DMARCScore:  report.DMARCScore,
			PTRScore:    report.PTRScore,
			MTASTSScore: report.MTASTSScore,
			TLSRPTScore: report.TLSRPTScore,
			BIMIScore:   report.BIMIScore,
			Details:     string(detailsJSON),
			CheckedAt:   now,
		}

		if err := s.db.WithContext(ctx).Create(&checkResult).Error; err != nil {
			s.logger.Error("failed to save email check result",
				zap.String("domain", domainName),
				zap.Error(err),
			)
			continue
		}
		checked++

		// Update monitor timestamps
		nextCheck := now.Add(s.interval)
		s.db.WithContext(ctx).Model(&m).Updates(map[string]interface{}{
			"last_checked_at": now,
			"next_check_at":   nextCheck,
		})

		// Cleanup old results (keep last 10)
		s.cleanupOldResults(ctx, m.ID)

		// Alert if score is below 50 or dropped more than 20
		if created := s.maybeCreateAlert(ctx, m, report, now); created {
			alertsCreated++
		}
	}

	s.logger.Info("Email check completed",
		zap.Int("monitors_checked", checked),
		zap.Int("alerts_created", alertsCreated),
	)
	return nil
}

// cleanupOldResults keeps only the last 10 results per monitor.
func (s *EmailScheduler) cleanupOldResults(ctx context.Context, monitorID uint) {
	var count int64
	s.db.WithContext(ctx).Model(&domain.EmailCheckResult{}).Where("monitor_id = ?", monitorID).Count(&count)
	if count > 10 {
		// Find the 10th newest and delete everything older
		var tenthResult domain.EmailCheckResult
		err := s.db.WithContext(ctx).Where("monitor_id = ?", monitorID).
			Order("checked_at DESC").
			Offset(9).
			Limit(1).
			First(&tenthResult).Error
		if err == nil {
			s.db.WithContext(ctx).Where("monitor_id = ? AND checked_at < ?", monitorID, tenthResult.CheckedAt).
				Delete(&domain.EmailCheckResult{})
		}
	}
}

// maybeCreateAlert reads alert rules from DB and creates alerts if conditions are met.
func (s *EmailScheduler) maybeCreateAlert(ctx context.Context, monitor domain.EmailMonitor, report *EmailCheckReport, now time.Time) bool {
	domainName := monitor.Domain.DomainName

	// Load enabled rules from DB
	var rules []domain.EmailAlertRule
	s.db.WithContext(ctx).Where("enabled = ?", true).Find(&rules)

	alertsCreated := false

	for _, rule := range rules {
		var triggered bool
		var message string

		switch rule.RuleType {
		case "total_score":
			if report.TotalScore < rule.Threshold {
				triggered = true
				message = fmt.Sprintf("域名 %s 邮件安全总分 %d/100 低于阈值 %d", domainName, report.TotalScore, rule.Threshold)
			}
		case "score_drop":
			var prev domain.EmailCheckResult
			err := s.db.WithContext(ctx).Where("monitor_id = ?", monitor.ID).
				Order("checked_at DESC").
				Offset(1).
				First(&prev).Error
			if err == nil && (prev.TotalScore-report.TotalScore) > rule.Threshold {
				triggered = true
				message = fmt.Sprintf("域名 %s 邮件安全评分下降 %d 分 (%d → %d)，超过阈值 %d", domainName, prev.TotalScore-report.TotalScore, prev.TotalScore, report.TotalScore, rule.Threshold)
			}
		case "mx_score":
			if report.MXScore <= rule.Threshold {
				triggered = true
				message = fmt.Sprintf("域名 %s MX 记录评分 %d/30 ≤ %d", domainName, report.MXScore, rule.Threshold)
			}
		case "spf_score":
			if report.SPFScore <= rule.Threshold {
				triggered = true
				message = fmt.Sprintf("域名 %s SPF 记录评分 %d/20 ≤ %d", domainName, report.SPFScore, rule.Threshold)
			}
		case "dkim_score":
			if report.DKIMScore <= rule.Threshold {
				triggered = true
				message = fmt.Sprintf("域名 %s DKIM 记录评分 %d/20 ≤ %d", domainName, report.DKIMScore, rule.Threshold)
			}
		case "dmarc_score":
			if report.DMARCScore <= rule.Threshold {
				triggered = true
				message = fmt.Sprintf("域名 %s DMARC 记录评分 %d/15 ≤ %d", domainName, report.DMARCScore, rule.Threshold)
			}
		}

		if !triggered {
			continue
		}

		// Check for existing unacknowledged alert of same type
		alertType := "email_" + rule.RuleType
		var existing domain.Alert
		err := s.db.WithContext(ctx).Where(
			"domain_id = ? AND alert_type = ? AND acknowledged = ?",
			monitor.DomainID, alertType, false,
		).First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			alert := domain.Alert{
				DomainID:     monitor.DomainID,
				AlertType:    alertType,
				Severity:     rule.Severity,
				Message:      message,
				Acknowledged: false,
				GeneratedAt:  now,
			}
			if err := s.db.WithContext(ctx).Create(&alert).Error; err != nil {
				s.logger.Error("failed to create email alert",
					zap.String("domain", domainName),
					zap.Error(err),
				)
			} else {
				alertsCreated = true
			}
		}
	}

	return alertsCreated
}

func splitSelectors(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
