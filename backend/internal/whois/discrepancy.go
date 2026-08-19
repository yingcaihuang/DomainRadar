package whois

import (
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"domainradar/internal/domain"
)

const (
	// DiscrepancyThresholdHours is the minimum difference in hours to flag a discrepancy.
	DiscrepancyThresholdHours = 24.0
)

// DiscrepancyResult holds the outcome of comparing WHOIS and manual expiration dates.
type DiscrepancyResult struct {
	HasDiscrepancy  bool       `json:"has_discrepancy"`
	ManualExpiry    *time.Time `json:"manual_expiry,omitempty"`
	WhoisExpiry     *time.Time `json:"whois_expiry,omitempty"`
	DifferenceHours float64   `json:"difference_hours"`
}

// DetectDiscrepancy compares a manual expiration date against a WHOIS expiration date.
// If either is nil, no discrepancy is detected. A discrepancy is flagged when the
// absolute difference exceeds 24 hours.
func DetectDiscrepancy(manualExpiry, whoisExpiry *time.Time) *DiscrepancyResult {
	result := &DiscrepancyResult{
		ManualExpiry: manualExpiry,
		WhoisExpiry:  whoisExpiry,
	}

	if manualExpiry == nil || whoisExpiry == nil {
		result.HasDiscrepancy = false
		result.DifferenceHours = 0
		return result
	}

	diff := manualExpiry.Sub(*whoisExpiry)
	absDiffHours := math.Abs(diff.Hours())

	result.DifferenceHours = absDiffHours
	result.HasDiscrepancy = absDiffHours > DiscrepancyThresholdHours

	return result
}

// CheckAndFlagDiscrepancy loads a domain from the database and checks for WHOIS
// expiration date discrepancy. If the domain has DataSourceType "manual" and the
// WHOIS expiration date differs by more than 24 hours, a discrepancy note is
// appended to the domain's Notes field.
func CheckAndFlagDiscrepancy(db *gorm.DB, domainID uint, whoisExpiry *time.Time, logger *zap.Logger) error {
	if whoisExpiry == nil {
		return nil
	}

	var d domain.NormalizedDomain
	if err := db.First(&d, domainID).Error; err != nil {
		return fmt.Errorf("loading domain %d: %w", domainID, err)
	}

	// Only flag discrepancies for manually entered domains.
	if d.DataSourceType != "manual" {
		return nil
	}

	discrepancy := DetectDiscrepancy(d.ExpirationDate, whoisExpiry)
	if !discrepancy.HasDiscrepancy {
		return nil
	}

	// Build discrepancy message.
	msg := fmt.Sprintf(
		"[WHOIS Discrepancy] Manual expiry: %s, WHOIS expiry: %s, difference: %.1f hours",
		formatTime(d.ExpirationDate),
		formatTime(whoisExpiry),
		discrepancy.DifferenceHours,
	)

	// Append to the domain notes to persist the discrepancy flag.
	updatedNotes := d.Notes
	if updatedNotes != "" {
		updatedNotes += "\n"
	}
	updatedNotes += msg

	if err := db.Model(&domain.NormalizedDomain{}).
		Where("id = ?", domainID).
		Update("notes", updatedNotes).Error; err != nil {
		return fmt.Errorf("updating domain %d notes: %w", domainID, err)
	}

	if logger != nil {
		logger.Info("WHOIS discrepancy detected",
			zap.Uint("domain_id", domainID),
			zap.String("domain_name", d.DomainName),
			zap.Float64("difference_hours", discrepancy.DifferenceHours),
		)
	}

	return nil
}

// formatTime formats a time pointer for display, returning "N/A" for nil.
func formatTime(t *time.Time) string {
	if t == nil {
		return "N/A"
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
