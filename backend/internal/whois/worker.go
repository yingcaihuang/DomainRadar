package whois

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"gorm.io/gorm"

	"domainradar/internal/domain"
)

const (
	// QueueKey is the Redis list key for WHOIS query jobs.
	QueueKey = "whois:queue"

	// DefaultWhoDatURL is the default base URL for the who-dat service.
	DefaultWhoDatURL = "http://who-dat:8080"

	// RateLimit is the maximum number of requests per second to who-dat.
	RateLimit = 2
)

// WHOISWorker processes WHOIS lookups via the who-dat REST API using a Redis queue.
type WHOISWorker struct {
	redis       *redis.Client
	db          *gorm.DB
	whoDatURL   string
	rateLimiter *rate.Limiter
	httpClient  *http.Client
	logger      *zap.Logger
}

// WHOISQueryJob represents a job in the Redis queue.
type WHOISQueryJob struct {
	DomainID   uint   `json:"domain_id"`
	DomainName string `json:"domain_name"`
	Retries    int    `json:"retries"`
	NextRetry  int64  `json:"next_retry"` // unix timestamp
}

// WHOISResult holds the parsed WHOIS data from who-dat.
type WHOISResult struct {
	ExpirationDate *time.Time      `json:"expiration_date"`
	Registrar      string          `json:"registrar"`
	CreationDate   *time.Time      `json:"creation_date"`
	Nameservers    []string        `json:"nameservers"`
	RawResponse    json.RawMessage `json:"raw_response"`
}

// NewWHOISWorker creates a new WHOISWorker with the given dependencies.
func NewWHOISWorker(redisClient *redis.Client, db *gorm.DB, whoDatURL string, logger *zap.Logger) *WHOISWorker {
	if whoDatURL == "" {
		whoDatURL = DefaultWhoDatURL
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &WHOISWorker{
		redis:       redisClient,
		db:          db,
		whoDatURL:   whoDatURL,
		rateLimiter: rate.NewLimiter(rate.Limit(RateLimit), 1),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// QueryDomain calls the who-dat REST API and parses the response into WHOISResult.
func (w *WHOISWorker) QueryDomain(ctx context.Context, domainName string) (*WHOISResult, error) {
	url := fmt.Sprintf("%s/%s", w.whoDatURL, domainName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling who-dat API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("who-dat returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	result, err := parseWhoDatResponse(body)
	if err != nil {
		return nil, fmt.Errorf("parsing who-dat response: %w", err)
	}

	result.RawResponse = json.RawMessage(body)
	return result, nil
}

// CalculateQueryInterval returns the appropriate query interval based on expiration proximity.
// >90 days: weekly, 30-90 days: daily, <30 days: every 12 hours.
func (w *WHOISWorker) CalculateQueryInterval(expiresAt time.Time) time.Duration {
	daysUntilExpiry := time.Until(expiresAt).Hours() / 24
	switch {
	case daysUntilExpiry > 90:
		return 7 * 24 * time.Hour // weekly
	case daysUntilExpiry > 30:
		return 24 * time.Hour // daily
	default:
		return 12 * time.Hour // every 12 hours
	}
}

// ProcessQueue continuously pops jobs from the Redis queue and processes them.
// It respects the rate limiter before each query and updates the domain in the database.
func (w *WHOISWorker) ProcessQueue(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Pop a job from the Redis list (blocking pop with 5s timeout).
		result, err := w.redis.BRPop(ctx, 5*time.Second, QueueKey).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			// Context cancellation is expected during shutdown.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Error("failed to pop from queue", zap.Error(err))
			continue
		}

		// result[0] is the key name, result[1] is the value.
		var job WHOISQueryJob
		if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
			w.logger.Error("failed to unmarshal job", zap.Error(err), zap.String("raw", result[1]))
			continue
		}

		// Respect deferred retries.
		if job.NextRetry > 0 && time.Now().Unix() < job.NextRetry {
			// Re-enqueue for later processing.
			if err := w.pushJob(ctx, &job); err != nil {
				w.logger.Error("failed to re-enqueue deferred job", zap.Error(err))
			}
			continue
		}

		// Wait for rate limiter.
		if err := w.rateLimiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Error("rate limiter error", zap.Error(err))
			continue
		}

		// Process the job.
		w.processJob(ctx, &job)
	}
}

// processJob handles a single WHOIS query job.
func (w *WHOISWorker) processJob(ctx context.Context, job *WHOISQueryJob) {
	result, err := w.QueryDomain(ctx, job.DomainName)
	if err != nil {
		w.logger.Error("WHOIS query failed",
			zap.Uint("domain_id", job.DomainID),
			zap.String("domain_name", job.DomainName),
			zap.Int("retries", job.Retries),
			zap.Error(err),
		)
		return
	}

	// Check for WHOIS discrepancy against manual entries before updating.
	if result.ExpirationDate != nil {
		if err := CheckAndFlagDiscrepancy(w.db, job.DomainID, result.ExpirationDate, w.logger); err != nil {
			w.logger.Warn("failed to check WHOIS discrepancy",
				zap.Uint("domain_id", job.DomainID),
				zap.Error(err),
			)
		}
	}

	// Update the domain record with WHOIS results.
	if err := w.updateDomain(ctx, job.DomainID, result); err != nil {
		w.logger.Error("failed to update domain with WHOIS data",
			zap.Uint("domain_id", job.DomainID),
			zap.Error(err),
		)
	}
}

// updateDomain persists the WHOIS results to the database.
func (w *WHOISWorker) updateDomain(ctx context.Context, domainID uint, result *WHOISResult) error {
	updates := map[string]interface{}{
		"last_sync_at": time.Now(),
	}

	if result.ExpirationDate != nil {
		updates["expiration_date"] = result.ExpirationDate
	}
	if result.CreationDate != nil {
		updates["creation_date"] = result.CreationDate
	}
	if result.Registrar != "" {
		updates["registrar_identifier"] = result.Registrar
	}
	if len(result.Nameservers) > 0 {
		nsJSON, err := json.Marshal(result.Nameservers)
		if err == nil {
			updates["nameservers"] = string(nsJSON)
		}
	}

	return w.db.WithContext(ctx).
		Model(&domain.NormalizedDomain{}).
		Where("id = ?", domainID).
		Updates(updates).Error
}

// EnqueueDomain adds a domain to the WHOIS query queue.
func (w *WHOISWorker) EnqueueDomain(ctx context.Context, domainID uint, domainName string) error {
	job := WHOISQueryJob{
		DomainID:   domainID,
		DomainName: domainName,
		Retries:    0,
		NextRetry:  0,
	}
	return w.pushJob(ctx, &job)
}

// pushJob serializes and pushes a job to the Redis queue.
func (w *WHOISWorker) pushJob(ctx context.Context, job *WHOISQueryJob) error {
	if w.redis == nil {
		return fmt.Errorf("redis client is nil")
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshaling job: %w", err)
	}
	return w.redis.LPush(ctx, QueueKey, data).Err()
}

// whoDatResponse represents the relevant fields from the who-dat JSON response.
// who-dat returns RDAP/WHOIS data in a structured format.
type whoDatResponse struct {
	Events []whoDatEvent `json:"events"`
	// Entities contains registrar information.
	Entities []whoDatEntity `json:"entities"`
	// Nameservers may be at root level.
	Nameservers []whoDatNameserver `json:"nameservers"`
}

type whoDatEvent struct {
	EventAction string `json:"eventAction"`
	EventDate   string `json:"eventDate"`
}

type whoDatEntity struct {
	Roles      []string         `json:"roles"`
	VCardArray json.RawMessage  `json:"vcardArray"`
	PublicIDs  []whoDatPublicID `json:"publicIds"`
	Handle     string           `json:"handle"`
}

type whoDatPublicID struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

type whoDatNameserver struct {
	LDHName    string `json:"ldhName"`
	ObjectClass string `json:"objectClassName"`
}

// parseWhoDatResponse parses the who-dat JSON response into a WHOISResult.
func parseWhoDatResponse(data []byte) (*WHOISResult, error) {
	var resp whoDatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	result := &WHOISResult{}

	// Extract dates from events.
	for _, event := range resp.Events {
		t, err := parseEventDate(event.EventDate)
		if err != nil {
			continue
		}
		switch event.EventAction {
		case "expiration":
			result.ExpirationDate = &t
		case "registration":
			result.CreationDate = &t
		}
	}

	// Extract registrar from entities with "registrar" role.
	for _, entity := range resp.Entities {
		for _, role := range entity.Roles {
			if role == "registrar" {
				result.Registrar = extractEntityName(entity)
				break
			}
		}
	}

	// Extract nameservers.
	for _, ns := range resp.Nameservers {
		if ns.LDHName != "" {
			result.Nameservers = append(result.Nameservers, ns.LDHName)
		}
	}

	return result, nil
}

// parseEventDate attempts to parse an event date string in various formats.
func parseEventDate(dateStr string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// extractEntityName attempts to get the registrar name from an entity.
func extractEntityName(entity whoDatEntity) string {
	// Try to extract from handle first (common for RDAP responses).
	if entity.Handle != "" {
		return entity.Handle
	}

	// Try to extract from publicIds.
	for _, pid := range entity.PublicIDs {
		if pid.Identifier != "" {
			return pid.Identifier
		}
	}

	return ""
}
