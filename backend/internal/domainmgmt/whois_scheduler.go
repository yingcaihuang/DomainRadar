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

// AlertDispatchFunc is a callback for webhook notification dispatch.
type WhoisAlertDispatchFunc func(alert *domain.Alert)

// WhoisScheduler periodically checks WHOIS data for manually added domains
// (those without a bound registrar account). Domains managed through a
// registrar adapter are updated exclusively by the registrar sync — the
// registry (WHOIS/RDAP) can report a renewed expiration for a domain the
// registrar no longer considers owned (e.g. GoDaddy keeps the registration
// alive during the redemption period).
type WhoisScheduler struct {
	db             *gorm.DB
	logger         *zap.Logger
	interval       time.Duration
	OnAlertCreated WhoisAlertDispatchFunc
}

// NewWhoisScheduler creates a new WhoisScheduler (default: 24 hours).
func NewWhoisScheduler(db *gorm.DB, logger *zap.Logger) *WhoisScheduler {
	return &WhoisScheduler{db: db, logger: logger, interval: 24 * time.Hour}
}

// Start launches the WHOIS scheduler.
func (s *WhoisScheduler) Start(ctx context.Context) {
	go func() {
		// Wait 2 minutes before first run to allow other services to start
		time.Sleep(10 * time.Second)
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

// runChecks fetches WHOIS for manually added domains that haven't been
// checked in 24 hours. Every due domain is checked each cycle (no batch
// limit); the 500ms spacing between requests bounds the load on who-dat.
// Ordering puts the most critical first — never-checked, then oldest-checked,
// tiebroken by nearest expiration — so if a cycle is interrupted the
// highest-risk domains have already been refreshed.
func (s *WhoisScheduler) runChecks(ctx context.Context) {
	cutoff := time.Now().Add(-s.interval)

	var domains []domain.NormalizedDomain
	if err := s.db.WithContext(ctx).
		Where("registrar_account_id IS NULL"). // registrar-bound domains are owned by the sync scheduler
		Where("whois_last_checked_at IS NULL OR whois_last_checked_at < ?", cutoff).
		Order("(whois_last_checked_at IS NULL) DESC"). // never-checked first
		Order("whois_last_checked_at ASC").            // then oldest-checked first
		Order("expiration_date ASC NULLS LAST").       // tiebreak: expired / expiring soon first
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
		if _, err := s.checkDomain(ctx, &d, whoDatURL); err != nil {
			s.logger.Debug("WHOIS check failed", zap.String("domain", d.DomainName), zap.Error(err))
			continue
		}
		updated++
		// Small delay between requests to be polite
		time.Sleep(500 * time.Millisecond)
	}

	s.logger.Info("WHOIS check batch completed", zap.Int("checked", updated), zap.Int("total", len(domains)))
}

// CheckNow performs an immediate WHOIS check for a single domain, persists
// the results and returns the raw who-dat response together with the
// reloaded domain. It backs the manual refresh endpoint for manually added
// domains.
func (s *WhoisScheduler) CheckNow(ctx context.Context, domainID uint) (map[string]interface{}, *domain.NormalizedDomain, error) {
	var d domain.NormalizedDomain
	if err := s.db.WithContext(ctx).First(&d, domainID).Error; err != nil {
		return nil, nil, fmt.Errorf("loading domain %d: %w", domainID, err)
	}
	if d.RegistrarAccountID != nil {
		return nil, nil, fmt.Errorf("domain %s is managed by a registrar account", d.DomainName)
	}

	whoDatURL := os.Getenv("WHO_DAT_URL")
	if whoDatURL == "" {
		whoDatURL = "http://who-dat:8080"
	}

	whoisData, err := s.checkDomain(ctx, &d, whoDatURL)
	if err != nil {
		return nil, nil, err
	}

	var reloaded domain.NormalizedDomain
	if err := s.db.WithContext(ctx).First(&reloaded, domainID).Error; err != nil {
		return whoisData, nil, fmt.Errorf("reloading domain %d: %w", domainID, err)
	}
	return whoisData, &reloaded, nil
}

// checkDomain fetches WHOIS for a single domain and updates the DB.
func (s *WhoisScheduler) checkDomain(ctx context.Context, d *domain.NormalizedDomain, whoDatURL string) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(whoDatURL + "/" + d.DomainName)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var whoisData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&whoisData); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
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

	// Derive status from the WHOIS expiration date.
	if expDate, ok := updates["expiration_date"]; ok {
		t := expDate.(time.Time)
		if t.After(now) && d.Status == "expired" {
			updates["status"] = "active"
		} else if t.Before(now) && d.Status == "active" {
			updates["status"] = "expired"
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

	// Detect changes that warrant an alert
	var changes []string
	if expDate, ok := updates["expiration_date"]; ok {
		if d.ExpirationDate != nil {
			newExp := expDate.(time.Time)
			// Compare with 1-day tolerance — WHOIS and registrar APIs often
			// represent the same expiration with different timezone offsets,
			// causing ±1 day differences (e.g. GoDaddy UTC-7 vs WHOIS UTC)
			diff := newExp.Sub(*d.ExpirationDate)
			if diff < 0 {
				diff = -diff
			}
			if diff > 36*time.Hour { // more than 1.5 days = real change
				changes = append(changes, fmt.Sprintf("到期日变更: %s → %s",
					d.ExpirationDate.Format("2006-01-02"), newExp.Format("2006-01-02")))
			}
		}
	}

	if err := s.db.WithContext(ctx).Model(d).Updates(updates).Error; err != nil {
		return nil, err
	}

	// Create alert for WHOIS changes
	if len(changes) > 0 && s.OnAlertCreated != nil {
		alert := domain.Alert{
			DomainID:       d.ID,
			AlertType:      "whois_change",
			Severity:       "informational",
			Message:        fmt.Sprintf("域名 %s WHOIS 信息变更: %s", d.DomainName, changes[0]),
			DeliveryStatus: "pending",
			GeneratedAt:    now,
		}
		if err := s.db.WithContext(ctx).Create(&alert).Error; err == nil {
			s.OnAlertCreated(&alert)
		}
	}

	return whoisData, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
