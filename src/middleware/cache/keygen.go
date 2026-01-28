package cache

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	// CacheKeyPrefix follows Phase 2 naming convention: namespace:entity:
	CacheKeyPrefix = "api:response:"
)

// GenerateCacheKey creates a deterministic cache key from HTTP method, path, and query string
func GenerateCacheKey(method, path, queryString string) string {
	normalizedPath := normalizePath(path)
	sortedQuery := sortQueryParams(queryString)
	return fmt.Sprintf("%s%s:%s?%s", CacheKeyPrefix, method, normalizedPath, sortedQuery)
}

// normalizePath cleans up the URL path by removing duplicate slashes and trailing slash
func normalizePath(path string) string {
	// Remove duplicate slashes
	path = strings.ReplaceAll(path, "//", "/")
	// Remove trailing slash
	path = strings.TrimSuffix(path, "/")
	return path
}

// sortQueryParams sorts query parameters alphabetically to ensure deterministic keys
// regardless of the order parameters appear in the URL
func sortQueryParams(queryString string) string {
	if queryString == "" {
		return ""
	}

	// Parse query string
	params, err := url.ParseQuery(queryString)
	if err != nil {
		// If parsing fails, return original query string
		return queryString
	}

	// Get sorted parameter names
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	// Rebuild query string with sorted parameters
	var sortedParts []string
	for _, name := range names {
		values := params[name]
		for _, value := range values {
			sortedParts = append(sortedParts, fmt.Sprintf("%s=%s", name, url.QueryEscape(value)))
		}
	}

	return strings.Join(sortedParts, "&")
}

// shouldSkipCache determines if a given path should bypass caching
func shouldSkipCache(path string) bool {
	skipPaths := []string{
		"/login",
		"/register",
		"/auth/token",
		"/auth/refresh",
	}

	for _, skipPath := range skipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}
