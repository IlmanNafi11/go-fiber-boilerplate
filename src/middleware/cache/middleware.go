package cache

import (
	"time"

	"app/src/redis"

	"github.com/gofiber/fiber/v2"
	fibercache "github.com/gofiber/fiber/v2/middleware/cache"
	redisstorage "github.com/gofiber/storage/redis/v3"
)

// NewResponseCacheMiddleware creates a Fiber cache middleware with Redis storage backend
// Returns nil if Redis client is unavailable (graceful degradation)
func NewResponseCacheMiddleware(redisClient *redis.RedisClient) fiber.Handler {
	// If Redis client is nil, disable caching gracefully
	if redisClient == nil {
		return nil
	}

	// Get underlying go-redis client from our RedisClient wrapper
	goRedisClient := redisClient.GetClient()

	// Create Redis storage backend
	store := redisstorage.NewFromConnection(goRedisClient)

	// Configure cache middleware
	config := fibercache.Config{
		// Next determines if we should skip caching for this request
		Next: func(c *fiber.Ctx) bool {
			method := c.Method()
			path := c.Path()

			// Skip write operations (POST, PUT, DELETE, PATCH)
			if method != fiber.MethodGet && method != fiber.MethodHead {
				return true
			}

			// Skip auth endpoints
			if shouldSkipCache(path) {
				return true
			}

			// Skip error responses (status code >= 400)
			if c.Response().StatusCode() >= 400 {
				return true
			}

			// Cache this request
			return false
		},

		// Expiration: 30 minutes (aligns with Phase 1 decision for API cache TTL)
		// Within the 5-30 minute range recommended for API response cache
		Expiration: 30 * time.Minute,

		// CacheHeader: X-Cache (shows hit/miss/unreachable status)
		CacheHeader: "X-Cache",

		// KeyGenerator: Use our custom key generator with path normalization and query sorting
		KeyGenerator: func(c *fiber.Ctx) string {
			return GenerateCacheKey(c.Method(), c.Path(), string(c.Request().URI().QueryString()))
		},

		// Storage: Redis backend
		Storage: store,

		// Methods: Only cache safe methods (GET, HEAD)
		Methods: []string{fiber.MethodGet, fiber.MethodHead},

		// CacheControl: Enable client-side caching headers
		CacheControl: true,
	}

	// Return cache middleware handler
	return fibercache.New(config)
}
