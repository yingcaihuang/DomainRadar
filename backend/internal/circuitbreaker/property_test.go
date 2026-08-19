package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 2.9**
// Property 20: who-dat circuit breaker state transitions
// Pauses after exactly 3 consecutive failures; resumes on first success after being paused.
func TestProperty20_WhoDatCircuitBreakerStateTransitions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cb := NewWhoDatBreaker()

		// Use a controllable clock starting at a fixed point
		baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		currentTime := baseTime
		cb.now = func() time.Time { return currentTime }

		// Generate a random sequence of operations: true = success, false = failure
		// Also generate time advances to test cooldown behavior
		type op struct {
			isSuccess    bool
			advanceSecs  int // time to advance before this operation
		}

		seqLen := rapid.IntRange(1, 50).Draw(t, "seqLen")
		ops := make([]op, seqLen)
		for i := range ops {
			ops[i] = op{
				isSuccess:   rapid.Bool().Draw(t, "success"),
				advanceSecs: rapid.IntRange(0, 200).Draw(t, "advanceSecs"),
			}
		}

		// Track expected state by simulating the circuit breaker logic
		consecutiveFailures := 0
		isOpen := false
		isHalfOpen := false
		var lastFailureTime time.Time

		testErr := errors.New("who-dat unreachable")
		cooldown := 90 * time.Second

		for _, o := range ops {
			// Advance time
			currentTime = currentTime.Add(time.Duration(o.advanceSecs) * time.Second)

			// Check if cooldown has elapsed while open
			if isOpen && currentTime.Sub(lastFailureTime) >= cooldown {
				isHalfOpen = true
				isOpen = false
			}

			if isOpen {
				// Circuit is open and cooldown hasn't elapsed - calls rejected
				err := cb.Execute(func() error {
					if o.isSuccess {
						return nil
					}
					return testErr
				})

				if err != ErrCircuitOpen {
					t.Fatalf("expected ErrCircuitOpen when circuit is open, got: %v", err)
				}
				// State unchanged
				continue
			}

			// Circuit is closed or half-open - call goes through
			var execErr error
			if o.isSuccess {
				execErr = cb.Execute(func() error { return nil })
			} else {
				execErr = cb.Execute(func() error { return testErr })
			}

			if o.isSuccess {
				// Success: circuit should close, failures reset
				if execErr != nil {
					t.Fatalf("expected no error on success, got: %v", execErr)
				}
				consecutiveFailures = 0
				isOpen = false
				isHalfOpen = false

				// Verify: circuit is closed after success
				if cb.State() != StateClosed {
					t.Fatalf("expected StateClosed after success, got: %s", cb.State())
				}
			} else {
				// Failure
				if !errors.Is(execErr, testErr) {
					t.Fatalf("expected testErr on failure, got: %v", execErr)
				}

				if isHalfOpen {
					// Any failure in half-open goes back to open
					isOpen = true
					isHalfOpen = false
					lastFailureTime = currentTime
					// failure count increments but state goes to open immediately
					consecutiveFailures++

					if cb.State() != StateOpen {
						t.Fatalf("expected StateOpen after failure in half-open, got: %s", cb.State())
					}
				} else {
					// Closed state failure
					consecutiveFailures++
					lastFailureTime = currentTime

					if consecutiveFailures >= 3 {
						// Should open after exactly 3 consecutive failures
						isOpen = true
						if cb.State() != StateOpen {
							t.Fatalf("expected StateOpen after %d consecutive failures, got: %s",
								consecutiveFailures, cb.State())
						}
					} else {
						// Below threshold - should stay closed
						if cb.State() != StateClosed {
							t.Fatalf("expected StateClosed with only %d failures (threshold=3), got: %s",
								consecutiveFailures, cb.State())
						}
					}
				}
			}
		}
	})
}

// TestProperty20_ExactThreshold verifies the circuit does NOT open at 2 failures
// and DOES open at exactly 3 consecutive failures.
func TestProperty20_ExactThreshold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cb := NewWhoDatBreaker()

		baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		cb.now = func() time.Time { return baseTime }

		testErr := errors.New("who-dat unreachable")

		// Generate number of successes to intersperse (resets counter)
		numResets := rapid.IntRange(0, 5).Draw(t, "numResets")

		for i := 0; i < numResets; i++ {
			// Some failures below threshold
			failCount := rapid.IntRange(1, 2).Draw(t, "failsBefore")
			for j := 0; j < failCount; j++ {
				cb.Execute(func() error { return testErr })
			}
			// Verify NOT open after fewer than 3 consecutive failures
			if cb.State() != StateClosed {
				t.Fatalf("circuit opened with only %d consecutive failures", failCount)
			}
			// Reset with a success
			cb.Execute(func() error { return nil })
			if cb.State() != StateClosed {
				t.Fatal("expected StateClosed after success")
			}
			if cb.FailureCount() != 0 {
				t.Fatalf("expected failure count 0 after success, got %d", cb.FailureCount())
			}
		}

		// Now cause exactly 3 consecutive failures
		for i := 0; i < 2; i++ {
			cb.Execute(func() error { return testErr })
			if cb.State() != StateClosed {
				t.Fatalf("circuit opened prematurely at %d failures", i+1)
			}
		}
		// Third failure should open the circuit
		cb.Execute(func() error { return testErr })
		if cb.State() != StateOpen {
			t.Fatalf("expected StateOpen after exactly 3 consecutive failures, got: %s", cb.State())
		}
	})
}

// TestProperty20_ResumeAfterCooldownAndSuccess verifies that after being paused (open),
// the circuit resumes on the first success after cooldown elapses.
func TestProperty20_ResumeAfterCooldownAndSuccess(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cb := NewWhoDatBreaker()

		baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		currentTime := baseTime
		cb.now = func() time.Time { return currentTime }

		testErr := errors.New("who-dat unreachable")

		// Trip the breaker with 3 failures
		for i := 0; i < 3; i++ {
			cb.Execute(func() error { return testErr })
		}
		if cb.State() != StateOpen {
			t.Fatal("expected StateOpen after 3 failures")
		}

		// Advance time by random amount past cooldown (90s)
		advancePastCooldown := rapid.IntRange(91, 300).Draw(t, "advancePastCooldown")
		currentTime = currentTime.Add(time.Duration(advancePastCooldown) * time.Second)

		// Execute a successful call - should resume (close) the circuit
		err := cb.Execute(func() error { return nil })
		if err != nil {
			t.Fatalf("expected successful execution after cooldown, got: %v", err)
		}
		if cb.State() != StateClosed {
			t.Fatalf("expected StateClosed after success post-cooldown, got: %s", cb.State())
		}
		if cb.FailureCount() != 0 {
			t.Fatalf("expected failure count reset to 0, got: %d", cb.FailureCount())
		}
	})
}

// TestProperty20_StaysOpenBeforeCooldown verifies the circuit stays open
// before the cooldown elapses regardless of what happens.
func TestProperty20_StaysOpenBeforeCooldown(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cb := NewWhoDatBreaker()

		baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		currentTime := baseTime
		cb.now = func() time.Time { return currentTime }

		testErr := errors.New("who-dat unreachable")

		// Trip the breaker
		for i := 0; i < 3; i++ {
			cb.Execute(func() error { return testErr })
		}

		// Advance time by less than cooldown (< 90s)
		advanceBeforeCooldown := rapid.IntRange(0, 89).Draw(t, "advanceBeforeCooldown")
		currentTime = currentTime.Add(time.Duration(advanceBeforeCooldown) * time.Second)

		// Any attempt should be rejected
		err := cb.Execute(func() error { return nil })
		if !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("expected ErrCircuitOpen before cooldown elapses, got: %v", err)
		}
	})
}
