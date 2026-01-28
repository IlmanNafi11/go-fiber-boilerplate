package router

import (
	"app/src/config"
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
	userService := service.NewUserService(db, validate)
	tokenService := service.NewTokenService(db, validate, userService)
	authService := service.NewAuthService(db, validate, userService, tokenService)

	v1 := app.Group("/v1")

	HealthCheckRoutes(v1, healthCheckService)
	AuthRoutes(v1, authService, userService, tokenService, emailService)
	UserRoutes(v1, userService, tokenService)
	// TODO: add another routes here...

	if !config.IsProd {
		DocsRoutes(v1)
	}
}
