package service

import (
	"app/src/config"
	"app/src/model"
	"app/src/redis"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// SessionData represents cached user session data
type SessionData struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	VerifiedEmail bool   `json:"verified_email"`
	SessionID     string `json:"session_id"` // For SESS-07 privilege elevation tracking
	CreatedAt     int64  `json:"created_at"` // For cache freshness tracking
}

// ErrCacheMiss indicates the requested session is not in the cache
var ErrCacheMiss = errors.New("cache miss")

// SessionService defines the interface for session caching operations
type SessionService interface {
	CacheUserSession(ctx context.Context, userID string, user *model.User) error
	GetUserSession(ctx context.Context, userID string) (*SessionData, error)
	InvalidateSession(ctx context.Context, userID string) error
	GenerateSessionID() (string, error)
}

// sessionService implements SessionService interface
type sessionService struct {
	redisClient *redis.RedisClient
}

// NewSessionService creates a new session service instance
func NewSessionService(redisClient *redis.RedisClient) SessionService {
	return &sessionService{
		redisClient: redisClient,
	}
}

// CacheUserSession stores user session data in Redis cache
func (s *sessionService) CacheUserSession(ctx context.Context, userID string, user *model.User) error {
	// Check if Redis is available
	if !redis.IsAvailable() {
		// Graceful degradation - return nil instead of error (SESS-05)
		return nil
	}

	// Generate secure session ID
	sessionID, err := s.GenerateSessionID()
	if err != nil {
		return fmt.Errorf("failed to generate session ID: %w", err)
	}

	// Create session data from user model
	sessionData := &SessionData{
		ID:            user.ID.String(),
		Name:          user.Name,
		Email:         user.Email,
		Role:          user.Role,
		VerifiedEmail: user.VerifiedEmail,
		SessionID:     sessionID,
		CreatedAt:     time.Now().Unix(),
	}

	// Serialize to JSON
	serialized, err := json.Marshal(sessionData)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	// Execute through circuit breaker
	result, err := s.redisClient.ExecuteWithCircuitBreaker(ctx, func() (interface{}, error) {
		key := fmt.Sprintf("session:user:%s", userID)
		ttl := time.Duration(config.SessionCacheTTL) * time.Minute
		return nil, s.redisClient.GetClient().Set(ctx, key, serialized, ttl).Err()
	})

	if err != nil {
		return fmt.Errorf("failed to cache session: %w", err)
	}

	// Result is nil for Set operations
	_ = result

	return nil
}

// GetUserSession retrieves user session data from Redis cache
func (s *sessionService) GetUserSession(ctx context.Context, userID string) (*SessionData, error) {
	// Check if Redis is available
	if !redis.IsAvailable() {
		// Return ErrCacheMiss to trigger DB fallback (graceful degradation, SESS-05)
		return nil, ErrCacheMiss
	}

	// Execute through circuit breaker
	result, err := s.redisClient.ExecuteWithCircuitBreaker(ctx, func() (interface{}, error) {
		key := fmt.Sprintf("session:user:%s", userID)
		data, err := s.redisClient.GetClient().Get(ctx, key).Bytes()
		if err != nil {
			if errors.Is(err, goredis.Nil) {
				// Cache miss - return ErrCacheMiss
				return nil, ErrCacheMiss
			}
			// Redis error - treat as unavailable (graceful degradation)
			return nil, ErrCacheMiss
		}
		return data, nil
	})

	if err != nil {
		if errors.Is(err, ErrCacheMiss) {
			return nil, ErrCacheMiss
		}
		// Unexpected error - treat as cache miss for graceful degradation
		return nil, ErrCacheMiss
	}

	// Extract bytes from result
	data, ok := result.([]byte)
	if !ok {
		return nil, ErrCacheMiss
	}

	// Unmarshal JSON to SessionData
	var sessionData SessionData
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &sessionData, nil
}

// InvalidateSession removes user session data from Redis cache
func (s *sessionService) InvalidateSession(ctx context.Context, userID string) error {
	// Check if Redis is available
	if !redis.IsAvailable() {
		// Graceful degradation - return nil instead of error
		return nil
	}

	// Execute through circuit breaker
	_, err := s.redisClient.ExecuteWithCircuitBreaker(ctx, func() (interface{}, error) {
		key := fmt.Sprintf("session:user:%s", userID)
		return nil, s.redisClient.GetClient().Del(ctx, key).Err()
	})

	if err != nil {
		// Log error but don't fail the operation (graceful degradation)
		return nil
	}

	return nil
}

// GenerateSessionID generates a cryptographically secure session ID
func (s *sessionService) GenerateSessionID() (string, error) {
	// Generate 32 random bytes (256 bits of entropy)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode with base64.URLEncoding for URL-safe output
	return base64.URLEncoding.EncodeToString(b), nil
}
