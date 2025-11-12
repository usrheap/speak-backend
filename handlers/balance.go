package handlers

import (
	"database/sql"
	"errors"
	"fmt"

	"speak/db"

	"github.com/gofiber/fiber/v2"
)

func GetBalance(c *fiber.Ctx) error {
	claims, err := getClaimsFromContext(c)
	if err != nil {
		return unauthorizedResponse(c, err)
	}

	var quantity float64
	err = db.DB.QueryRow("SELECT quantity FROM balance WHERE user_id = $1", claims.UserID).Scan(&quantity)
	switch {
	case err == nil:
		return c.JSON(fiber.Map{"balance": quantity})
	case errors.Is(err, sql.ErrNoRows):
		if _, insertErr := db.DB.Exec(
			"INSERT INTO balance (user_id, quantity) VALUES ($1, 0) ON CONFLICT (user_id) DO NOTHING",
			claims.UserID,
		); insertErr != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to initialize balance",
				"details": insertErr.Error(),
			})
		}
		return c.JSON(fiber.Map{"balance": 0})
	default:
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch balance",
			"details": err.Error(),
		})
	}
}

func ensureBalanceRecord(tx *sql.Tx, userID int64) error {
	if tx == nil {
		return fmt.Errorf("transaction is required to ensure balance record")
	}
	_, err := tx.Exec(
		"INSERT INTO balance (user_id, quantity) VALUES ($1, 0) ON CONFLICT (user_id) DO NOTHING",
		userID,
	)
	return err
}
