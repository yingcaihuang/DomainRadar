package cache

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewCacheService_EmptyURL(t *testing.T) {
	_, err := NewCacheService("", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis URL is required")
}

func TestNewCacheService_InvalidURL(t *testing.T) {
	_, err := NewCacheService("not-a-valid-url", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse redis URL")
}

func TestNewCacheService_ValidURL(t *testing.T) {
	svc, err := NewCacheService("redis://localhost:6379/0", nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.NotNil(t, svc.client)
	assert.NotNil(t, svc.logger)
	_ = svc.Close()
}

func TestNewCacheService_WithLogger(t *testing.T) {
	logger := zaptest.NewLogger(t)
	svc, err := NewCacheService("redis://localhost:6379/0", logger)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.Equal(t, logger, svc.logger)
	_ = svc.Close()
}

func TestNewCacheServiceFromEnv_Default(t *testing.T) {
	// Unset REDIS_URL to use the default
	t.Setenv("REDIS_URL", "")
	svc, err := NewCacheServiceFromEnv(nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	_ = svc.Close()
}

func TestNewCacheServiceFromEnv_CustomURL(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://customhost:6380/2")
	svc, err := NewCacheServiceFromEnv(nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	_ = svc.Close()
}

// TestGracefulDegradation_Get verifies that Get returns empty string + nil error
// when Redis is unreachable (graceful degradation).
func TestGracefulDegradation_Get(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	val, err := svc.Get(context.Background(), "some-key")
	assert.NoError(t, err)
	assert.Equal(t, "", val)
}

// TestGracefulDegradation_Set verifies that Set returns nil error
// when Redis is unreachable (graceful degradation).
func TestGracefulDegradation_Set(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	err := svc.Set(context.Background(), "some-key", "some-value", time.Minute)
	assert.NoError(t, err)
}

// TestGracefulDegradation_Delete verifies that Delete returns nil error
// when Redis is unreachable (graceful degradation).
func TestGracefulDegradation_Delete(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	err := svc.Delete(context.Background(), "some-key")
	assert.NoError(t, err)
}

// TestGracefulDegradation_GetJSON verifies that GetJSON returns nil error
// when Redis is unreachable (graceful degradation).
func TestGracefulDegradation_GetJSON(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	var dest map[string]string
	err := svc.GetJSON(context.Background(), "some-key", &dest)
	assert.NoError(t, err)
	assert.Nil(t, dest) // dest is left unchanged
}

// TestGracefulDegradation_SetJSON verifies that SetJSON returns nil error
// when Redis is unreachable (graceful degradation).
func TestGracefulDegradation_SetJSON(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	err := svc.SetJSON(context.Background(), "some-key", map[string]string{"a": "b"}, time.Minute)
	assert.NoError(t, err)
}

// TestGracefulDegradation_Ping verifies that Ping returns an error
// when Redis is unreachable (Ping does NOT degrade gracefully).
func TestGracefulDegradation_Ping(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	err := svc.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis ping failed")
}

func TestSetJSON_MarshalError(t *testing.T) {
	svc := newUnreachableService(t)
	defer svc.Close()

	// Channels cannot be marshaled to JSON
	err := svc.SetJSON(context.Background(), "bad-key", make(chan int), time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal value to JSON")
}

func TestGetJSON_UnmarshalError(t *testing.T) {
	// Create a service with a real client that we manually seed with invalid JSON
	logger := zaptest.NewLogger(t)
	svc := &CacheService{
		client: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		}),
		logger: logger,
	}
	defer svc.Close()

	ctx := context.Background()

	// Try to ping - if Redis is not available, skip this test
	if err := svc.client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping unmarshal error test")
	}

	// Set an invalid JSON value directly
	key := "test-invalid-json-" + time.Now().Format("20060102150405")
	svc.client.Set(ctx, key, "not-valid-json{{{", time.Minute)
	defer svc.client.Del(ctx, key)

	var dest map[string]string
	err := svc.GetJSON(ctx, key, &dest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal cached JSON")
}

// newUnreachableService creates a CacheService pointing to an unreachable Redis
// instance for testing graceful degradation. Uses a short dial timeout so tests
// don't hang.
func newUnreachableService(t *testing.T) *CacheService {
	t.Helper()
	logger := zap.NewNop()

	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:1", // Unreachable port
		DialTimeout: 100 * time.Millisecond,
		ReadTimeout: 100 * time.Millisecond,
	})

	return &CacheService{
		client: client,
		logger: logger,
	}
}
