package sync

import (
	"fmt"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MergeResult holds the outcome of a merge operation for logging.
type MergeResult struct {
	StartedAt      time.Time
	EndedAt        time.Time
	DomainsSynced  int
	DomainsUpdated int
	DomainsCreated int
	Errors         []string
}

// MergeDomains merges remote (API-sourced) domains into the local database for a
// given registrar account. It updates API-sourced fields while preserving user-defined
// fields (group_id, notes, tags). New domains are created with data_source_type="api".
// Returns the number of domains synced, updated, and any error encountered.
func MergeDomains(db *gorm.DB, accountID uint, remoteDomains []domain.NormalizedDomain, logger *zap.Logger) (synced int, updated int, err error) {
	startTime := time.Now()
	synced = len(remoteDomains)
	var errors []string

	presentNames := make([]string, 0, len(remoteDomains))

	for _, remote := range remoteDomains {
		presentNames = append(presentNames, remote.DomainName)

		var existing domain.NormalizedDomain
		result := db.Where("domain_name = ?", remote.DomainName).First(&existing)

		if result.Error == gorm.ErrRecordNotFound {
			// Create new record with API data.
			newDomain := remote
			newDomain.DataSourceType = "api"
			newDomain.RegistrarAccountID = &accountID
			newDomain.AbsenceCount = 0
			now := time.Now()
			newDomain.LastSyncAt = &now

			if createErr := db.Create(&newDomain).Error; createErr != nil {
				errors = append(errors, fmt.Sprintf("failed to create domain %s: %v", remote.DomainName, createErr))
				if logger != nil {
					logger.Error("Failed to create domain during merge",
						zap.String("domain_name", remote.DomainName),
						zap.Error(createErr),
					)
				}
				continue
			}
			updated++ // counts as a new domain inserted
		} else if result.Error != nil {
			errors = append(errors, fmt.Sprintf("failed to query domain %s: %v", remote.DomainName, result.Error))
			if logger != nil {
				logger.Error("Failed to query domain during merge",
					zap.String("domain_name", remote.DomainName),
					zap.Error(result.Error),
				)
			}
			continue
		} else {
			// Domain exists — detect expiration change before updating.
			expirationChanged := DetectExpirationChange(existing.ExpirationDate, remote.ExpirationDate)

			// Update only API-sourced fields, preserving user-defined fields (group_id, notes, tags).
			now := time.Now()

			// Serialize nameservers to JSON string for the update map.
			var nameserversValue interface{}
			if remote.Nameservers != nil {
				nsJSON, _ := remote.Nameservers.Value()
				nameserversValue = nsJSON
			}

			updateFields := map[string]interface{}{
				"expiration_date":    remote.ExpirationDate,
				"creation_date":      remote.CreationDate,
				"auto_renew":         remote.AutoRenew,
				"renewal_deadline":   remote.RenewalDeadline,
				"status":             remote.Status,
				"nameservers":        nameserversValue,
				"privacy_protection": remote.PrivacyProtection,
				"lock_status":        remote.LockStatus,
				"last_sync_at":       &now,
				"absence_count":      0, // Reset absence count since domain is present.
			}

			if updateErr := db.Model(&existing).Updates(updateFields).Error; updateErr != nil {
				errors = append(errors, fmt.Sprintf("failed to update domain %s: %v", remote.DomainName, updateErr))
				if logger != nil {
					logger.Error("Failed to update domain during merge",
						zap.String("domain_name", remote.DomainName),
						zap.Error(updateErr),
					)
				}
				continue
			}
			updated++

			// If expiration date changed, trigger sync frequency recalculation.
			if expirationChanged && remote.ExpirationDate != nil {
				newInterval := CalculateSyncInterval(*remote.ExpirationDate)
				newIntervalHours := int(newInterval.Hours())

				// Update the registrar account's sync interval if this domain
				// warrants more frequent syncing.
				var account domain.RegistrarAccount
				if accErr := db.First(&account, accountID).Error; accErr == nil {
					currentIntervalHours := account.SyncIntervalHours
					if newIntervalHours < currentIntervalHours {
						db.Model(&account).Update("sync_interval_hours", newIntervalHours)
					}
				}

				if logger != nil {
					logger.Info("Expiration date changed, sync frequency recalculated",
						zap.String("domain_name", remote.DomainName),
						zap.Duration("new_interval", newInterval),
					)
				}
			}
		}
	}

	// Track absences for domains belonging to this account that are NOT in the remote list.
	if absErr := TrackAbsences(db, accountID, presentNames, logger); absErr != nil {
		errors = append(errors, fmt.Sprintf("absence tracking failed: %v", absErr))
		if logger != nil {
			logger.Error("Failed to track absences",
				zap.Uint("account_id", accountID),
				zap.Error(absErr),
			)
		}
	}

	endTime := time.Now()

	// Log the sync operation summary.
	if logger != nil {
		logger.Info("Merge operation completed",
			zap.Uint("account_id", accountID),
			zap.Time("start_time", startTime),
			zap.Time("end_time", endTime),
			zap.Int("domains_synced", synced),
			zap.Int("domains_updated", updated),
			zap.Int("error_count", len(errors)),
		)
	}

	if len(errors) > 0 {
		return synced, updated, fmt.Errorf("merge completed with %d errors: %v", len(errors), errors[0])
	}

	return synced, updated, nil
}

// TrackAbsences increments the absence counter for domains belonging to the
// given account that are NOT present in the remote domain list. After 2
// consecutive absences, the domain status is set to "unverified-removed".
// Domains that ARE present have their absence count reset to 0.
func TrackAbsences(db *gorm.DB, accountID uint, presentDomainNames []string, logger *zap.Logger) error {
	// Find domains belonging to this account that are NOT in the present list.
	query := db.Model(&domain.NormalizedDomain{}).
		Where("registrar_account_id = ?", accountID).
		Where("status != ?", "unverified-removed")

	if len(presentDomainNames) > 0 {
		query = query.Where("domain_name NOT IN ?", presentDomainNames)
	}

	var absentDomains []domain.NormalizedDomain
	if err := query.Find(&absentDomains).Error; err != nil {
		return fmt.Errorf("failed to find absent domains: %w", err)
	}

	for _, d := range absentDomains {
		newAbsenceCount := d.AbsenceCount + 1
		updates := map[string]interface{}{
			"absence_count": newAbsenceCount,
		}

		// After 2 consecutive absences, mark as "unverified-removed".
		if newAbsenceCount >= 2 {
			updates["status"] = "unverified-removed"
			if logger != nil {
				logger.Warn("Domain marked as unverified-removed after 2 consecutive absences",
					zap.String("domain_name", d.DomainName),
					zap.Uint("account_id", accountID),
				)
			}
		}

		if err := db.Model(&domain.NormalizedDomain{}).
			Where("id = ?", d.ID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update absence for domain %s: %w", d.DomainName, err)
		}
	}

	// Reset absence count for present domains (already handled in MergeDomains
	// via the update fields, but also handle it here for domains that were found
	// via a direct match without going through MergeDomains update path).
	if len(presentDomainNames) > 0 {
		if err := db.Model(&domain.NormalizedDomain{}).
			Where("registrar_account_id = ?", accountID).
			Where("domain_name IN ?", presentDomainNames).
			Where("absence_count > ?", 0).
			Update("absence_count", 0).Error; err != nil {
			return fmt.Errorf("failed to reset absence count: %w", err)
		}
	}

	return nil
}

// DetectExpirationChange returns true if the expiration date has changed between
// the old and new values. This is used to trigger sync frequency recalculation
// within 5 minutes of detecting the change.
func DetectExpirationChange(oldExpiry, newExpiry *time.Time) bool {
	// If both are nil, no change.
	if oldExpiry == nil && newExpiry == nil {
		return false
	}
	// If one is nil and the other is not, it's a change.
	if oldExpiry == nil || newExpiry == nil {
		return true
	}
	// Compare the time values (ignoring sub-second differences for robustness).
	return !oldExpiry.Truncate(time.Second).Equal(newExpiry.Truncate(time.Second))
}
