package handlers

import (
	"database/sql"
	"os"
	"speak/db"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

type LoginViaEmailVerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func LoginViaEmailVerify(c *fiber.Ctx) error {
	var req LoginViaEmailVerifyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	if req.Email == "" || req.Code == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Email and code are required"})
	}

	// Verify code and check expiration
	var userID int64
	var expireTime time.Time
	err := db.DB.QueryRow(
		"SELECT user_id, expire_time FROM verifications WHERE email = $1 AND code = $2 AND type = 'email'",
		req.Email, req.Code,
	).Scan(&userID, &expireTime)

	if err == sql.ErrNoRows {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid code"})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	// Check if code is expired
	if time.Now().After(expireTime) {
		return c.Status(400).JSON(fiber.Map{"error": "Code expired"})
	}

	// Start transaction
	tx, err := db.DB.Begin()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}
	defer tx.Rollback()

	// Delete verification
	_, err = tx.Exec("DELETE FROM verifications WHERE user_id = $1", userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	// Generate JWT token (72 hours)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-key-change-in-production"
	}

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to generate token"})
	}

	return c.JSON(fiber.Map{
		"token":  tokenString,
		"userid": userID,
	})
}

