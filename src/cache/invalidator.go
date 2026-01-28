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
// This includes session cache and user list and specific user GET/HEAD responses
func (ci *CacheInvalidator) InvalidateUserRelatedCache(ctx context.Context, userID string) error {
	if ci == nil {
		return nil
	}

	// Log comprehensive invalidation for user
	logrus.Infof("Invalidating all cache for user %s (session + API response)", userID)

	// Invalidate session cache
	if err := ci.InvalidateSessionCache(ctx, userID); err != nil {
		logrus.Warnf("Failed to invalidate session cache for user %s: %v", userID, err)
	}

	// Invalidate API response cache
	// Use existing GetAPIResponseKeyPattern(userID)
	apiPattern := GetAPIResponseKeyPattern(userID) // "api:response:*:user:{userID}:*"

	if err := ci.InvalidateByPattern(ctx, apiPattern); err != nil {
		logrus.Warnf("Failed to invalidate API response cache for user %s: %v", userID, err)
	}

	return nil
}

// InvalidateSessionCache invalidates session cache for user
// Uses pattern-based deletion with SCAN for session:user:{userID}
func (ci *CacheInvalidator) InvalidateSessionCache(ctx context.Context, userID string) error {
	if ci == nil {
		return nil // No invalidation if Redis unavailable
	}

	// Build session cache pattern
	sessionPattern := GetSessionPattern(userID) // "session:user:{userID}"

	// Log invalidation attempt
	logrus.Infof("Invalidating session cache for user %s (pattern: %s)", userID, sessionPattern)

	// Use existing InvalidateByPattern (SCAN-based deletion)
	return ci.InvalidateByPattern(ctx, sessionPattern)
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
