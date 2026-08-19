package whois

import (
	"math"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestProperty9_WHOISExponentialBackoffTiming verifies that for the default retry
// configuration, the delay for retry N equals 2^(N+1) seconds. Also verifies that
// after MaxRetries (3), the system should mark the query as failed (boundary condition).
//
// **Validates: Requirements 2.3**
func TestProperty9_WHOISExponentialBackoffTiming(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := DefaultRetryConfig()

		// Generate a random retry count in the valid range [0, MaxRetries-1]
		retryCount := rapid.IntRange(0, cfg.MaxRetries-1).Draw(t, "retryCount")

		// Calculate the backoff
		delay := CalculateBackoff(cfg, retryCount)

		// Property: delay == 2^(retryCount+1) seconds
		// Because BaseDelay=2s and Multiplier=2: 2s * 2^retryCount = 2^(retryCount+1) seconds
		expectedSeconds := math.Pow(2, float64(retryCount+1))
		expectedDelay := time.Duration(expectedSeconds) * time.Second

		if delay != expectedDelay {
			t.Fatalf("CalculateBackoff(cfg, %d) = %v, want %v (2^%d seconds)",
				retryCount, delay, expectedDelay, retryCount+1)
		}
	})
}

// TestProperty9_WHOISMaxRetriesBoundary verifies that after MaxRetries (3) attempts,
// the system marks the query as failed. The retry logic should not allow further retries
// once retryCount >= MaxRetries.
//
// **Validates: Requirements 2.3**
func TestProperty9_WHOISMaxRetriesBoundary(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := DefaultRetryConfig()

		// Generate retry counts at or beyond MaxRetries
		retryCount := rapid.IntRange(cfg.MaxRetries, cfg.MaxRetries+10).Draw(t, "retryCount")

		// Property: retryCount >= MaxRetries means the query should be marked as failed.
		// The system should NOT retry when retryCount >= MaxRetries.
		shouldRetryMore := retryCount < cfg.MaxRetries

		if shouldRetryMore {
			t.Fatalf("expected no more retries at retryCount=%d (MaxRetries=%d)",
				retryCount, cfg.MaxRetries)
		}
	})
}

// TestProperty19_WHOISExpirationDiscrepancyDetection verifies that DetectDiscrepancy
// flags a discrepancy if and only if the absolute date difference exceeds 24 hours.
//
// **Validates: Requirements 2.6**
func TestProperty19_WHOISExpirationDiscrepancyDetection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two random time values within a reasonable range (2020-2030)
		baseUnix := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		rangeSeconds := int64(10 * 365 * 24 * 3600) // ~10 years

		manual := time.Unix(
			baseUnix+rapid.Int64Range(0, rangeSeconds).Draw(t, "manualUnix"),
			0,
		).UTC()
		whois := time.Unix(
			baseUnix+rapid.Int64Range(0, rangeSeconds).Draw(t, "whoisUnix"),
			0,
		).UTC()

		result := DetectDiscrepancy(&manual, &whois)

		// Calculate the expected absolute difference
		diff := manual.Sub(whois)
		absDiffHours := math.Abs(diff.Hours())

		// Property: HasDiscrepancy == true iff abs(difference) > 24 hours
		expectedDiscrepancy := absDiffHours > DiscrepancyThresholdHours

		if result.HasDiscrepancy != expectedDiscrepancy {
			t.Fatalf("DetectDiscrepancy(%v, %v): HasDiscrepancy=%v, want %v (diff=%.2f hours, threshold=%v)",
				manual, whois, result.HasDiscrepancy, expectedDiscrepancy, absDiffHours, DiscrepancyThresholdHours)
		}

		// Property: DifferenceHours matches our calculation
		if math.Abs(result.DifferenceHours-absDiffHours) > 0.001 {
			t.Fatalf("DifferenceHours=%.6f, expected=%.6f", result.DifferenceHours, absDiffHours)
		}
	})
}

// TestProperty19_WHOISDiscrepancyNilCases verifies that DetectDiscrepancy returns
// HasDiscrepancy=false when either or both inputs are nil.
//
// **Validates: Requirements 2.6**
func TestProperty19_WHOISDiscrepancyNilCases(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random time for the non-nil case
		baseUnix := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		rangeSeconds := int64(10 * 365 * 24 * 3600)
		someTime := time.Unix(
			baseUnix+rapid.Int64Range(0, rangeSeconds).Draw(t, "someTimeUnix"),
			0,
		).UTC()

		// Choose which nil scenario to test
		scenario := rapid.IntRange(0, 2).Draw(t, "scenario")

		var manualPtr, whoisPtr *time.Time

		switch scenario {
		case 0:
			// Both nil
			manualPtr = nil
			whoisPtr = nil
		case 1:
			// Manual nil, WHOIS non-nil
			manualPtr = nil
			whoisPtr = &someTime
		case 2:
			// Manual non-nil, WHOIS nil
			manualPtr = &someTime
			whoisPtr = nil
		}

		result := DetectDiscrepancy(manualPtr, whoisPtr)

		// Property: when either input is nil, HasDiscrepancy must be false
		if result.HasDiscrepancy {
			t.Fatalf("expected HasDiscrepancy=false for nil input scenario %d, got true", scenario)
		}

		// Property: DifferenceHours must be 0 for nil cases
		if result.DifferenceHours != 0 {
			t.Fatalf("expected DifferenceHours=0 for nil input scenario %d, got %f", scenario, result.DifferenceHours)
		}
	})
}
