package redis

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"app/src/config"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/sony/gobreaker/v2"
)

var (
	// ErrRedisUnavailable is returned when Redis is disabled or circuit breaker is open
	ErrRedisUnavailable = fmt.Errorf("redis unavailable")

	// redisClient is the singleton Redis client instance
	redisClient *redis.Client

	// redisAvailable is the atomic availability flag
	// 0 = unavailable, 1 = available
	redisAvailable int32

	// redisCB is the circuit breaker instance
	redisCB *gobreaker.CircuitBreaker[interface{}]
)

// RedisClient wraps the go-redis client with circuit breaker protection
type RedisClient struct {
	client         *redis.Client
	circuitBreaker *gobreaker.CircuitBreaker[interface{}]
}

// NewRedisClient creates a new Redis client with circuit breaker
func NewRedisClient(cfg config.RedisConfig) (*RedisClient, error) {
	if !cfg.Enabled {
		logrus.Info("Redis disabled - running in database-only mode")
		setAvailable(false)
		return nil, nil
	}

	// Create Redis options with connection pool
	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.MaxActive,
		MinIdleConns: cfg.MaxIdle,
		MaxIdleConns: cfg.MaxIdle,
		PoolTimeout:  time.Duration(cfg.PoolTimeout) * time.Second,
		DialTimeout:  time.Duration(cfg.DialTimeout) * time.Second,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,

		// Retry with exponential backoff
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 32 * time.Millisecond,
	}

	// Create client
	client := redis.NewClient(opts)

	// Create circuit breaker
	cb := gobreaker.NewCircuitBreaker[interface{}](gobreaker.Settings{
		Name:        "Redis",
		MaxRequests: 5,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool { return counts.ConsecutiveFailures > 3 },
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			logrus.Infof("Circuit breaker '%s' state changed: %s -> %s", name, from, to)
		},
	})

	redisClient = client
	redisCB = cb

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.DialTimeout)*time.Second)
	defer cancel()

	if err := testConnection(ctx, client); err != nil {
		logrus.Errorf("Failed to connect to Redis: %v", err)
		setAvailable(false)
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	// Connection successful
	setAvailable(true)
	logrus.Infof("Redis connected successfully: %s:%d (DB: %d)", cfg.Host, cfg.Port, cfg.DB)

	// Start background health monitor
	go startHealthMonitor(context.Background())

	redisClientInstance := &RedisClient{
		client:         client,
		circuitBreaker: cb,
	}

	return redisClientInstance, nil
}

// testConnection tests Redis connectivity with PING command
func testConnection(ctx context.Context, client *redis.Client) error {
	return client.Ping(ctx).Err()
}

// IsAvailable returns true if Redis is available and circuit breaker allows requests
func IsAvailable() bool {
	if atomic.LoadInt32(&redisAvailable) == 0 {
		return false
	}

	// Check circuit breaker state
	if redisCB != nil && redisCB.State() == gobreaker.StateOpen {
		return false
	}

	return true
}

// startHealthMonitor runs periodic health checks in background
func startHealthMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logrus.Info("Health monitor stopped")
			return
		case <-ticker.C:
			if redisClient == nil {
				continue
			}

			err := redisClient.Ping(ctx).Err()
			if err != nil {
				logrus.Warnf("Redis health check failed: %v", err)
				setAvailable(false)
			} else {
				if atomic.LoadInt32(&redisAvailable) == 0 {
					logrus.Info("Redis reconnected")
					setAvailable(true)
					// TODO: Trigger cache warm-up (Phase 2-6)
				}
			}
		}
	}
}

// setAvailable sets the atomic availability flag
func setAvailable(available bool) {
	var v int32 = 0
	if available {
		v = 1
	}
	atomic.StoreInt32(&redisAvailable, v)
}

// GetClient returns the initialized Redis client
func (r *RedisClient) GetClient() *redis.Client {
	return r.client
}

// ExecuteWithCircuitBreaker executes a function through the circuit breaker
func (r *RedisClient) ExecuteWithCircuitBreaker(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	if r == nil {
		return nil, ErrRedisUnavailable
	}

	result, err := r.circuitBreaker.Execute(fn)
	return result, err
}

// Close closes the Redis client connection
func (r *RedisClient) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}
