package middleware

import (
	"app/src/config"
	"app/src/model"
	"app/src/service"
	"app/src/utils"
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func Auth(userService service.UserService, sessionService service.SessionService, requiredRights ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "Please authenticate")
		}

		userID, err := utils.VerifyToken(token, config.JWTSecret, config.TokenTypeAccess)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "Please authenticate")
		}

		// Try cache first (SESS-02)
		sessionData, err := sessionService.GetUserSession(c.Context(), userID)
		var user *model.User

		if err == nil && sessionData != nil {
			// Cache hit - convert SessionData to model.User
			user = &model.User{
				ID:            uuid.MustParse(sessionData.ID),
				Name:          sessionData.Name,
				Email:         sessionData.Email,
				Role:          sessionData.Role,
				VerifiedEmail: sessionData.VerifiedEmail,
			}
			// Skip database call
		} else {
			// Cache miss or Redis error - fallback to database
			if !errors.Is(err, service.ErrCacheMiss) {
				// Redis error, log warning but continue
				utils.Log.Warn("Cache error, falling back to database", "error", err)
			}
			// Query database
			user, err = userService.GetUserByID(c, userID)
			if err != nil || user == nil {
				return fiber.NewError(fiber.StatusUnauthorized, "Please authenticate")
			}
			// Populate cache asynchronously (don't block response)
			go func() {
				if cacheErr := sessionService.CacheUserSession(context.Background(), userID, user); cacheErr != nil {
					utils.Log.Warn("Failed to populate cache", "error", cacheErr)
				}
			}()
		}

		c.Locals("user", user)

		if len(requiredRights) > 0 {
			userRights, hasRights := config.RoleRights[user.Role]
			if (!hasRights || !hasAllRights(userRights, requiredRights)) && c.Params("userId") != userID {
				return fiber.NewError(fiber.StatusForbidden, "You don't have permission to access this resource")
			}
		}

		return c.Next()
	}
}

func hasAllRights(userRights, requiredRights []string) bool {
	rightSet := make(map[string]struct{}, len(userRights))
	for _, right := range userRights {
		rightSet[right] = struct{}{}
	}

	for _, right := range requiredRights {
		if _, exists := rightSet[right]; !exists {
			return false
		}
	}
	return true
}
