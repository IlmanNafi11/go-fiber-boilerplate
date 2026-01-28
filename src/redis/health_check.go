package redis

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// HealthMonitor manages Redis health checks in background goroutine
type HealthMonitor struct {
	client        *redis.Client
	interval      time.Duration
	ticker        *time.Ticker
	stopChan      chan struct{}
	available     *atomic.Bool
	ctx           context.Context
	cancel        context.CancelFunc
	onStateChange func(available bool)
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(client *redis.Client, interval time.Duration, onStateChange func(available bool)) *HealthMonitor {
	available := &atomic.Bool{}
	available.Store(false) // Default to false, will update on first check

	ctx, cancel := context.WithCancel(context.Background())

	return &HealthMonitor{
		client:        client,
		interval:      interval,
		stopChan:      make(chan struct{}),
		available:     available,
		ctx:           ctx,
		cancel:        cancel,
		onStateChange: onStateChange,
	}
}

// Start begins periodic health checks
func (hm *HealthMonitor) Start() {
	hm.ticker = time.NewTicker(hm.interval)
	defer hm.ticker.Stop()

	// Initial check
	available := hm.checkHealth()
	hm.available.Store(available)

	// Log initial state
	if available {
		logrus.Info("Redis is available (initial check)")
	} else {
		logrus.Warn("Redis is unavailable (initial check)")
	}

	// Call state change callback if provided
	if hm.onStateChange != nil {
		hm.onStateChange(available)
	}

	for {
		select {
		case <-hm.ctx.Done():
			logrus.Info("Health monitor stopped")
			close(hm.stopChan)
			return
		case <-hm.ticker.C:
			available := hm.checkHealth()
			previousAvailable := hm.available.Load()
			hm.available.Store(available)

			// Only log on state change
			if available != previousAvailable {
				if hm.onStateChange != nil {
					hm.onStateChange(available)
				}
				if available {
					logrus.Info("Redis is now available")
				} else {
					logrus.Warn("Redis is now unavailable")
				}
			}
		}
	}
}

// checkHealth performs PING command to test Redis connectivity
func (hm *HealthMonitor) checkHealth() bool {
	ctx, cancel := context.WithTimeout(hm.ctx, 5*time.Second)
	defer cancel()

	result := hm.client.Ping(ctx)
	return result.Err() == nil
}

// Stop gracefully shuts down health monitor
func (hm *HealthMonitor) Stop() {
	if hm.cancel != nil {
		hm.cancel()
	}
	<-hm.stopChan
}

// IsAvailable returns current Redis availability
func (hm *HealthMonitor) IsAvailable() bool {
	return hm.available.Load()
}
