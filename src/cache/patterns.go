package cache

import (
	"fmt"
)

const (
	// SessionKeyPrefix is the prefix for session cache keys
	// Format: session:user:{userID}
	SessionKeyPrefix = "session:user:"
)

// GetSessionKey returns user session cache key
// Format: session:user:{userID}
func GetSessionKey(userID string) string {
	return fmt.Sprintf("%s%s", SessionKeyPrefix, userID)
}

// GetSessionPattern returns pattern for session cache invalidation
// Matches all session cache keys for user: session:user:{userID}
func GetSessionPattern(userID string) string {
	return fmt.Sprintf("%s%s", SessionKeyPrefix, userID)
}

// GetAPIResponseKeyPattern returns pattern for API response cache invalidation
// Matches all API response cache keys containing user data: api:response:*:user:{userID}:*
// Format: api:response:{method}:{path}?{query}:user:{userID} (from Phase 3 middleware/keygen.go)
func GetAPIResponseKeyPattern(userID string) string {
	return fmt.Sprintf("api:response:*:user:%s:*", userID)
}
