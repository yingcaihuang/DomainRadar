package sync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateSyncInterval_MoreThan90Days(t *testing.T) {
	// 91 days from now should return weekly (168h).
	expiresAt := time.Now().Add(91 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, WeeklyInterval, interval, "expected weekly interval for >90 days")
}

func TestCalculateSyncInterval_Exactly91Days(t *testing.T) {
	expiresAt := time.Now().Add(91 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, WeeklyInterval, interval)
}

func TestCalculateSyncInterval_Between30And90Days(t *testing.T) {
	// 60 days from now should return daily (24h).
	expiresAt := time.Now().Add(60 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, DailyInterval, interval, "expected daily interval for 30-90 days")
}

func TestCalculateSyncInterval_Exactly31Days(t *testing.T) {
	// 31 days is > 30, so should be daily.
	expiresAt := time.Now().Add(31 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, DailyInterval, interval)
}

func TestCalculateSyncInterval_LessThan30Days(t *testing.T) {
	// 15 days from now should return every 12h.
	expiresAt := time.Now().Add(15 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, TwelveHInterval, interval, "expected 12h interval for <30 days")
}

func TestCalculateSyncInterval_Exactly30Days(t *testing.T) {
	// Exactly 30 days is not > 30, so should be 12h.
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, TwelveHInterval, interval)
}

func TestCalculateSyncInterval_AlreadyExpired(t *testing.T) {
	// Past expiration should return 12h (most frequent tier).
	expiresAt := time.Now().Add(-5 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, TwelveHInterval, interval, "expected 12h interval for expired domains")
}

func TestCalculateSyncInterval_VeryFarFuture(t *testing.T) {
	// 365 days from now should return weekly.
	expiresAt := time.Now().Add(365 * 24 * time.Hour)
	interval := CalculateSyncInterval(expiresAt)
	assert.Equal(t, WeeklyInterval, interval)
}

func TestClampInterval_WithinBounds(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"1 hour (min)", 1 * time.Hour, 1 * time.Hour},
		{"12 hours", 12 * time.Hour, 12 * time.Hour},
		{"24 hours", 24 * time.Hour, 24 * time.Hour},
		{"168 hours (weekly)", 168 * time.Hour, 168 * time.Hour},
		{"720 hours (max)", 720 * time.Hour, 720 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClampInterval(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClampInterval_BelowMinimum(t *testing.T) {
	tests := []struct {
		name  string
		input time.Duration
	}{
		{"zero", 0},
		{"30 minutes", 30 * time.Minute},
		{"59 minutes", 59 * time.Minute},
		{"negative", -1 * time.Hour},
		{"1 second", 1 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClampInterval(tt.input)
			assert.Equal(t, MinInterval, result, "expected clamping to min (1 hour)")
		})
	}
}

func TestClampInterval_AboveMaximum(t *testing.T) {
	tests := []struct {
		name  string
		input time.Duration
	}{
		{"721 hours", 721 * time.Hour},
		{"31 days", 31 * 24 * time.Hour},
		{"365 days", 365 * 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClampInterval(tt.input)
			assert.Equal(t, MaxInterval, result, "expected clamping to max (720 hours)")
		})
	}
}

func TestClampInterval_BoundaryValues(t *testing.T) {
	// Exactly min should not be clamped.
	assert.Equal(t, MinInterval, ClampInterval(MinInterval))
	// Exactly max should not be clamped.
	assert.Equal(t, MaxInterval, ClampInterval(MaxInterval))
	// Just below min.
	assert.Equal(t, MinInterval, ClampInterval(MinInterval-1*time.Nanosecond))
	// Just above max.
	assert.Equal(t, MaxInterval, ClampInterval(MaxInterval+1*time.Nanosecond))
}
