package router

import (
	"app/src/cache"
	"app/src/config"
	middlewareCache "app/src/middleware/cache"
	"app/src/redis"
	"app/src/service"
	"app/src/validation"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func Routes(app *fiber.App, db *gorm.DB) {
	validate := validation.Validator()

	// Initialize Redis client
	redisConfig, err := config.LoadRedisConfig()
	if err != nil {
		logrus.Errorf("Failed to load Redis config: %v", err)
	}

	var redisClient *redis.RedisClient
	if redisConfig != nil && redisConfig.Enabled {
		redisClient, err = redis.NewRedisClient(*redisConfig)
		if err != nil {
			logrus.Errorf("Failed to initialize Redis client: %v", err)
			// Redis disabled, continue without it
		}

		// Initialize and start health monitor
		if redisClient != nil {
			healthMonitor := redis.InitHealthMonitor(30*time.Second, func(available bool) {
				// State change callback - can be used for cache warm-up in Phase 2-6
				if available {
					logrus.Info("Redis state changed to available")
				} else {
					logrus.Warn("Redis state changed to unavailable")
				}
			})
			if healthMonitor != nil {
				redis.StartHealthMonitor()
				logrus.Info("Redis health monitor started")
			}

			logrus.Info("Redis client initialized successfully")
		}
	} else {
		logrus.Info("Redis disabled or not configured")
	}

	healthCheckService := service.NewHealthCheckService(db, redis.GetHealthMonitor())
	emailService := service.NewEmailService()

	// Initialize session service
	var sessionService service.SessionService
	if redisClient != nil {
		sessionService = service.NewSessionService(redisClient)
		logrus.Info("Session service initialized")
	} else {
		logrus.Warn("Session service disabled (Redis unavailable)")
	}

	// Initialize cache invalidator
	var cacheInvalidator *cache.CacheInvalidator
	if redisClient != nil {
		cacheInvalidator = cache.NewCacheInvalidator(redisClient)
		if cacheInvalidator != nil {
			logrus.Info("Cache invalidator initialized")
		}
	} else {
		logrus.Info("Cache invalidator disabled (Redis unavailable)")
	}

	userService := service.NewUserService(db, validate, sessionService, cacheInvalidator)
	tokenService := service.NewTokenService(db, validate, userService, sessionService)
	authService := service.NewAuthService(db, validate, userService, tokenService, cacheInvalidator)

	// Initialize cache middleware
	var cacheMiddleware fiber.Handler
	if redisClient != nil {
		cacheMiddleware = middlewareCache.NewResponseCacheMiddleware(redisClient)
		if cacheMiddleware != nil {
			logrus.Info("Cache middleware initialized")
		}
	} else {
		logrus.Info("Cache middleware disabled (Redis unavailable)")
	}

	v1 := app.Group("/v1")

	// Apply cache middleware to all routes
	// The middleware's Next() function will skip auth endpoints and write operations automatically
	if cacheMiddleware != nil {
		app.Use(cacheMiddleware)
	}

	HealthCheckRoutes(v1, healthCheckService)
	AuthRoutes(v1, authService, userService, tokenService, emailService, sessionService)
	UserRoutes(v1, userService, tokenService, sessionService)
	// TODO: add another routes here...

	if !config.IsProd {
		DocsRoutes(v1)
	}
}
