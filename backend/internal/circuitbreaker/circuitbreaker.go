package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// State represents the current state of the circuit breaker.
type State int

const (
	StateClosed   State = iota // Normal operation, requests pass through
	StateOpen                  // Failing, requests are rejected
	StateHalfOpen              // Testing, one request allowed through
)

// String returns a human-readable state name.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is in the open state.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker implements the circuit breaker pattern for protecting
// calls to external services.
type CircuitBreaker struct {
	mu              sync.Mutex
	name            string
	state           State
	failureCount    int
	threshold       int
	cooldown        time.Duration
	lastFailureTime time.Time
	now             func() time.Time // injectable clock for testing
}

// NewCircuitBreaker creates a new CircuitBreaker with the given configuration.
// - name: identifier for this breaker (used in logging/metrics)
// - threshold: number of consecutive failures before opening
// - cooldown: duration to wait in open state before transitioning to half-open
func NewCircuitBreaker(name string, threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:      name,
		state:     StateClosed,
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

// Execute wraps a function call with circuit breaker protection.
// If the circuit is open, it returns ErrCircuitOpen without calling fn.
// If the circuit is half-open, it allows one call through and transitions
// based on the result.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()
	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// allowRequest determines whether a request should be allowed through.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if cooldown has elapsed
		if cb.now().Sub(cb.lastFailureTime) >= cb.cooldown {
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful call and resets the circuit breaker.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0
	cb.state = StateClosed
}

// RecordFailure records a failed call and potentially opens the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = cb.now()

	if cb.state == StateHalfOpen {
		// Any failure in half-open goes back to open
		cb.state = StateOpen
		return
	}

	if cb.failureCount >= cb.threshold {
		cb.state = StateOpen
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// IsOpen returns true if the circuit breaker is in the open state.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateOpen {
		// Check if it should transition to half-open
		if cb.now().Sub(cb.lastFailureTime) >= cb.cooldown {
			cb.state = StateHalfOpen
			return false
		}
		return true
	}
	return false
}

// Name returns the circuit breaker's name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// FailureCount returns the current consecutive failure count.
func (cb *CircuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}

// Reset manually resets the circuit breaker to the closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failureCount = 0
	cb.lastFailureTime = time.Time{}
}

// Predefined circuit breaker configurations for DomainRadar services.

// NewWhoDatBreaker creates a circuit breaker for the who-dat WHOIS/RDAP service.
// 3 consecutive failures, 90 second cooldown.
func NewWhoDatBreaker() *CircuitBreaker {
	return NewCircuitBreaker("who-dat", 3, 90*time.Second)
}

// NewRegistrarBreaker creates a circuit breaker for registrar API calls.
// 5 consecutive failures, 5 minute cooldown.
func NewRegistrarBreaker() *CircuitBreaker {
	return NewCircuitBreaker("registrar", 5, 5*time.Minute)
}

// NewNotificationBreaker creates a circuit breaker for notification channels.
// 3 consecutive failures, 60 second cooldown.
func NewNotificationBreaker() *CircuitBreaker {
	return NewCircuitBreaker("notification", 3, 60*time.Second)
}
