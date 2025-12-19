package handler

import (
	"searchav/internal/config"

	"github.com/gofiber/fiber/v2"
)

const (
	// AuthHeader is the header name for password authentication
	AuthHeader = "X-Auth-Password"
	// AdultPermKey is the context key for adult permission
	AdultPermKey = "adult_perm"
)

// AuthMiddleware creates a password authentication middleware
func AuthMiddleware(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		password := c.Get(AuthHeader)
		result := cfg.ValidatePassword(password)

		// If auth is enabled and password is invalid, return 401
		if cfg.Auth.Enabled && !result.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code": 401,
				"msg":  "unauthorized",
			})
		}

		// Store adult permission in context for later use
		c.Locals(AdultPermKey, result.Adult)

		return c.Next()
	}
}

// GetAdultPerm retrieves the adult permission from context
func GetAdultPerm(c *fiber.Ctx) bool {
	if perm, ok := c.Locals(AdultPermKey).(bool); ok {
		return perm
	}
	return false
}
