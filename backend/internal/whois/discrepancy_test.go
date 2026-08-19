package whois

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestDetectDiscrepancy_BothNil(t *testing.T) {
	result := DetectDiscrepancy(nil, nil)

	require.NotNil(t, result)
	assert.False(t, result.HasDiscrepancy)
	assert.Equal(t, 0.0, result.DifferenceHours)
	assert.Nil(t, result.ManualExpiry)
	assert.Nil(t, result.WhoisExpiry)
}

func TestDetectDiscrepancy_ManualNil(t *testing.T) {
	whois := time.Now()
	result := DetectDiscrepancy(nil, &whois)

	assert.False(t, result.HasDiscrepancy)
	assert.Equal(t, 0.0, result.DifferenceHours)
}

func TestDetectDiscrepancy_WhoisNil(t *testing.T) {
	manual := time.Now()
	result := DetectDiscrepancy(&manual, nil)

	assert.False(t, result.HasDiscrepancy)
	assert.Equal(t, 0.0, result.DifferenceHours)
}

func TestDetectDiscrepancy_NoDifference(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	result := DetectDiscrepancy(&now, &now)

	assert.False(t, result.HasDiscrepancy)
	assert.Equal(t, 0.0, result.DifferenceHours)
}

func TestDetectDiscrepancy_SmallDifference_NoFlag(t *testing.T) {
	manual := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	whois := time.Date(2025, 6, 15, 20, 0, 0, 0, time.UTC) // 8 hours later

	result := DetectDiscrepancy(&manual, &whois)

	assert.False(t, result.HasDiscrepancy)
	assert.InDelta(t, 8.0, result.DifferenceHours, 0.01)
}

func TestDetectDiscrepancy_Exactly24Hours_NoFlag(t *testing.T) {
	manual := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	whois := time.Date(2025, 6, 16, 0, 0, 0, 0, time.UTC) // exactly 24 hours

	result := DetectDiscrepancy(&manual, &whois)

	// Exactly 24 hours should NOT flag (requirement is > 24 hours).
	assert.False(t, result.HasDiscrepancy)
	assert.InDelta(t, 24.0, result.DifferenceHours, 0.01)
}

func TestDetectDiscrepancy_JustOver24Hours_Flags(t *testing.T) {
	manual := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	whois := time.Date(2025, 6, 16, 0, 1, 0, 0, time.UTC) // 24h + 1min

	result := DetectDiscrepancy(&manual, &whois)

	assert.True(t, result.HasDiscrepancy)
	assert.Greater(t, result.DifferenceHours, 24.0)
}

func TestDetectDiscrepancy_LargeDifference_ManualBefore(t *testing.T) {
	manual := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	whois := time.Date(2025, 7, 15, 0, 0, 0, 0, time.UTC) // 30 days later

	result := DetectDiscrepancy(&manual, &whois)

	assert.True(t, result.HasDiscrepancy)
	assert.InDelta(t, 720.0, result.DifferenceHours, 1.0) // ~30 days in hours
}

func TestDetectDiscrepancy_LargeDifference_ManualAfter(t *testing.T) {
	manual := time.Date(2025, 7, 15, 0, 0, 0, 0, time.UTC)
	whois := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC) // 30 days before

	result := DetectDiscrepancy(&manual, &whois)

	assert.True(t, result.HasDiscrepancy)
	assert.InDelta(t, 720.0, result.DifferenceHours, 1.0)
}

func TestDetectDiscrepancy_ReturnsCorrectPointers(t *testing.T) {
	manual := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	whois := time.Date(2025, 8, 15, 0, 0, 0, 0, time.UTC)

	result := DetectDiscrepancy(&manual, &whois)

	require.NotNil(t, result.ManualExpiry)
	require.NotNil(t, result.WhoisExpiry)
	assert.Equal(t, manual, *result.ManualExpiry)
	assert.Equal(t, whois, *result.WhoisExpiry)
}

func TestDetectDiscrepancy_AbsoluteDifference(t *testing.T) {
	// Ensure the function uses absolute difference regardless of direction.
	manual := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	whoisBefore := time.Date(2025, 6, 13, 0, 0, 0, 0, time.UTC) // 48h before
	whoisAfter := time.Date(2025, 6, 17, 0, 0, 0, 0, time.UTC)  // 48h after

	resultBefore := DetectDiscrepancy(&manual, &whoisBefore)
	resultAfter := DetectDiscrepancy(&manual, &whoisAfter)

	assert.True(t, resultBefore.HasDiscrepancy)
	assert.True(t, resultAfter.HasDiscrepancy)
	assert.InDelta(t, 48.0, resultBefore.DifferenceHours, 0.01)
	assert.InDelta(t, 48.0, resultAfter.DifferenceHours, 0.01)
}

func TestFormatTime_Nil(t *testing.T) {
	assert.Equal(t, "N/A", formatTime(nil))
}

func TestFormatTime_Valid(t *testing.T) {
	tm := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)
	assert.Equal(t, "2025-06-15 14:30:45 UTC", formatTime(&tm))
}

func TestFormatTime_NonUTC(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	tm := time.Date(2025, 6, 15, 14, 30, 45, 0, loc)
	// Should output in UTC.
	result := formatTime(&tm)
	assert.Contains(t, result, "UTC")
}
