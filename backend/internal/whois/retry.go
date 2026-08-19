package whois

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"domainradar/internal/circuitbreaker"
)

// RetryConfig defines parameters for the exponential backoff retry strategy.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	Multiplier int
}

// DefaultRetryConfig returns the standard retry configuration:
// 3 retries with 2s base delay and 2x multiplier (2s, 4s, 8s).
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  2 * time.Second,
		Multiplier: 2,
	}
}

// CalculateBackoff returns the delay duration for a given retry attempt.
// Retry 0: 2s, Retry 1: 4s, Retry 2: 8s (BaseDelay * Multiplier^retryCount).
func CalculateBackoff(cfg RetryConfig, retryCount int) time.Duration {
	delay := cfg.BaseDelay
	for i := 0; i < retryCount; i++ {
		delay *= time.Duration(cfg.Multiplier)
	}
	return delay
}

// ShouldRetry determines whether a WHOIS query failure is retryable.
// Returns true for HTTP 429 (rate limited), 5xx (server errors), or network errors.
// Returns false for other errors (e.g., 404, parse errors).
func ShouldRetry(statusCode int, err error) bool {
	// HTTP 429 Too Many Requests.
	if statusCode == http.StatusTooManyRequests {
		return true
	}

	// HTTP 5xx server errors.
	if statusCode >= 500 && statusCode < 600 {
		return true
	}

	// Network errors (connection refused, timeout, DNS failure, etc.).
	if err != nil && isNetworkError(err) {
		return true
	}

	return false
}

// isNetworkError checks whether an error is a network-level error.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.Error (timeout, temporary).
	var netErr net.Error
	if isAs(err, &netErr) {
		return true
	}

	// Check for net.OpError (connection refused, etc.).
	var opErr *net.OpError
	if isAs(err, &opErr) {
		return true
	}

	// Check for net.DNSError.
	var dnsErr *net.DNSError
	if isAs(err, &dnsErr) {
		return true
	}

	// Check for common network error messages as a fallback.
	errMsg := err.Error()
	networkIndicators := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"i/o timeout",
		"network is unreachable",
		"calling who-dat API",
	}
	for _, indicator := range networkIndicators {
		if strings.Contains(strings.ToLower(errMsg), indicator) {
			return true
		}
	}

	return false
}

// isAs is a helper wrapping errors.As to keep imports clean.
func isAs[T any](err error, target *T) bool {
	// Use a manual unwrap loop to check type assertions.
	for e := err; e != nil; e = unwrap(e) {
		if _, ok := any(e).(*T); ok {
			return true
		}
		if v, ok := any(e).(T); ok {
			*target = v
			return true
		}
	}
	return false
}

func unwrap(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// RetryableWorker extends WHOISWorker with retry and circuit breaker capabilities.
type RetryableWorker struct {
	*WHOISWorker
	retryConfig    RetryConfig
	circuitBreaker *circuitbreaker.CircuitBreaker
	healthClient   *http.Client
}

// NewRetryableWorker creates a new RetryableWorker wrapping an existing WHOISWorker.
func NewRetryableWorker(worker *WHOISWorker, cb *circuitbreaker.CircuitBreaker) *RetryableWorker {
	return &RetryableWorker{
		WHOISWorker:    worker,
		retryConfig:    DefaultRetryConfig(),
		circuitBreaker: cb,
		healthClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// QueryDomainWithRetry wraps QueryDomain with retry logic and circuit breaker checks.
// On retryable failure: re-enqueues the job with NextRetry set to now + backoff delay.
// After max retries: marks the query as failed and retains previous data.
func (rw *RetryableWorker) QueryDomainWithRetry(ctx context.Context, job *WHOISQueryJob) (*WHOISResult, error) {
	// Check circuit breaker before making the request.
	if rw.circuitBreaker.IsOpen() {
		return nil, fmt.Errorf("circuit breaker is open, who-dat service unavailable")
	}

	result, err := rw.QueryDomain(ctx, job.DomainName)
	if err != nil {
		// Determine if we should retry.
		statusCode := extractStatusCode(err)
		if ShouldRetry(statusCode, err) {
			// Record failure in the circuit breaker.
			rw.circuitBreaker.RecordFailure()

			if job.Retries < rw.retryConfig.MaxRetries {
				// Re-enqueue with backoff delay.
				backoff := CalculateBackoff(rw.retryConfig, job.Retries)
				job.Retries++
				job.NextRetry = time.Now().Add(backoff).Unix()

				if pushErr := rw.pushJob(ctx, job); pushErr != nil {
					rw.logger.Error("failed to re-enqueue job for retry",
						zap.Uint("domain_id", job.DomainID),
						zap.String("domain_name", job.DomainName),
						zap.Error(pushErr),
					)
				} else {
					rw.logger.Info("re-enqueued job for retry",
						zap.Uint("domain_id", job.DomainID),
						zap.String("domain_name", job.DomainName),
						zap.Int("retry", job.Retries),
						zap.Duration("backoff", backoff),
					)
				}
				return nil, fmt.Errorf("retryable error (attempt %d/%d): %w", job.Retries, rw.retryConfig.MaxRetries, err)
			}

			// Max retries exhausted - mark as failed.
			rw.logger.Error("max retries exhausted, marking query as failed",
				zap.Uint("domain_id", job.DomainID),
				zap.String("domain_name", job.DomainName),
				zap.Int("retries", job.Retries),
				zap.Error(err),
			)
			return nil, fmt.Errorf("max retries (%d) exhausted for domain %s: %w", rw.retryConfig.MaxRetries, job.DomainName, err)
		}

		// Non-retryable error (e.g., 404, parse error).
		return nil, err
	}

	// Success - record in circuit breaker.
	rw.circuitBreaker.RecordSuccess()
	return result, nil
}

// CheckWhoDatHealth checks the health of the who-dat service by calling its health endpoint.
func (rw *RetryableWorker) CheckWhoDatHealth(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", rw.whoDatURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating health check request: %w", err)
	}

	resp, err := rw.healthClient.Do(req)
	if err != nil {
		return fmt.Errorf("who-dat health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("who-dat health check returned status %d", resp.StatusCode)
	}

	return nil
}

// StartHealthMonitor runs a background goroutine that checks who-dat health every 30 seconds.
// If 3 consecutive health checks fail, the circuit breaker will open (handled by the circuit breaker logic).
// It logs a warning on startup if the service is unreachable.
func (rw *RetryableWorker) StartHealthMonitor(ctx context.Context) {
	// Perform initial health check on startup.
	if err := rw.CheckWhoDatHealth(ctx); err != nil {
		rw.logger.Warn("who-dat service unreachable on startup, deferring WHOIS queries",
			zap.String("url", rw.whoDatURL),
			zap.Error(err),
		)
		rw.circuitBreaker.RecordFailure()
	} else {
		rw.logger.Info("who-dat service healthy on startup",
			zap.String("url", rw.whoDatURL),
		)
	}

	// Start background health monitor goroutine.
	go rw.healthMonitorLoop(ctx)
}

// healthMonitorLoop periodically checks who-dat health and updates the circuit breaker.
func (rw *RetryableWorker) healthMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rw.CheckWhoDatHealth(ctx); err != nil {
				rw.logger.Warn("who-dat health check failed",
					zap.Error(err),
				)
				rw.circuitBreaker.RecordFailure()
			} else {
				rw.circuitBreaker.RecordSuccess()
			}
		}
	}
}

// extractStatusCode attempts to extract an HTTP status code from a who-dat error message.
// Returns 0 if no status code can be extracted.
func extractStatusCode(err error) int {
	if err == nil {
		return 0
	}

	errMsg := err.Error()
	// Parse "who-dat returned status <code>:" format.
	var code int
	if _, scanErr := fmt.Sscanf(errMsg, "who-dat returned status %d", &code); scanErr == nil {
		return code
	}

	// Also check for wrapped errors.
	if strings.Contains(errMsg, "status 429") {
		return 429
	}
	if strings.Contains(errMsg, "status 500") {
		return 500
	}
	if strings.Contains(errMsg, "status 502") {
		return 502
	}
	if strings.Contains(errMsg, "status 503") {
		return 503
	}
	if strings.Contains(errMsg, "status 504") {
		return 504
	}

	return 0
}
