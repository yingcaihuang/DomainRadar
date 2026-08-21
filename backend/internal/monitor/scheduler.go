package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// schedulerTickInterval is the interval between scheduler ticks.
	schedulerTickInterval = 30 * time.Second
	// maxChecksPerMonitor is the maximum number of check records to keep per monitor.
	maxChecksPerMonitor = 1000
)

// AlertDispatchFunc is a callback for webhook notification dispatch.
type AlertDispatchFunc func(alert *domain.Alert)

// MonitorScheduler periodically runs probe checks for due service monitors.
type MonitorScheduler struct {
	db             *gorm.DB
	logger         *zap.Logger
	mu             sync.Mutex
	OnAlertCreated AlertDispatchFunc
}

// NewMonitorScheduler creates a new MonitorScheduler.
func NewMonitorScheduler(db *gorm.DB, logger *zap.Logger) *MonitorScheduler {
	return &MonitorScheduler{
		db:     db,
		logger: logger,
	}
}

// Start launches the scheduler loop in a goroutine.
func (s *MonitorScheduler) Start(ctx context.Context) {
	go s.run(ctx)
}

func (s *MonitorScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()

	s.logger.Info("Monitor scheduler started", zap.Duration("tick_interval", schedulerTickInterval))

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Monitor scheduler stopped")
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *MonitorScheduler) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var monitors []domain.ServiceMonitor
	if err := s.db.Where("enabled = ?", true).Find(&monitors).Error; err != nil {
		s.logger.Error("failed to fetch monitors", zap.Error(err))
		return
	}

	now := time.Now()

	for i := range monitors {
		m := &monitors[i]
		if s.isDue(m, now) {
			s.runCheck(m)
		}
	}
}

// isDue checks if a monitor's next check is due based on its last check time.
func (s *MonitorScheduler) isDue(m *domain.ServiceMonitor, now time.Time) bool {
	var lastCheck domain.ServiceCheck
	err := s.db.Where("monitor_id = ?", m.ID).
		Order("checked_at DESC").
		First(&lastCheck).Error

	if err != nil {
		// No previous check, it's due
		return true
	}

	interval := time.Duration(m.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 300 * time.Second
	}

	return now.After(lastCheck.CheckedAt.Add(interval))
}

// consecutiveFailuresThreshold is the number of consecutive failures to trigger an alert.
const consecutiveFailuresThreshold = 3

// runCheck executes the probe for a monitor and saves the result.
func (s *MonitorScheduler) runCheck(m *domain.ServiceMonitor) {
	check := RunProbe(m)
	if err := s.db.Create(&check).Error; err != nil {
		s.logger.Error("failed to save check result",
			zap.Uint("monitor_id", m.ID),
			zap.Error(err))
		return
	}

	// Evaluate alert condition
	s.evaluateAlert(m, &check)

	// Cleanup old records: keep only the latest maxChecksPerMonitor
	s.cleanup(m.ID)
}

// evaluateAlert checks for consecutive failures and creates/clears downtime alerts.
func (s *MonitorScheduler) evaluateAlert(m *domain.ServiceMonitor, currentCheck *domain.ServiceCheck) {
	if currentCheck.Success {
		// Recovery: check if there's an active downtime alert and close it
		var activeAlert domain.Alert
		err := s.db.Where(
			"domain_id = ? AND alert_type = ? AND acknowledged = ?",
			m.DomainID, "service_down", false,
		).First(&activeAlert).Error
		if err == nil {
			// Mark as acknowledged (recovered)
			now := time.Now()
			s.db.Model(&activeAlert).Updates(map[string]interface{}{
				"acknowledged":    true,
				"acknowledged_at": &now,
			})
			// Create recovery alert
			recoveryAlert := domain.Alert{
				DomainID:       m.DomainID,
				AlertType:      "service_recovered",
				Severity:       "informational",
				Message:        fmt.Sprintf("服务监控 [%s] %s 已恢复正常", m.Label, m.Target),
				DeliveryStatus: "pending",
				GeneratedAt:    now,
			}
			if err := s.db.Create(&recoveryAlert).Error; err == nil && s.OnAlertCreated != nil {
				s.OnAlertCreated(&recoveryAlert)
			}
		}
		return
	}

	// Failure: count consecutive failures
	var recentChecks []domain.ServiceCheck
	s.db.Where("monitor_id = ?", m.ID).
		Order("checked_at DESC").
		Limit(consecutiveFailuresThreshold).
		Find(&recentChecks)

	consecutiveFailures := 0
	for _, c := range recentChecks {
		if !c.Success {
			consecutiveFailures++
		} else {
			break
		}
	}

	if consecutiveFailures >= consecutiveFailuresThreshold {
		// Check if alert already exists
		var existing domain.Alert
		err := s.db.Where(
			"domain_id = ? AND alert_type = ? AND acknowledged = ?",
			m.DomainID, "service_down", false,
		).First(&existing).Error

		if err != nil {
			// No existing alert, create one
			now := time.Now()
			alert := domain.Alert{
				DomainID:       m.DomainID,
				AlertType:      "service_down",
				Severity:       "critical",
				Message:        fmt.Sprintf("服务监控 [%s] %s 连续 %d 次探测失败: %s", m.Label, m.Target, consecutiveFailures, currentCheck.Error),
				DeliveryStatus: "pending",
				GeneratedAt:    now,
			}
			if err := s.db.Create(&alert).Error; err != nil {
				s.logger.Error("failed to create service down alert",
					zap.Uint("monitor_id", m.ID), zap.Error(err))
			} else {
				s.logger.Warn("Service down alert created",
					zap.String("target", m.Target),
					zap.Int("consecutive_failures", consecutiveFailures))
				if s.OnAlertCreated != nil {
					s.OnAlertCreated(&alert)
				}
			}
		}
	}
}

// cleanup removes old check records beyond the retention limit.
func (s *MonitorScheduler) cleanup(monitorID uint) {
	var count int64
	if err := s.db.Model(&domain.ServiceCheck{}).
		Where("monitor_id = ?", monitorID).
		Count(&count).Error; err != nil {
		return
	}

	if count <= maxChecksPerMonitor {
		return
	}

	// Find the ID threshold to delete older records
	var cutoff domain.ServiceCheck
	if err := s.db.Where("monitor_id = ?", monitorID).
		Order("checked_at DESC").
		Offset(maxChecksPerMonitor).
		First(&cutoff).Error; err != nil {
		return
	}

	s.db.Where("monitor_id = ? AND checked_at <= ?", monitorID, cutoff.CheckedAt).
		Delete(&domain.ServiceCheck{})
}
