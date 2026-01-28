package router

import (
	"app/src/controller"
	m "app/src/middleware"
	"app/src/service"

	"github.com/gofiber/fiber/v2"
)

func UserRoutes(v1 fiber.Router, u service.UserService, t service.TokenService, s service.SessionService) {
	userController := controller.NewUserController(u, t)

	user := v1.Group("/users")

	user.Get("/", m.Auth(u, s, "getUsers"), userController.GetUsers)
	user.Post("/", m.Auth(u, s, "manageUsers"), userController.CreateUser)
	user.Get("/:userId", m.Auth(u, s, "getUsers"), userController.GetUserByID)
	user.Patch("/:userId", m.Auth(u, s, "manageUsers"), userController.UpdateUser)
	user.Delete("/:userId", m.Auth(u, s, "manageUsers"), userController.DeleteUser)
}
