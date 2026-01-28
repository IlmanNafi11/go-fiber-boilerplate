package middleware

import (
	"app/src/config"
	"app/src/redis"
	"app/src/response"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	redisstorage "github.com/gofiber/storage/redis/v3"
	"github.com/sirupsen/logrus"
)

// NewRateLimiterMiddleware creates rate limiter middleware with Redis storage and sliding window algorithm
func NewRateLimiterMiddleware(redisClient *redis.RedisClient, rateLimitConfig *config.RateLimiterConfig) fiber.Handler {
	// RATE-05: Graceful degradation - return nil if Redis unavailable
	if redisClient == nil || rateLimitConfig == nil || !rateLimitConfig.Enabled {
		logrus.Info("Rate limiter disabled (Redis unavailable or disabled)")
		return nil
	}

	// Create Redis storage from existing client
	// Reuse Phase 1's Redis client - DO NOT create new connection
	store := redisstorage.NewFromConnection(redisClient.GetClient())

	// Use the higher max and larger window to accommodate both authenticated and unauthenticated users
	// Fiber v2 doesn't support dynamic MaxFunc/ExpirationFunc, so we use single configuration
	maxRequests := rateLimitConfig.AuthMax
	if rateLimitConfig.DefaultMax > maxRequests {
		maxRequests = rateLimitConfig.DefaultMax
	}

	windowDuration := rateLimitConfig.AuthWindow
	if rateLimitConfig.DefaultWindow > windowDuration {
		windowDuration = rateLimitConfig.DefaultWindow
	}

	// Configure rate limiter with sliding window
	return limiter.New(limiter.Config{
		// RATE-03: Use higher limit (supports both authenticated and unauthenticated)
		Max: maxRequests,
		// RATE-02: Use larger window (supports both authenticated and unauthenticated)
		Expiration: windowDuration,
		// RATE-01: Rate limiting per user/IP - differentiate in key generator
		KeyGenerator: func(c *fiber.Ctx) string {
			// Check for authenticated user first
			if userID := c.Locals("user_id"); userID != nil {
				return fmt.Sprintf("rate_limit:user:%v", userID)
			}
			// RATE-01: Check for proxy headers (X-Forwarded-For, CF-Connecting-IP)
			if forwardedFor := c.Get("X-Forwarded-For"); forwardedFor != "" {
				return fmt.Sprintf("rate_limit:ip:%s", forwardedFor)
			}
			if cfIP := c.Get("CF-Connecting-IP"); cfIP != "" {
				return fmt.Sprintf("rate_limit:ip:%s", cfIP)
			}
			// Fallback to connection IP
			return fmt.Sprintf("rate_limit:ip:%s", c.IP())
		},
		LimitReached: func(c *fiber.Ctx) error {
			// RATE-04: Return 429 Too Many Requests
			// Fiber automatically sets Retry-After header based on Expiration
			return c.Status(fiber.StatusTooManyRequests).
				JSON(response.Common{
					Code:    fiber.StatusTooManyRequests,
					Status:  "error",
					Message: "Too many requests. Please try again later.",
				})
		},
		Storage:                store,                   // RATE-01: Redis storage backend
		LimiterMiddleware:      limiter.SlidingWindow{}, // RATE-02: Sliding window algorithm
		SkipSuccessfulRequests: true,                    // Don't count successful requests towards limit
	})
}
