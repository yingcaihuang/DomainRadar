package domainmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WhoisScheduler periodically checks WHOIS data for all domains.
type WhoisScheduler struct {
	db       *gorm.DB
	logger   *zap.Logger
	interval time.Duration
}

// NewWhoisScheduler creates a new WhoisScheduler (default: 24 hours).
func NewWhoisScheduler(db *gorm.DB, logger *zap.Logger) *WhoisScheduler {
	return &WhoisScheduler{db: db, logger: logger, interval: 24 * time.Hour}
}

// Start launches the WHOIS scheduler.
func (s *WhoisScheduler) Start(ctx context.Context) {
	go func() {
		// Wait 2 minutes before first run to allow other services to start
		time.Sleep(2 * time.Minute)
		s.runChecks(ctx)

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runChecks(ctx)
			}
		}
	}()
	s.logger.Info("WHOIS scheduler started", zap.Duration("interval", s.interval))
}

// runChecks fetches WHOIS for domains that haven't been checked in 24 hours.
func (s *WhoisScheduler) runChecks(ctx context.Context) {
	cutoff := time.Now().Add(-s.interval)

	var domains []domain.NormalizedDomain
	if err := s.db.WithContext(ctx).
		Where("whois_last_checked_at IS NULL OR whois_last_checked_at < ?", cutoff).
		Limit(50). // Process in batches to avoid overwhelming who-dat
		Find(&domains).Error; err != nil {
		s.logger.Error("WHOIS scheduler: failed to query domains", zap.Error(err))
		return
	}

	if len(domains) == 0 {
		return
	}

	whoDatURL := os.Getenv("WHO_DAT_URL")
	if whoDatURL == "" {
		whoDatURL = "http://who-dat:8080"
	}

	updated := 0
	for _, d := range domains {
		if err := s.checkDomain(ctx, &d, whoDatURL); err != nil {
			s.logger.Debug("WHOIS check failed", zap.String("domain", d.DomainName), zap.Error(err))
			continue
		}
		updated++
		// Small delay between requests to be polite
		time.Sleep(500 * time.Millisecond)
	}

	s.logger.Info("WHOIS check batch completed", zap.Int("checked", updated), zap.Int("total", len(domains)))
}

// checkDomain fetches WHOIS for a single domain and updates the DB.
func (s *WhoisScheduler) checkDomain(ctx context.Context, d *domain.NormalizedDomain, whoDatURL string) error {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(whoDatURL + "/" + d.DomainName)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var whoisData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&whoisData); err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}

	now := time.Now()
	updates := map[string]interface{}{
		"whois_last_checked_at": now,
	}

	// Update expiration date from WHOIS if available
	if dates, ok := whoisData["dates"].(map[string]interface{}); ok {
		if expires, ok := dates["expires"].(string); ok && expires != "" {
			if t, err := time.Parse(time.RFC3339, expires); err == nil {
				updates["expiration_date"] = t
			}
		}
		if created, ok := dates["created"].(string); ok && created != "" {
			if t, err := time.Parse(time.RFC3339, created); err == nil {
				updates["creation_date"] = t
			}
		}
	}

	// Update registrar info
	if registrar, ok := whoisData["registrar"].(map[string]interface{}); ok {
		if name, ok := registrar["name"].(string); ok && name != "" {
			// Don't overwrite if already set from adapter
			if d.RegistrarIdentifier == "" {
				updates["registrar_identifier"] = name
			}
		}
	}

	return s.db.WithContext(ctx).Model(d).Updates(updates).Error
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
