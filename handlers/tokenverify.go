package handlers

import (
	"database/sql"
	"os"
	"speak/db"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

type TokenVerifyRequest struct {
	Token string `json:"token"`
}

func TokenVerify(c *fiber.Ctx) error {
	var req TokenVerifyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	if req.Token == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Token is required"})
	}

	// Get JWT secret
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-key-change-in-production"
	}

	// Parse and verify token
	parsedToken, err := jwt.ParseWithClaims(req.Token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid or expired token"})
	}

	// Extract claims
	claims, ok := parsedToken.Claims.(*Claims)
	if !ok || !parsedToken.Valid {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid token"})
	}

	// Get user info from database
	var firstName, lastName sql.NullString
	err = db.DB.QueryRow(
		"SELECT first_name, last_name FROM users WHERE user_id = $1",
		claims.UserID,
	).Scan(&firstName, &lastName)

	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "User not found"})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	// Prepare response
	response := fiber.Map{}
	if firstName.Valid {
		response["firstname"] = firstName.String
	}
	if lastName.Valid {
		response["lastname"] = lastName.String
	}

	return c.JSON(response)
}

