package cache

import (
	"context"
	"fmt"

	"app/src/redis"

	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// CacheInvalidator handles cache invalidation operations
type CacheInvalidator struct {
	redisClient *goredis.Client
}

// NewCacheInvalidator creates a new cache invalidator
// Returns nil if redisClient is nil (no invalidation if Redis disabled)
func NewCacheInvalidator(redisClient *redis.RedisClient) *CacheInvalidator {
	if redisClient == nil {
		return nil
	}
	goRedisClient := redisClient.GetClient()
	return &CacheInvalidator{
		redisClient: goRedisClient,
	}
}

// InvalidateUserRelatedCache invalidates all user-related cache entries
// This includes user list and specific user GET/HEAD responses
func (ci *CacheInvalidator) InvalidateUserRelatedCache(ctx context.Context, userID string) error {
	if ci == nil || ci.redisClient == nil {
		return nil
	}

	// Build invalidation patterns using cache key format from keygen.go
	patterns := []string{
		// Invalidate user list (GET /v1/users)
		"api:response:GET:/users*",
		// Invalidate user list (HEAD /v1/users)
		"api:response:HEAD:/users*",
		// Invalidate specific user (GET /v1/users/:id)
		fmt.Sprintf("api:response:GET:/users/%s*", userID),
		// Invalidate specific user (HEAD /v1/users/:id)
		fmt.Sprintf("api:response:HEAD:/users/%s*", userID),
	}

	// Invalidate each pattern
	for _, pattern := range patterns {
		if err := ci.InvalidateByPattern(ctx, pattern); err != nil {
			logrus.Warnf("failed to invalidate cache pattern %s: %v", pattern, err)
			// Don't fail the operation - cache invalidation is best-effort
		}
	}

	return nil
}

// InvalidateByPattern deletes all cache keys matching the given pattern
// Uses SCAN instead of KEYS to avoid blocking Redis server in production
func (ci *CacheInvalidator) InvalidateByPattern(ctx context.Context, pattern string) error {
	if ci == nil || ci.redisClient == nil {
		return nil
	}

	// Use SCAN to find keys matching pattern (DO NOT use KEYS - it's blocking)
	iter := ci.redisClient.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	// Check for iteration errors
	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan iterator error: %w", err)
	}

	// Delete found keys
	if len(keys) > 0 {
		if err := ci.redisClient.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
		logrus.Debugf("Invalidated %d cache keys matching pattern: %s", len(keys), pattern)
	}

	return nil
}
