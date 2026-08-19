package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"domainradar/internal/adapter"
	"domainradar/internal/crypto"
	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// Sync frequency tiers based on expiration proximity.
	WeeklyInterval  = 168 * time.Hour // >90 days until expiry
	DailyInterval   = 24 * time.Hour  // 30-90 days until expiry
	TwelveHInterval = 12 * time.Hour  // <30 days until expiry

	// Override clamping bounds.
	MinInterval = 1 * time.Hour       // minimum allowed sync interval
	MaxInterval = 720 * time.Hour     // maximum allowed sync interval (30 days)

	// Maximum duration for a single sync cycle before abort.
	MaxCycleDuration = 10 * time.Minute

	// Scheduler tick interval for checking accounts.
	schedulerTickInterval = 1 * time.Minute
)

// SyncScheduler orchestrates periodic domain data synchronization with smart
// frequency scheduling based on expiration proximity.
type SyncScheduler struct {
	db              *gorm.DB
	adapterRegistry *adapter.AdapterRegistry
	cryptoService   *crypto.CryptoService
	logger          *zap.Logger

	// mu protects lastSyncTimes.
	mu            sync.RWMutex
	lastSyncTimes map[uint]time.Time // accountID -> last sync start time
}

// NewSyncScheduler creates a new SyncScheduler instance.
func NewSyncScheduler(
	db *gorm.DB,
	adapterRegistry *adapter.AdapterRegistry,
	cryptoService *crypto.CryptoService,
	logger *zap.Logger,
) *SyncScheduler {
	return &SyncScheduler{
		db:              db,
		adapterRegistry: adapterRegistry,
		cryptoService:   cryptoService,
		logger:          logger,
		lastSyncTimes:   make(map[uint]time.Time),
	}
}

// CalculateSyncInterval determines the sync interval based on expiration proximity.
//   - >90 days until expiry: weekly (168h)
//   - 30-90 days until expiry: daily (24h)
//   - <30 days until expiry: every 12 hours
func CalculateSyncInterval(expiresAt time.Time) time.Duration {
	daysUntilExpiry := time.Until(expiresAt).Hours() / 24
	switch {
	case daysUntilExpiry > 90:
		return WeeklyInterval
	case daysUntilExpiry > 30:
		return DailyInterval
	default:
		return TwelveHInterval
	}
}

// ClampInterval ensures the given interval is within the allowed bounds [1h, 30d].
func ClampInterval(interval time.Duration) time.Duration {
	if interval < MinInterval {
		return MinInterval
	}
	if interval > MaxInterval {
		return MaxInterval
	}
	return interval
}

// RunSyncCycle executes a full sync for a registrar account.
// It creates a SyncLog entry, decrypts credentials, fetches domains from the
// registrar adapter, and updates the SyncLog with results.
// The cycle is aborted if it exceeds MaxCycleDuration (10 minutes).
func (s *SyncScheduler) RunSyncCycle(ctx context.Context, accountID uint) error {
	// Create context with 10-minute timeout.
	cycleCtx, cancel := context.WithTimeout(ctx, MaxCycleDuration)
	defer cancel()

	// Create SyncLog entry with status "running".
	syncLog := domain.SyncLog{
		RegistrarAccountID: accountID,
		StartedAt:          time.Now(),
		Status:             "running",
	}
	if err := s.db.WithContext(cycleCtx).Create(&syncLog).Error; err != nil {
		return fmt.Errorf("failed to create sync log: %w", err)
	}

	// Fetch the registrar account with its config.
	var account domain.RegistrarAccount
	if err := s.db.WithContext(cycleCtx).
		Preload("RegistrarConfig").
		First(&account, accountID).Error; err != nil {
		s.finalizeSyncLog(ctx, &syncLog, "failed", 0, 0, fmt.Sprintf("account not found: %v", err))
		return fmt.Errorf("failed to find account %d: %w", accountID, err)
	}

	// Decrypt credentials.
	credJSON, err := s.cryptoService.Decrypt(account.CredentialsEncrypted)
	if err != nil {
		s.finalizeSyncLog(ctx, &syncLog, "failed", 0, 0, fmt.Sprintf("credential decryption failed: %v", err))
		return fmt.Errorf("failed to decrypt credentials for account %d: %w", accountID, err)
	}

	var credential adapter.RegistrarCredential
	if err := json.Unmarshal([]byte(credJSON), &credential); err != nil {
		s.finalizeSyncLog(ctx, &syncLog, "failed", 0, 0, fmt.Sprintf("credential parse failed: %v", err))
		return fmt.Errorf("failed to parse credentials for account %d: %w", accountID, err)
	}

	// Get the adapter from the registry.
	registrarAdapter, err := s.adapterRegistry.Get(account.RegistrarConfig.RegistrarType)
	if err != nil {
		s.finalizeSyncLog(ctx, &syncLog, "failed", 0, 0, fmt.Sprintf("adapter not found: %v", err))
		return fmt.Errorf("no adapter for registrar type %s: %w", account.RegistrarConfig.RegistrarType, err)
	}

	// Call adapter.ListDomains with the timeout context.
	domains, err := registrarAdapter.ListDomains(cycleCtx, &credential)
	if err != nil {
		// Check if the error was due to context deadline exceeded (timeout).
		if cycleCtx.Err() == context.DeadlineExceeded {
			s.finalizeSyncLog(ctx, &syncLog, "timeout", 0, 0, "sync cycle exceeded 10-minute maximum duration")
			s.logger.Warn("Sync cycle timed out",
				zap.Uint("account_id", accountID),
				zap.Duration("max_duration", MaxCycleDuration),
			)
			return fmt.Errorf("sync cycle timed out for account %d", accountID)
		}
		s.finalizeSyncLog(ctx, &syncLog, "failed", 0, 0, fmt.Sprintf("list domains failed: %v", err))
		return fmt.Errorf("failed to list domains for account %d: %w", accountID, err)
	}

	// Merge fetched domains into the database.
	synced, mergeUpdated, mergeErr := MergeDomains(s.db.WithContext(ctx), accountID, domains, s.logger)
	if mergeErr != nil {
		s.finalizeSyncLog(ctx, &syncLog, "failed", synced, mergeUpdated, fmt.Sprintf("merge failed: %v", mergeErr))
		return fmt.Errorf("merge failed for account %d: %w", accountID, mergeErr)
	}

	// Update SyncLog with success.
	domainsSynced := synced
	s.finalizeSyncLog(ctx, &syncLog, "completed", domainsSynced, mergeUpdated, "")

	// Update account's last sync time and domain count.
	now := time.Now()
	s.db.WithContext(ctx).Model(&account).Updates(map[string]interface{}{
		"last_sync_at": now,
		"domain_count": domainsSynced,
	})

	s.logger.Info("Sync cycle completed",
		zap.Uint("account_id", accountID),
		zap.Int("domains_synced", domainsSynced),
	)

	return nil
}

// finalizeSyncLog updates the SyncLog entry with the final status and results.
func (s *SyncScheduler) finalizeSyncLog(ctx context.Context, syncLog *domain.SyncLog, status string, synced, updated int, errMsg string) {
	now := time.Now()
	syncLog.EndedAt = &now
	syncLog.Status = status
	syncLog.DomainsSynced = synced
	syncLog.DomainsUpdated = updated
	syncLog.ErrorMessage = errMsg

	if err := s.db.WithContext(ctx).Save(syncLog).Error; err != nil {
		s.logger.Error("Failed to update sync log",
			zap.Uint("sync_log_id", syncLog.ID),
			zap.Error(err),
		)
	}
}

// Start begins the sync scheduler as a background goroutine. It periodically
// checks all accounts and schedules sync cycles based on each account's
// configured sync interval.
func (s *SyncScheduler) Start(ctx context.Context) {
	go s.run(ctx)
	s.logger.Info("Sync scheduler started")
}

// run is the main loop for the scheduler background goroutine.
func (s *SyncScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Sync scheduler stopping", zap.Error(ctx.Err()))
			return
		case <-ticker.C:
			s.checkAndScheduleAccounts(ctx)
		}
	}
}

// checkAndScheduleAccounts queries all active accounts and starts sync cycles
// for those that are due.
func (s *SyncScheduler) checkAndScheduleAccounts(ctx context.Context) {
	var accounts []domain.RegistrarAccount
	if err := s.db.WithContext(ctx).
		Preload("RegistrarConfig").
		Where("status = ?", "connected").
		Find(&accounts).Error; err != nil {
		s.logger.Error("Failed to query registrar accounts", zap.Error(err))
		return
	}

	for _, account := range accounts {
		if s.isDue(account) {
			s.mu.Lock()
			s.lastSyncTimes[account.ID] = time.Now()
			s.mu.Unlock()

			go func(acctID uint) {
				if err := s.RunSyncCycle(ctx, acctID); err != nil {
					s.logger.Error("Sync cycle failed",
						zap.Uint("account_id", acctID),
						zap.Error(err),
					)
				}
			}(account.ID)
		}
	}
}

// isDue checks whether a given account is due for a sync cycle based on its
// configured sync interval and last sync time.
func (s *SyncScheduler) isDue(account domain.RegistrarAccount) bool {
	interval := ClampInterval(time.Duration(account.SyncIntervalHours) * time.Hour)

	s.mu.RLock()
	lastSync, tracked := s.lastSyncTimes[account.ID]
	s.mu.RUnlock()

	// If we haven't tracked this account yet, check the DB last_sync_at.
	if !tracked {
		if account.LastSyncAt == nil {
			// Never synced — it's due.
			return true
		}
		lastSync = *account.LastSyncAt
	}

	return time.Since(lastSync) >= interval
}
