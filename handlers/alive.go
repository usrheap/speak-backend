package handlers

import "github.com/gofiber/fiber/v2"

func Alive(c *fiber.Ctx) error {
	return c.SendString("OK")
}

