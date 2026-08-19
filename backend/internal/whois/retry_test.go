package whois

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domainradar/internal/circuitbreaker"
)

func TestCalculateBackoff(t *testing.T) {
	cfg := DefaultRetryConfig()

	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{0, 2 * time.Second},  // BaseDelay * 2^0 = 2s
		{1, 4 * time.Second},  // BaseDelay * 2^1 = 4s
		{2, 8 * time.Second},  // BaseDelay * 2^2 = 8s
		{3, 16 * time.Second}, // BaseDelay * 2^3 = 16s (beyond max retries, but function still works)
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("retry_%d", tc.retryCount), func(t *testing.T) {
			delay := CalculateBackoff(cfg, tc.retryCount)
			assert.Equal(t, tc.expected, delay)
		})
	}
}

func TestCalculateBackoff_CustomConfig(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 5,
		BaseDelay:  1 * time.Second,
		Multiplier: 3,
	}

	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{0, 1 * time.Second},  // 1s * 3^0 = 1s
		{1, 3 * time.Second},  // 1s * 3^1 = 3s
		{2, 9 * time.Second},  // 1s * 3^2 = 9s
		{3, 27 * time.Second}, // 1s * 3^3 = 27s
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("retry_%d", tc.retryCount), func(t *testing.T) {
			delay := CalculateBackoff(cfg, tc.retryCount)
			assert.Equal(t, tc.expected, delay)
		})
	}
}

func TestShouldRetry_HTTP429(t *testing.T) {
	assert.True(t, ShouldRetry(429, nil))
}

func TestShouldRetry_HTTP5xx(t *testing.T) {
	assert.True(t, ShouldRetry(500, nil))
	assert.True(t, ShouldRetry(502, nil))
	assert.True(t, ShouldRetry(503, nil))
	assert.True(t, ShouldRetry(504, nil))
	assert.True(t, ShouldRetry(599, nil))
}

func TestShouldRetry_NetworkErrors(t *testing.T) {
	// Connection refused.
	connErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("connection refused"),
	}
	assert.True(t, ShouldRetry(0, connErr))

	// DNS error.
	dnsErr := &net.DNSError{
		Err:  "no such host",
		Name: "who-dat",
	}
	assert.True(t, ShouldRetry(0, dnsErr))

	// Timeout error via message.
	timeoutErr := fmt.Errorf("calling who-dat API: %w", &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("i/o timeout"),
	})
	assert.True(t, ShouldRetry(0, timeoutErr))
}

func TestShouldRetry_NonRetryable(t *testing.T) {
	// HTTP 404.
	assert.False(t, ShouldRetry(404, nil))

	// HTTP 400.
	assert.False(t, ShouldRetry(400, nil))

	// HTTP 200 (success, no retry needed).
	assert.False(t, ShouldRetry(200, nil))

	// Parse error (not a network issue).
	parseErr := errors.New("parsing who-dat response: invalid json")
	assert.False(t, ShouldRetry(0, parseErr))

	// No error, no status code.
	assert.False(t, ShouldRetry(0, nil))
}

func TestShouldRetry_WrappedNetworkError(t *testing.T) {
	wrappedErr := fmt.Errorf("calling who-dat API: %w", &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("connection refused"),
	})
	assert.True(t, ShouldRetry(0, wrappedErr))
}

func TestCheckWhoDatHealth_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	err := rw.CheckWhoDatHealth(context.Background())
	assert.NoError(t, err)
}

func TestCheckWhoDatHealth_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	err := rw.CheckWhoDatHealth(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 503")
}

func TestCheckWhoDatHealth_Unreachable(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "http://127.0.0.1:1", zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)
	rw.healthClient.Timeout = 1 * time.Second

	err := rw.CheckWhoDatHealth(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "who-dat health check failed")
}

func TestQueryDomainWithRetry_SuccessFirstAttempt(t *testing.T) {
	mockResponse := map[string]interface{}{
		"events": []map[string]interface{}{
			{"eventAction": "expiration", "eventDate": "2025-12-31T00:00:00Z"},
		},
		"entities":    []interface{}{},
		"nameservers": []interface{}{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	job := &WHOISQueryJob{
		DomainID:   1,
		DomainName: "example.com",
		Retries:    0,
	}

	result, err := rw.QueryDomainWithRetry(context.Background(), job)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.ExpirationDate)
}

func TestQueryDomainWithRetry_CircuitBreakerOpen(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "http://127.0.0.1:1", zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()

	// Force circuit open by recording 3 failures.
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	rw := NewRetryableWorker(worker, cb)

	job := &WHOISQueryJob{
		DomainID:   1,
		DomainName: "example.com",
		Retries:    0,
	}

	result, err := rw.QueryDomainWithRetry(context.Background(), job)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestQueryDomainWithRetry_RetryableError_ReEnqueues(t *testing.T) {
	// Server returns 429 (retryable).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	job := &WHOISQueryJob{
		DomainID:   1,
		DomainName: "example.com",
		Retries:    0,
		NextRetry:  0,
	}

	// Without Redis, pushJob will fail, but the retry logic should still increment retries.
	_, err := rw.QueryDomainWithRetry(context.Background(), job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "retryable error")
	// Job retries should be incremented.
	assert.Equal(t, 1, job.Retries)
	// NextRetry should be set to approximately now + 2s.
	assert.True(t, job.NextRetry > 0)
}

func TestQueryDomainWithRetry_MaxRetriesExhausted(t *testing.T) {
	// Server returns 500 (retryable).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	job := &WHOISQueryJob{
		DomainID:   1,
		DomainName: "example.com",
		Retries:    3, // Already at max.
		NextRetry:  0,
	}

	_, err := rw.QueryDomainWithRetry(context.Background(), job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries")
}

func TestQueryDomainWithRetry_NonRetryableError(t *testing.T) {
	// Server returns 404 (not retryable).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	job := &WHOISQueryJob{
		DomainID:   1,
		DomainName: "nonexistent.tld",
		Retries:    0,
	}

	_, err := rw.QueryDomainWithRetry(context.Background(), job)
	assert.Error(t, err)
	// Should NOT contain "retryable error" or "max retries".
	assert.NotContains(t, err.Error(), "retryable error")
	assert.NotContains(t, err.Error(), "max retries")
	// Job retries should remain at 0.
	assert.Equal(t, 0, job.Retries)
}

func TestExtractStatusCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "429 from error message",
			err:      fmt.Errorf("who-dat returned status %d: rate limited", 429),
			expected: 429,
		},
		{
			name:     "500 from error message",
			err:      fmt.Errorf("who-dat returned status %d: internal error", 500),
			expected: 500,
		},
		{
			name:     "503 from error message",
			err:      fmt.Errorf("who-dat returned status %d: service unavailable", 503),
			expected: 503,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: 0,
		},
		{
			name:     "no status code in message",
			err:      errors.New("some random error"),
			expected: 0,
		},
		{
			name:     "wrapped 429",
			err:      fmt.Errorf("query failed: who-dat returned status 429: rate limit"),
			expected: 429,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := extractStatusCode(tc.err)
			assert.Equal(t, tc.expected, code)
		})
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 2*time.Second, cfg.BaseDelay)
	assert.Equal(t, 2, cfg.Multiplier)
}

func TestNewRetryableWorker(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "http://who-dat:8080", zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	assert.NotNil(t, rw)
	assert.Equal(t, worker, rw.WHOISWorker)
	assert.Equal(t, cb, rw.circuitBreaker)
	assert.Equal(t, DefaultRetryConfig(), rw.retryConfig)
	assert.NotNil(t, rw.healthClient)
}

func TestStartHealthMonitor_StartupHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rw.StartHealthMonitor(ctx)

	// Give the goroutine a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Circuit should be closed (healthy).
	assert.Equal(t, circuitbreaker.StateClosed, cb.State())
	assert.Equal(t, 0, cb.FailureCount())
}

func TestStartHealthMonitor_StartupUnhealthy(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "http://127.0.0.1:1", zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)
	rw.healthClient.Timeout = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rw.StartHealthMonitor(ctx)

	// Give the startup check time to complete.
	time.Sleep(700 * time.Millisecond)

	// Should have recorded one failure.
	assert.Equal(t, 1, cb.FailureCount())
}

func TestCircuitBreakerIntegration_OpensAfter3Failures(t *testing.T) {
	// Server that always returns 503.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	worker := NewWHOISWorker(nil, nil, server.URL, zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	rw := NewRetryableWorker(worker, cb)

	// Simulate 3 consecutive health check failures.
	for i := 0; i < 3; i++ {
		err := rw.CheckWhoDatHealth(context.Background())
		assert.Error(t, err)
		cb.RecordFailure()
	}

	// Circuit should be open.
	assert.True(t, cb.IsOpen())

	// Queries should be blocked.
	job := &WHOISQueryJob{DomainID: 1, DomainName: "test.com"}
	_, err := rw.QueryDomainWithRetry(context.Background(), job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

func TestCircuitBreakerIntegration_ResumesOnSuccess(t *testing.T) {
	worker := NewWHOISWorker(nil, nil, "http://127.0.0.1:1", zap.NewNop())
	cb := circuitbreaker.NewWhoDatBreaker()
	_ = NewRetryableWorker(worker, cb)

	// Open the circuit.
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	assert.True(t, cb.IsOpen())

	// Record success to close it.
	cb.RecordSuccess()
	assert.False(t, cb.IsOpen())
	assert.Equal(t, circuitbreaker.StateClosed, cb.State())
}
