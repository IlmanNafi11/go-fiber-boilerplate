package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Password     string `mapstructure:"password"`
	DB           int    `mapstructure:"db"`
	Enabled      bool   `mapstructure:"enabled"`
	MaxIdle      int    `mapstructure:"max_idle"`
	MaxActive    int    `mapstructure:"max_active"`
	IdleTimeout  int    `mapstructure:"idle_timeout"`
	PoolTimeout  int    `mapstructure:"pool_timeout"`
	DialTimeout  int    `mapstructure:"dial_timeout"`
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
}

// Validate checks if the Redis configuration is valid
func (c *RedisConfig) Validate() error {
	// Validate port is in valid range
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid Redis port: %d (must be between 1-65535)", c.Port)
	}

	// Validate DB is non-negative
	if c.DB < 0 {
		return fmt.Errorf("invalid Redis DB: %d (must be >= 0)", c.DB)
	}

	// Validate host is not empty
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("Redis host cannot be empty")
	}

	return nil
}

// ValidateRedisConfig validates Redis configuration from global variables
func ValidateRedisConfig(host string, port int, db int) error {
	// Check if Redis is configured (any env var present)
	redisConfigured := host != "" || port != 0

	if !redisConfigured {
		// Redis is intentionally disabled - no config provided
		RedisEnabled = false
		return nil
	}

	// Create RedisConfig for validation
	cfg := &RedisConfig{
		Host: host,
		Port: port,
		DB:   db,
	}

	// Set defaults if not provided
	if cfg.Port == 0 {
		cfg.Port = 6379
	}

	if cfg.DB == 0 {
		cfg.DB = 0
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid Redis configuration: %w", err)
	}

	// All validations passed - Redis is enabled
	RedisEnabled = true
	return nil
}

// LoadRedisConfig loads Redis configuration from environment variables
func LoadRedisConfig() (*RedisConfig, error) {
	var config RedisConfig

	// Check if Redis is enabled (at least one env var present)
	host := viper.GetString("REDIS_HOST")
	port := viper.GetString("REDIS_PORT")

	enabled := host != "" || port != ""
	config.Enabled = enabled

	if !enabled {
		return &config, nil
	}

	// Load with defaults
	config.Host = viper.GetString("REDIS_HOST")
	if config.Host == "" {
		config.Host = "localhost"
	}

	config.Port = viper.GetInt("REDIS_PORT")
	if config.Port == 0 {
		config.Port = 6379
	}

	config.Password = viper.GetString("REDIS_PASSWORD")
	config.DB = viper.GetInt("REDIS_DB")
	if config.DB < 0 {
		config.DB = 0
	}

	// Connection pool parameters
	config.MaxIdle = viper.GetInt("REDIS_MAX_IDLE")
	if config.MaxIdle == 0 {
		config.MaxIdle = 10
	}

	config.MaxActive = viper.GetInt("REDIS_MAX_ACTIVE")
	if config.MaxActive == 0 {
		config.MaxActive = 100
	}

	config.IdleTimeout = viper.GetInt("REDIS_IDLE_TIMEOUT")
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 300 // 5 minutes in seconds
	}

	config.PoolTimeout = viper.GetInt("REDIS_POOL_TIMEOUT")
	if config.PoolTimeout == 0 {
		config.PoolTimeout = 4 // 4 seconds
	}

	config.DialTimeout = viper.GetInt("REDIS_DIAL_TIMEOUT")
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 // 10 seconds
	}

	config.ReadTimeout = viper.GetInt("REDIS_READ_TIMEOUT")
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 5 // 5 seconds
	}

	config.WriteTimeout = viper.GetInt("REDIS_WRITE_TIMEOUT")
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 5 // 5 seconds
	}

	return &config, nil
}
