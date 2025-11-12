package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"speak/db"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

var (
	errMissingToken = errors.New("authorization token is required")
)

func extractTokenFromRequest(c *fiber.Ctx) (string, error) {
	authHeader := c.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	if token := c.Get("X-Auth-Token"); token != "" {
		return token, nil
	}

	if token := c.Query("token"); token != "" {
		return token, nil
	}

	return "", errMissingToken
}

func parseClaimsFromToken(tokenString string) (*Claims, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-key-change-in-production"
	}

	parsedToken, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsedToken.Claims.(*Claims)
	if !ok || !parsedToken.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

func getClaimsFromContext(c *fiber.Ctx) (*Claims, error) {
	tokenString, err := extractTokenFromRequest(c)
	if err != nil {
		return nil, err
	}

	return parseClaimsFromToken(tokenString)
}

func isUserAdmin(userID int64) (bool, error) {
	queries := []string{
		"SELECT is_admin FROM users WHERE user_id = $1",
		"SELECT role = 'admin' FROM users WHERE user_id = $1",
		"SELECT EXISTS (SELECT 1 FROM admins WHERE user_id = $1)",
		"SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = $1 AND role = 'admin')",
	}

	for _, query := range queries {
		var isAdmin bool
		err := db.DB.QueryRow(query, userID).Scan(&isAdmin)
		switch {
		case err == nil:
			return isAdmin, nil
		case errors.Is(err, sql.ErrNoRows):
			return false, nil
		default:
			if pqErr, ok := err.(*pq.Error); ok {
				switch pqErr.Code {
				case "42703", "42P01":
					continue
				}
			}
			return false, err
		}
	}

	return false, fmt.Errorf("could not determine admin status for user %d", userID)
}

func unauthorizedResponse(c *fiber.Ctx, err error) error {
	message := "Unauthorized"
	status := fiber.StatusUnauthorized

	if errors.Is(err, errMissingToken) {
		message = "Missing authorization token"
	} else if errors.Is(err, jwt.ErrSignatureInvalid) {
		message = "Invalid token signature"
	} else if errors.Is(err, jwt.ErrTokenExpired) {
		message = "Token expired"
	}

	return c.Status(status).JSON(fiber.Map{
		"error":   message,
		"details": err.Error(),
	})
}
