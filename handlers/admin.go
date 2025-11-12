package handlers

import (
	"github.com/gofiber/fiber/v2"
)

func VerifyAdmin(c *fiber.Ctx) error {
	claims, err := getClaimsFromContext(c)
	if err != nil {
		return unauthorizedResponse(c, err)
	}

	isAdmin, err := isUserAdmin(claims.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to verify admin status",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"is_admin": isAdmin,
	})
}
