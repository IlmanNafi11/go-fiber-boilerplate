package service

import (
	"app/src/config"
	"app/src/model"
	"app/src/utils"
	"app/src/validation"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type UserService interface {
	GetUsers(c *fiber.Ctx, params *validation.QueryUser) ([]model.User, int64, error)
	GetUserByID(c *fiber.Ctx, id string) (*model.User, error)
	GetUserByEmail(c *fiber.Ctx, email string) (*model.User, error)
	CreateUser(c *fiber.Ctx, req *validation.CreateUser) (*model.User, error)
	UpdatePassOrVerify(c *fiber.Ctx, req *validation.UpdatePassOrVerify, id string) error
	UpdateUser(c *fiber.Ctx, req *validation.UpdateUser, id string) (*model.User, error)
	DeleteUser(c *fiber.Ctx, id string) error
	CreateGoogleUser(c *fiber.Ctx, req *validation.GoogleLogin) (*model.User, error)
}

type userService struct {
	Log            *logrus.Logger
	DB             *gorm.DB
	Validate       *validator.Validate
	SessionService SessionService
}

func NewUserService(db *gorm.DB, validate *validator.Validate, sessionService SessionService) UserService {
	return &userService{
		Log:            utils.Log,
		DB:             db,
		Validate:       validate,
		SessionService: sessionService,
	}
}

func (s *userService) GetUsers(c *fiber.Ctx, params *validation.QueryUser) ([]model.User, int64, error) {
	var users []model.User
	var totalResults int64

	if err := s.Validate.Struct(params); err != nil {
		return nil, 0, err
	}

	offset := (params.Page - 1) * params.Limit
	query := s.DB.WithContext(c.Context()).Order("created_at asc")

	if search := params.Search; search != "" {
		query = query.Where("name LIKE ? OR email LIKE ? OR role LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	result := query.Find(&users).Count(&totalResults)
	if result.Error != nil {
		s.Log.Errorf("Failed to search users: %+v", result.Error)
		return nil, 0, result.Error
	}

	result = query.Limit(params.Limit).Offset(offset).Find(&users)
	if result.Error != nil {
		s.Log.Errorf("Failed to get all users: %+v", result.Error)
		return nil, 0, result.Error
	}

	return users, totalResults, result.Error
}

func (s *userService) GetUserByID(c *fiber.Ctx, id string) (*model.User, error) {
	user := new(model.User)

	result := s.DB.WithContext(c.Context()).First(user, "id = ?", id)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	if result.Error != nil {
		s.Log.Errorf("Failed get user by id: %+v", result.Error)
	}

	return user, result.Error
}

func (s *userService) GetUserByEmail(c *fiber.Ctx, email string) (*model.User, error) {
	user := new(model.User)

	result := s.DB.WithContext(c.Context()).Where("email = ?", email).First(user)

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	if result.Error != nil {
		s.Log.Errorf("Failed get user by email: %+v", result.Error)
	}

	return user, result.Error
}

func (s *userService) CreateUser(c *fiber.Ctx, req *validation.CreateUser) (*model.User, error) {
	if err := s.Validate.Struct(req); err != nil {
		return nil, err
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		s.Log.Errorf("Failed hash password: %+v", err)
		return nil, err
	}

	user := &model.User{
		Name:     req.Name,
		Email:    req.Email,
		Password: hashedPassword,
		Role:     req.Role,
	}

	result := s.DB.WithContext(c.Context()).Create(user)

	if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
		return nil, fiber.NewError(fiber.StatusConflict, "Email is already in use")
	}

	if result.Error != nil {
		s.Log.Errorf("Failed to create user: %+v", result.Error)
	}

	return user, result.Error
}

func (s *userService) UpdateUser(c *fiber.Ctx, req *validation.UpdateUser, id string) (*model.User, error) {
	if err := s.Validate.Struct(req); err != nil {
		return nil, err
	}

	if req.Email == "" && req.Name == "" && req.Password == "" && req.Role == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "Invalid Request")
	}

	// Get current user to detect role changes
	currentUser, err := s.GetUserByID(c, id)
	if err != nil {
		return nil, err
	}

	// Check if role is changing (privilege elevation detection, SESS-07)
	roleChanged := req.Role != "" && req.Role != currentUser.Role

	if req.Password != "" {
		hashedPassword, err := utils.HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		req.Password = hashedPassword
	}

	updateBody := &model.User{
		Name:     req.Name,
		Password: req.Password,
		Email:    req.Email,
		Role:     req.Role,
	}

	result := s.DB.WithContext(c.Context()).Where("id = ?", id).Updates(updateBody)

	if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
		return nil, fiber.NewError(fiber.StatusConflict, "Email is already in use")
	}

	if result.RowsAffected == 0 {
		return nil, fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	if result.Error != nil {
		s.Log.Errorf("Failed to update user: %+v", result.Error)
	}

	// Handle cache invalidation and session regeneration
	if s.SessionService != nil {
		if roleChanged {
			// Role changed - regenerate session ID for security (SESS-07 privilege elevation)
			bytes := make([]byte, 32)
			if _, err := rand.Read(bytes); err != nil {
				s.Log.Warn("Failed to generate new session ID, using cache invalidation only", "error", err)
				// Invalidate old cache
				if invalidateErr := s.SessionService.InvalidateSession(c.Context(), id); invalidateErr != nil {
					s.Log.Warn("Failed to invalidate cache", "error", invalidateErr)
				}
				return nil, fiber.NewError(fiber.StatusInternalServerError, "Session update failed")
			}
			newSessionID := base64.URLEncoding.EncodeToString(bytes)

			// Invalidate old cache and set new one with new session ID
			if invalidateErr := s.SessionService.InvalidateSession(c.Context(), id); invalidateErr != nil {
				s.Log.Warn("Failed to invalidate old cache", "error", invalidateErr)
			}

			// Get updated user data
			updatedUser, err := s.GetUserByID(c, id)
			if err != nil {
				return nil, err
			}

			// Cache user with new session ID
			if cacheErr := s.SessionService.CacheUserSession(c.Context(), id, updatedUser); cacheErr != nil {
				s.Log.Warn("Failed to cache user with new session", "error", cacheErr)
			}

			// Update session cookie
			c.Cookie(&fiber.Cookie{
				Name:     "session_id",
				Value:    newSessionID,
				MaxAge:   config.SessionCacheTTL * 60, // Convert minutes to seconds
				Path:     "/",
				Secure:   config.IsProd,
				HTTPOnly: true,
				SameSite: "Lax",
			})
		} else {
			// Profile changed but not role - just invalidate cache (SESS-03)
			if invalidateErr := s.SessionService.InvalidateSession(c.Context(), id); invalidateErr != nil {
				s.Log.Warn("Failed to invalidate cache on user update", "error", invalidateErr)
			}
		}
	}

	user, err := s.GetUserByID(c, id)
	if err != nil {
		return nil, err
	}

	return user, result.Error
}

func (s *userService) UpdatePassOrVerify(c *fiber.Ctx, req *validation.UpdatePassOrVerify, id string) error {
	if err := s.Validate.Struct(req); err != nil {
		return err
	}

	if req.Password == "" && !req.VerifiedEmail {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid Request")
	}

	if req.Password != "" {
		hashedPassword, err := utils.HashPassword(req.Password)
		if err != nil {
			return err
		}
		req.Password = hashedPassword
	}

	updateBody := &model.User{
		Password:      req.Password,
		VerifiedEmail: req.VerifiedEmail,
	}

	result := s.DB.WithContext(c.Context()).Where("id = ?", id).Updates(updateBody)

	if result.RowsAffected == 0 {
		return fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	if result.Error != nil {
		s.Log.Errorf("Failed to update user password or verifiedEmail: %+v", result.Error)
	}

	return result.Error
}

func (s *userService) DeleteUser(c *fiber.Ctx, id string) error {
	user := new(model.User)

	result := s.DB.WithContext(c.Context()).Delete(user, "id = ?", id)

	if result.RowsAffected == 0 {
		return fiber.NewError(fiber.StatusNotFound, "User not found")
	}

	if result.Error != nil {
		s.Log.Errorf("Failed to delete user: %+v", result.Error)
	}

	// Invalidate cache after successful deletion (SESS-04)
	if s.SessionService != nil {
		if invalidateErr := s.SessionService.InvalidateSession(c.Context(), id); invalidateErr != nil {
			s.Log.Warn("Failed to invalidate cache on user deletion", "error", invalidateErr)
			// Don't fail deletion - graceful degradation
		}
	}

	return result.Error
}

func (s *userService) CreateGoogleUser(c *fiber.Ctx, req *validation.GoogleLogin) (*model.User, error) {
	if err := s.Validate.Struct(req); err != nil {
		return nil, err
	}

	userFromDB, err := s.GetUserByEmail(c, req.Email)
	if err != nil {
		if err.Error() == "User not found" {
			user := &model.User{
				Name:          req.Name,
				Email:         req.Email,
				VerifiedEmail: req.VerifiedEmail,
			}

			if createErr := s.DB.WithContext(c.Context()).Create(user).Error; createErr != nil {
				s.Log.Errorf("Failed to create user: %+v", createErr)
				return nil, createErr
			}

			return user, nil
		}

		return nil, err
	}

	userFromDB.VerifiedEmail = req.VerifiedEmail
	if updateErr := s.DB.WithContext(c.Context()).Save(userFromDB).Error; updateErr != nil {
		s.Log.Errorf("Failed to update user: %+v", updateErr)
		return nil, updateErr
	}

	return userFromDB, nil
}
