package service

import (
	"app/src/redis"
	"app/src/utils"
	"errors"
	"runtime"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type HealthCheckService interface {
	GormCheck() error
	MemoryHeapCheck() error
	RedisCheck() bool
}

type healthCheckService struct {
	Log           *logrus.Logger
	DB            *gorm.DB
	HealthMonitor *redis.HealthMonitor
}

func NewHealthCheckService(db *gorm.DB, healthMonitor *redis.HealthMonitor) HealthCheckService {
	return &healthCheckService{
		Log:           utils.Log,
		DB:            db,
		HealthMonitor: healthMonitor,
	}
}

func (s *healthCheckService) GormCheck() error {
	sqlDB, errDB := s.DB.DB()
	if errDB != nil {
		s.Log.Errorf("failed to access the database connection pool: %v", errDB)
		return errDB
	}

	if err := sqlDB.Ping(); err != nil {
		s.Log.Errorf("failed to ping the database: %v", err)
		return err
	}

	return nil
}

// RedisCheck returns true if Redis is available, false otherwise
func (s *healthCheckService) RedisCheck() bool {
	if s.HealthMonitor == nil {
		// Redis not configured or disabled
		return false
	}
	return s.HealthMonitor.IsAvailable()
}

// MemoryHeapCheck checks if heap memory usage exceeds a threshold
func (s *healthCheckService) MemoryHeapCheck() error {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats) // Collect memory statistics

	heapAlloc := memStats.HeapAlloc            // Heap memory currently allocated
	heapThreshold := uint64(300 * 1024 * 1024) // Example threshold: 300 MB

	s.Log.Infof("Heap Memory Allocation: %v bytes", heapAlloc)

	// If the heap allocation exceeds the threshold, return an error
	if heapAlloc > heapThreshold {
		s.Log.Errorf("Heap memory usage exceeds threshold: %v bytes", heapAlloc)
		return errors.New("heap memory usage too high")
	}

	return nil
}
