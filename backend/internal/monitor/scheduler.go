package monitor

import (
	"context"
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

// runCheck executes the probe for a monitor and saves the result.
func (s *MonitorScheduler) runCheck(m *domain.ServiceMonitor) {
	check := RunProbe(m)
	if err := s.db.Create(&check).Error; err != nil {
		s.logger.Error("failed to save check result",
			zap.Uint("monitor_id", m.ID),
			zap.Error(err))
		return
	}

	// Cleanup old records: keep only the latest maxChecksPerMonitor
	s.cleanup(m.ID)
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
