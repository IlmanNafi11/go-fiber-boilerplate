package service

import (
	"app/src/config"
	"app/src/model"
	res "app/src/response"
	"app/src/utils"
	"app/src/validation"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type TokenService interface {
	GenerateToken(userID string, expires time.Time, tokenType string) (string, error)
	SaveToken(c *fiber.Ctx, token, userID, tokenType string, expires time.Time) error
	DeleteToken(c *fiber.Ctx, tokenType string, userID string) error
	DeleteAllToken(c *fiber.Ctx, userID string) error
	GetTokenByUserID(c *fiber.Ctx, tokenStr string) (*model.Token, error)
	GenerateAuthTokens(c *fiber.Ctx, user *model.User) (*res.Tokens, error)
	GenerateResetPasswordToken(c *fiber.Ctx, req *validation.ForgotPassword) (string, error)
	GenerateVerifyEmailToken(c *fiber.Ctx, user *model.User) (*string, error)
}

type tokenService struct {
	Log            *logrus.Logger
	DB             *gorm.DB
	Validate       *validator.Validate
	UserService    UserService
	SessionService SessionService
}

func NewTokenService(db *gorm.DB, validate *validator.Validate, userService UserService, sessionService SessionService) TokenService {
	return &tokenService{
		Log:            utils.Log,
		DB:             db,
		Validate:       validate,
		UserService:    userService,
		SessionService: sessionService,
	}
}

func (s *tokenService) GenerateToken(userID string, expires time.Time, tokenType string) (string, error) {
	claims := jwt.MapClaims{
		"sub":  userID,
		"iat":  time.Now().Unix(),
		"exp":  expires.Unix(),
		"type": tokenType,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(config.JWTSecret))
}

func (s *tokenService) SaveToken(c *fiber.Ctx, token, userID, tokenType string, expires time.Time) error {
	if err := s.DeleteToken(c, tokenType, userID); err != nil {
		return err
	}

	tokenDoc := &model.Token{
		Token:   token,
		UserID:  uuid.MustParse(userID),
		Type:    tokenType,
		Expires: expires,
	}

	result := s.DB.WithContext(c.Context()).Create(tokenDoc)

	if result.Error != nil {
		s.Log.Errorf("Failed save token: %+v", result.Error)
	}

	return result.Error
}

func (s *tokenService) DeleteToken(c *fiber.Ctx, tokenType string, userID string) error {
	tokenDoc := new(model.Token)

	result := s.DB.WithContext(c.Context()).
		Where("type = ? AND user_id = ?", tokenType, userID).
		Delete(tokenDoc)

	if result.Error != nil {
		s.Log.Errorf("Failed to delete token: %+v", result.Error)
	}

	// Invalidate session cache after successful token deletion (INVL-04)
	if result.Error == nil && s.SessionService != nil {
		if invalidateErr := s.SessionService.InvalidateSession(c.Context(), userID); invalidateErr != nil {
			s.Log.Warnf("failed to invalidate session cache on token deletion: %v", invalidateErr)
			// Don't fail deletion - cache invalidation is best-effort
		}
	}

	return result.Error
}

func (s *tokenService) DeleteAllToken(c *fiber.Ctx, userID string) error {
	tokenDoc := new(model.Token)

	result := s.DB.WithContext(c.Context()).Where("user_id = ?", userID).Delete(tokenDoc)

	if result.Error != nil {
		s.Log.Errorf("Failed to delete all token: %+v", result.Error)
	}

	return result.Error
}

func (s *tokenService) GetTokenByUserID(c *fiber.Ctx, tokenStr string) (*model.Token, error) {
	userID, err := utils.VerifyToken(tokenStr, config.JWTSecret, config.TokenTypeRefresh)
	if err != nil {
		return nil, err
	}

	tokenDoc := new(model.Token)

	result := s.DB.WithContext(c.Context()).
		Where("token = ? AND user_id = ?", tokenStr, userID).
		First(tokenDoc)

	if result.Error != nil {
		s.Log.Errorf("Failed get token by user id: %+v", err)
		return nil, result.Error
	}

	return tokenDoc, nil
}

func (s *tokenService) GenerateAuthTokens(c *fiber.Ctx, user *model.User) (*res.Tokens, error) {
	accessTokenExpires := time.Now().UTC().Add(time.Minute * time.Duration(config.JWTAccessExp))
	accessToken, err := s.GenerateToken(user.ID.String(), accessTokenExpires, config.TokenTypeAccess)
	if err != nil {
		s.Log.Errorf("Failed generate token: %+v", err)
		return nil, err
	}

	refreshTokenExpires := time.Now().UTC().Add(time.Hour * 24 * time.Duration(config.JWTRefreshExp))
	refreshToken, err := s.GenerateToken(user.ID.String(), refreshTokenExpires, config.TokenTypeRefresh)
	if err != nil {
		s.Log.Errorf("Failed generate token: %+v", err)
		return nil, err
	}

	if err = s.SaveToken(c, refreshToken, user.ID.String(), config.TokenTypeRefresh, refreshTokenExpires); err != nil {
		return nil, err
	}

	// Cache user session with session ID generation (SESS-01, SESS-07)
	if s.SessionService != nil {
		if cacheErr := s.SessionService.CacheUserSession(c.Context(), user.ID.String(), user); cacheErr != nil {
			s.Log.Warn("Failed to cache user session, continuing without cache", "error", cacheErr)
			// Continue with token generation - graceful degradation
		} else {
			// Set session cookie (SESS-06)
			sessionID, err := s.SessionService.GenerateSessionID()
			if err != nil {
				s.Log.Warn("Failed to generate session ID for cookie", "error", err)
			} else {
				c.Cookie(&fiber.Cookie{
					Name:     "session_id",
					Value:    sessionID,
					MaxAge:   config.SessionCacheTTL * 60, // Convert minutes to seconds
					Path:     "/",
					Secure:   config.IsProd, // HTTPS only in production
					HTTPOnly: true,          // Prevent JavaScript access
					SameSite: "Lax",         // Allow top-level navigation
				})
			}
		}
	}

	return &res.Tokens{
		Access: res.TokenExpires{
			Token:   accessToken,
			Expires: accessTokenExpires,
		},
		Refresh: res.TokenExpires{
			Token:   refreshToken,
			Expires: refreshTokenExpires,
		},
	}, nil
}

func (s *tokenService) GenerateResetPasswordToken(c *fiber.Ctx, req *validation.ForgotPassword) (string, error) {
	if err := s.Validate.Struct(req); err != nil {
		return "", err
	}

	user, err := s.UserService.GetUserByEmail(c, req.Email)
	if err != nil {
		return "", err
	}

	expires := time.Now().UTC().Add(time.Minute * time.Duration(config.JWTResetPasswordExp))
	resetPasswordToken, err := s.GenerateToken(user.ID.String(), expires, config.TokenTypeResetPassword)
	if err != nil {
		s.Log.Errorf("Failed generate token: %+v", err)
		return "", err
	}

	if err = s.SaveToken(c, resetPasswordToken, user.ID.String(), config.TokenTypeResetPassword, expires); err != nil {
		return "", err
	}

	return resetPasswordToken, nil
}

func (s *tokenService) GenerateVerifyEmailToken(c *fiber.Ctx, user *model.User) (*string, error) {
	expires := time.Now().UTC().Add(time.Minute * time.Duration(config.JWTVerifyEmailExp))
	verifyEmailToken, err := s.GenerateToken(user.ID.String(), expires, config.TokenTypeVerifyEmail)
	if err != nil {
		s.Log.Errorf("Failed generate token: %+v", err)
		return nil, err
	}

	if err = s.SaveToken(c, verifyEmailToken, user.ID.String(), config.TokenTypeVerifyEmail, expires); err != nil {
		return nil, err
	}

	return &verifyEmailToken, nil
}
