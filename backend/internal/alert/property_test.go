package alert

import (
	"testing"

	"pgregory.net/rapid"
)

// validSeverities is the set of valid severity strings returned by CalculateSeverity.
var validSeverities = map[string]bool{
	SeverityExpired:       true,
	SeverityCritical:      true,
	SeverityWarning:       true,
	SeverityInformational: true,
}

// TestProperty5_AlertSeverityAssignment verifies that CalculateSeverity returns the
// correct severity for all (daysRemaining, autoRenew) combinations:
//   - daysRemaining < 0: always "expired" regardless of autoRenew
//   - daysRemaining 0-7: always "critical" regardless of autoRenew
//   - daysRemaining 8-30 with autoRenew=true: "warning"
//   - daysRemaining 8-30 with autoRenew=false: "critical" (escalated)
//   - daysRemaining 31+: always "informational" regardless of autoRenew
//
// **Validates: Requirements 4.1, 4.2, 4.3, 4.4**
func TestProperty5_AlertSeverityAssignment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		daysRemaining := rapid.IntRange(-365, 365).Draw(t, "daysRemaining")
		autoRenew := rapid.Bool().Draw(t, "autoRenew")

		result := CalculateSeverity(daysRemaining, autoRenew)

		// Property: result is always one of the valid severity values
		if !validSeverities[result] {
			t.Fatalf("CalculateSeverity(%d, %v) returned invalid severity %q", daysRemaining, autoRenew, result)
		}

		// Property: correct severity for each range
		switch {
		case daysRemaining < 0:
			// Always expired regardless of autoRenew
			if result != SeverityExpired {
				t.Fatalf("CalculateSeverity(%d, %v) = %q, want %q (expired for negative days)",
					daysRemaining, autoRenew, result, SeverityExpired)
			}

		case daysRemaining <= 7:
			// Always critical regardless of autoRenew (0-7 days)
			if result != SeverityCritical {
				t.Fatalf("CalculateSeverity(%d, %v) = %q, want %q (critical for 0-7 days)",
					daysRemaining, autoRenew, result, SeverityCritical)
			}

		case daysRemaining <= 30:
			// 8-30 days: depends on autoRenew
			if autoRenew {
				if result != SeverityWarning {
					t.Fatalf("CalculateSeverity(%d, %v) = %q, want %q (warning for 8-30 days with auto-renew)",
						daysRemaining, autoRenew, result, SeverityWarning)
				}
			} else {
				// Escalated from warning to critical when auto-renew disabled
				if result != SeverityCritical {
					t.Fatalf("CalculateSeverity(%d, %v) = %q, want %q (critical escalation for 8-30 days without auto-renew)",
						daysRemaining, autoRenew, result, SeverityCritical)
				}
			}

		default:
			// 31+ days: always informational regardless of autoRenew
			if result != SeverityInformational {
				t.Fatalf("CalculateSeverity(%d, %v) = %q, want %q (informational for 31+ days)",
					daysRemaining, autoRenew, result, SeverityInformational)
			}
		}
	})
}

// TestProperty6_AlertThresholdEvaluationCompleteness verifies that the severity string
// returned by CalculateSeverity is never empty and is always from the valid set of
// {"expired", "critical", "warning", "informational"}.
//
// **Validates: Requirements 4.1, 4.2, 4.3, 4.4**
func TestProperty6_AlertThresholdEvaluationCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		daysRemaining := rapid.IntRange(-365, 365).Draw(t, "daysRemaining")
		autoRenew := rapid.Bool().Draw(t, "autoRenew")

		result := CalculateSeverity(daysRemaining, autoRenew)

		// Property: severity is never empty
		if result == "" {
			t.Fatalf("CalculateSeverity(%d, %v) returned empty string", daysRemaining, autoRenew)
		}

		// Property: severity is from the valid set
		if !validSeverities[result] {
			t.Fatalf("CalculateSeverity(%d, %v) = %q, not in valid set %v",
				daysRemaining, autoRenew, result, validSeverities)
		}
	})
}
