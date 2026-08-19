package circuitbreaker

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test-service", 5, 30*time.Second)

	assert.Equal(t, "test-service", cb.Name())
	assert.Equal(t, StateClosed, cb.State())
	assert.Equal(t, 0, cb.FailureCount())
	assert.False(t, cb.IsOpen())
}

func TestState_String(t *testing.T) {
	assert.Equal(t, "CLOSED", StateClosed.String())
	assert.Equal(t, "OPEN", StateOpen.String())
	assert.Equal(t, "HALF_OPEN", StateHalfOpen.String())
	assert.Equal(t, "UNKNOWN", State(99).String())
}

func TestCircuitBreaker_ClosedState_PassesThrough(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)

	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	testErr := errors.New("service error")

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error { return testErr })
		assert.ErrorIs(t, err, testErr)
	}

	assert.Equal(t, StateOpen, cb.State())
	assert.True(t, cb.IsOpen())
}

func TestCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Minute)
	testErr := errors.New("service error")

	// Trip the breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}

	// Next call should be rejected without calling fn
	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})

	assert.ErrorIs(t, err, ErrCircuitOpen)
	assert.False(t, called)
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterCooldown(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 5*time.Second)
	testErr := errors.New("service error")

	// Trip the breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, StateOpen, cb.State())

	// Simulate time passing beyond cooldown
	mockTime := time.Now().Add(6 * time.Second)
	cb.now = func() time.Time { return mockTime }

	// Next request should be allowed (half-open)
	assert.False(t, cb.IsOpen())
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 5*time.Second)
	testErr := errors.New("service error")

	// Trip the breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}

	// Advance time past cooldown
	mockTime := time.Now().Add(6 * time.Second)
	cb.now = func() time.Time { return mockTime }

	// Execute successfully in half-open state
	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.State())
	assert.Equal(t, 0, cb.FailureCount())
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 5*time.Second)
	testErr := errors.New("service error")

	// Trip the breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}

	// Advance time past cooldown
	mockTime := time.Now().Add(6 * time.Second)
	cb.now = func() time.Time { return mockTime }

	// Fail in half-open state
	err := cb.Execute(func() error { return testErr })
	assert.ErrorIs(t, err, testErr)
	assert.Equal(t, StateOpen, cb.State())
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Minute)
	testErr := errors.New("service error")

	// Accumulate 2 failures (below threshold)
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })
	assert.Equal(t, 2, cb.FailureCount())

	// Success resets the counter
	cb.Execute(func() error { return nil })
	assert.Equal(t, 0, cb.FailureCount())
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Minute)
	testErr := errors.New("service error")

	// Trip the breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, StateOpen, cb.State())

	// Manual reset
	cb.Reset()
	assert.Equal(t, StateClosed, cb.State())
	assert.Equal(t, 0, cb.FailureCount())
}

func TestCircuitBreaker_ThreadSafety(t *testing.T) {
	cb := NewCircuitBreaker("test", 100, time.Minute)
	var wg sync.WaitGroup

	// Run 50 concurrent goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.Execute(func() error { return nil })
			cb.Execute(func() error { return errors.New("fail") })
			_ = cb.IsOpen()
			_ = cb.State()
			_ = cb.FailureCount()
		}()
	}

	wg.Wait()
	// Just verify it doesn't panic or deadlock
}

func TestNewWhoDatBreaker(t *testing.T) {
	cb := NewWhoDatBreaker()

	assert.Equal(t, "who-dat", cb.Name())
	assert.Equal(t, 3, cb.threshold)
	assert.Equal(t, 90*time.Second, cb.cooldown)
	assert.Equal(t, StateClosed, cb.State())
}

func TestNewRegistrarBreaker(t *testing.T) {
	cb := NewRegistrarBreaker()

	assert.Equal(t, "registrar", cb.Name())
	assert.Equal(t, 5, cb.threshold)
	assert.Equal(t, 5*time.Minute, cb.cooldown)
	assert.Equal(t, StateClosed, cb.State())
}

func TestNewNotificationBreaker(t *testing.T) {
	cb := NewNotificationBreaker()

	assert.Equal(t, "notification", cb.Name())
	assert.Equal(t, 3, cb.threshold)
	assert.Equal(t, 60*time.Second, cb.cooldown)
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_WhoDat_OpensAfter3Failures(t *testing.T) {
	cb := NewWhoDatBreaker()
	testErr := errors.New("who-dat unreachable")

	for i := 0; i < 3; i++ {
		cb.Execute(func() error { return testErr })
	}

	assert.Equal(t, StateOpen, cb.State())
	assert.True(t, cb.IsOpen())
}

func TestCircuitBreaker_Registrar_OpensAfter5Failures(t *testing.T) {
	cb := NewRegistrarBreaker()
	testErr := errors.New("API error")

	// 4 failures should not open
	for i := 0; i < 4; i++ {
		cb.Execute(func() error { return testErr })
	}
	assert.Equal(t, StateClosed, cb.State())

	// 5th failure opens it
	cb.Execute(func() error { return testErr })
	assert.Equal(t, StateOpen, cb.State())
}

func TestCircuitBreaker_WhoDat_RecoverAfterCooldown(t *testing.T) {
	cb := NewWhoDatBreaker()
	testErr := errors.New("who-dat unreachable")

	// Trip the breaker
	for i := 0; i < 3; i++ {
		cb.Execute(func() error { return testErr })
	}
	require.Equal(t, StateOpen, cb.State())

	// Simulate 90 seconds passing
	mockTime := time.Now().Add(91 * time.Second)
	cb.now = func() time.Time { return mockTime }

	// Should transition to half-open and allow a request
	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_StaysOpenBeforeCooldown(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 30*time.Second)
	testErr := errors.New("fail")

	// Trip the breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return testErr })
	}

	// Only 10 seconds pass (less than 30s cooldown)
	mockTime := time.Now().Add(10 * time.Second)
	cb.now = func() time.Time { return mockTime }

	// Should still be open
	assert.True(t, cb.IsOpen())
	err := cb.Execute(func() error { return nil })
	assert.ErrorIs(t, err, ErrCircuitOpen)
}
