package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// CacheService wraps a Redis client and provides caching operations
// with graceful degradation when Redis is unavailable.
type CacheService struct {
	client *redis.Client
	logger *zap.Logger
}

// NewCacheService creates a new CacheService by parsing the provided Redis URL.
// The redisURL should be in the format "redis://[:password@]host:port[/db]".
func NewCacheService(redisURL string, logger *zap.Logger) (*CacheService, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("redis URL is required")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	return &CacheService{
		client: client,
		logger: logger,
	}, nil
}

// NewCacheServiceFromEnv creates a CacheService using the REDIS_URL environment variable.
func NewCacheServiceFromEnv(logger *zap.Logger) (*CacheService, error) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}

	return NewCacheService(redisURL, logger)
}

// Get retrieves a value from the cache by key.
// Returns an empty string and nil error on cache miss or Redis unavailability.
func (s *CacheService) Get(ctx context.Context, key string) (string, error) {
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		s.logger.Warn("cache get failed, degrading gracefully",
			zap.String("key", key),
			zap.Error(err),
		)
		return "", nil
	}
	return val, nil
}

// Set stores a value in the cache with the specified TTL.
// If TTL is 0, the key will not expire.
// Returns nil on Redis unavailability (graceful degradation).
func (s *CacheService) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	err := s.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		s.logger.Warn("cache set failed, degrading gracefully",
			zap.String("key", key),
			zap.Error(err),
		)
		return nil
	}
	return nil
}

// Delete removes a key from the cache.
// Returns nil on Redis unavailability (graceful degradation).
func (s *CacheService) Delete(ctx context.Context, key string) error {
	err := s.client.Del(ctx, key).Err()
	if err != nil {
		s.logger.Warn("cache delete failed, degrading gracefully",
			zap.String("key", key),
			zap.Error(err),
		)
		return nil
	}
	return nil
}

// GetJSON retrieves a JSON value from the cache and unmarshals it into dest.
// Returns nil on cache miss or Redis unavailability (dest is left unchanged).
func (s *CacheService) GetJSON(ctx context.Context, key string, dest interface{}) error {
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		s.logger.Warn("cache get (JSON) failed, degrading gracefully",
			zap.String("key", key),
			zap.Error(err),
		)
		return nil
	}

	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return fmt.Errorf("failed to unmarshal cached JSON for key %q: %w", key, err)
	}
	return nil
}

// SetJSON marshals value to JSON and stores it in the cache with the specified TTL.
// Returns nil on Redis unavailability (graceful degradation).
func (s *CacheService) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value to JSON for key %q: %w", key, err)
	}

	err = s.client.Set(ctx, key, string(data), ttl).Err()
	if err != nil {
		s.logger.Warn("cache set (JSON) failed, degrading gracefully",
			zap.String("key", key),
			zap.Error(err),
		)
		return nil
	}
	return nil
}

// Ping checks Redis connectivity. Returns an error if Redis is unreachable.
func (s *CacheService) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

// Close closes the underlying Redis connection.
func (s *CacheService) Close() error {
	return s.client.Close()
}
