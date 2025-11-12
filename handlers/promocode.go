package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"speak/db"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"
)

type addPromocodeRequest struct {
	Name      string          `json:"name"`
	Keyword   string          `json:"keyword"`
	Quantity  json.RawMessage `json:"quantity"`
	IsActive  *bool           `json:"is_active"`
	StartTime *string         `json:"start_time"`
	EndTime   *string         `json:"end_time"`
	Metadata  *string         `json:"metadata"`
}

type activatePromocodeRequest struct {
	Keyword string `json:"promocode"`
}

type promocodeRecord struct {
	ID        int64
	Name      string
	Quantity  float64
	IsActive  bool
	StartTime *time.Time
	EndTime   *time.Time
}

type promocodeActivationResponse struct {
	PromocodeID int64      `json:"promocode_id"`
	Keyword     string     `json:"keyword"`
	Quantity    float64    `json:"quantity"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
}

func AddPromocode(c *fiber.Ctx) error {
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

	if !isAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Admin privileges required",
		})
	}

	var req addPromocodeRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid request payload",
			"details": err.Error(),
		})
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Name is required",
		})
	}

	quantityValue, err := parseFlexibleQuantity(req.Quantity)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid quantity",
			"details": err.Error(),
		})
	}

	if quantityValue <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Quantity must be greater than zero",
		})
	}

	if math.IsNaN(quantityValue) || math.IsInf(quantityValue, 0) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Quantity must be a finite number",
		})
	}

	roundedQuantity := math.Round(quantityValue)
	if math.Abs(quantityValue-roundedQuantity) > 1e-9 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Quantity must be an integer value",
		})
	}
	quantityInt := int64(roundedQuantity)

	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" || strings.EqualFold(keyword, "none") {
		var genErr error
		keyword, genErr = generatePromocodeKeyword(8)
		if genErr != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to generate promocode keyword",
				"details": genErr.Error(),
			})
		}
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	startTime, endTime, parseErr := resolvePromocodeTimes(req.StartTime, req.EndTime)
	if parseErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid start or end time",
			"details": parseErr.Error(),
		})
	}

	if startTime != nil && endTime != nil {
		insertNew := `
			INSERT INTO promocode (name, keyword, start_time, end_time, quantity, created_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
		`
		if _, err := db.DB.Exec(insertNew, name, keyword, *startTime, *endTime, quantityInt); err == nil {
			activeNow := computePromocodeActive(&promocodeRecord{
				StartTime: startTime,
				EndTime:   endTime,
			})
			return c.JSON(fiber.Map{
				"keyword":    keyword,
				"active":     activeNow,
				"quantity":   quantityInt,
				"name":       name,
				"start_time": startTime.Format(time.RFC3339),
				"end_time":   endTime.Format(time.RFC3339),
			})
		} else if pqErr, ok := err.(*pq.Error); ok {
			switch pqErr.Code {
			case "23505":
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error": "Promocode keyword already exists",
				})
			case "42P01", "42703":
				// fallback to legacy schema below
			default:
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   "Failed to create promocode",
					"details": err.Error(),
				})
			}
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to create promocode",
				"details": err.Error(),
			})
		}
	}

	response := fiber.Map{
		"keyword":  keyword,
		"active":   isActive,
		"quantity": quantityInt,
		"name":     name,
	}
	if startTime != nil {
		response["start_time"] = startTime.Format(time.RFC3339)
	}
	if endTime != nil {
		response["end_time"] = endTime.Format(time.RFC3339)
	}

	legacyQuery := `
		INSERT INTO promocodes (keyword, quantity, is_active, created_at)
		VALUES ($1, $2, $3, NOW())
	`
	if _, err := db.DB.Exec(legacyQuery, keyword, quantityInt, isActive); err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			switch pqErr.Code {
			case "23505":
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error": "Promocode keyword already exists",
				})
			case "42703":
				if _, retryErr := db.DB.Exec(
					"INSERT INTO promocodes (keyword, quantity, is_active) VALUES ($1, $2, $3)",
					keyword, quantityInt, isActive,
				); retryErr != nil {
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error":   "Failed to create promocode",
						"details": retryErr.Error(),
					})
				}
			default:
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   "Failed to create promocode",
					"details": err.Error(),
				})
			}
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to create promocode",
				"details": err.Error(),
			})
		}
	}

	return c.JSON(response)
}

func parseFlexibleQuantity(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("quantity is required")
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return 0, fmt.Errorf("quantity is required")
	}

	if trimmed[0] == '"' {
		unquoted, err := strconv.Unquote(trimmed)
		if err != nil {
			return 0, fmt.Errorf("invalid quantity: %w", err)
		}
		trimmed = strings.TrimSpace(unquoted)
		if trimmed == "" {
			return 0, fmt.Errorf("quantity is required")
		}
	}

	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quantity: %w", err)
	}

	return value, nil
}

func resolvePromocodeTimes(start, end *string) (*time.Time, *time.Time, error) {
	now := time.Now().UTC()
	var startTime *time.Time
	var endTime *time.Time

	if start != nil && strings.TrimSpace(*start) != "" {
		parsed, err := parseTimeInput(*start)
		if err != nil {
			return nil, nil, err
		}
		parsed = parsed.UTC()
		startTime = &parsed
	} else {
		startVal := now
		startTime = &startVal
	}

	if end != nil && strings.TrimSpace(*end) != "" {
		parsed, err := parseTimeInput(*end)
		if err != nil {
			return nil, nil, err
		}
		parsed = parsed.UTC()
		endTime = &parsed
	} else {
		defaultEnd := startTime.Add(30 * 24 * time.Hour)
		endTime = &defaultEnd
	}

	if !endTime.After(*startTime) {
		return nil, nil, fmt.Errorf("end time must be after start time")
	}

	return startTime, endTime, nil
}

func parseTimeInput(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	trimmed := strings.TrimSpace(value)
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time format: %s", value)
}

func ActivatePromocode(c *fiber.Ctx) error {
	claims, err := getClaimsFromContext(c)
	if err != nil {
		return unauthorizedResponse(c, err)
	}

	var req activatePromocodeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "Invalid request payload",
			"details": err.Error(),
		})
	}

	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Promocode keyword is required",
		})
	}

	record, err := findPromocodeByKeyword(keyword)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Promocode not found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch promocode",
			"details": err.Error(),
		})
	}

	if !record.IsActive {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Promocode is not active",
		})
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to start transaction",
			"details": err.Error(),
		})
	}
	defer tx.Rollback()

	var existing int
	checkPrimary := `
		SELECT 1 FROM promocode_activation WHERE promocode_id = $1 AND user_id = $2
	`
	if err := tx.QueryRow(checkPrimary, record.ID, claims.UserID).Scan(&existing); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// no activation yet, continue
		default:
			if pqErr, ok := err.(*pq.Error); ok {
				if pqErr.Code == "42P01" || pqErr.Code == "42703" {
					fallbackQuery := `
						SELECT 1 FROM promocode_activations WHERE promocode_id = $1 AND user_id = $2
					`
					if fallbackErr := tx.QueryRow(fallbackQuery, record.ID, claims.UserID).Scan(&existing); fallbackErr != nil {
						if errors.Is(fallbackErr, sql.ErrNoRows) {
							// still no activation, continue
						} else {
							return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
								"error":   "Failed to check legacy promocode activation",
								"details": fallbackErr.Error(),
							})
						}
					} else {
						return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
							"error": "Promocode already activated by this user",
						})
					}
				} else {
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error":   "Failed to check promocode activation",
						"details": err.Error(),
					})
				}
			} else {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   "Failed to check promocode activation",
					"details": err.Error(),
				})
			}
		}
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Promocode already activated by this user",
		})
	}

	if err := ensureBalanceRecord(tx, claims.UserID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to prepare balance record",
			"details": err.Error(),
		})
	}

	if _, err := tx.Exec(
		"UPDATE balance SET quantity = quantity + $1 WHERE user_id = $2",
		record.Quantity, claims.UserID,
	); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to update balance",
			"details": err.Error(),
		})
	}

	insertActivation := `
		INSERT INTO promocode_activation (promocode_id, user_id, enable_time, quantity)
		VALUES ($1, $2, $3, $4)
	`
	now := time.Now().UTC()
	if _, err := tx.Exec(insertActivation, record.ID, claims.UserID, now, int64(math.Round(record.Quantity))); err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			switch pqErr.Code {
			case "42703", "42P01":
				if _, retryErr := tx.Exec(
					"INSERT INTO promocode_activations (promocode_id, user_id, activated_at) VALUES ($1, $2, $3)",
					record.ID, claims.UserID, now,
				); retryErr != nil {
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error":   "Failed to record promocode activation",
						"details": retryErr.Error(),
					})
				}
			case "23505":
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Promocode already activated by this user",
				})
			default:
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   "Failed to record promocode activation",
					"details": err.Error(),
				})
			}
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to record promocode activation",
				"details": err.Error(),
			})
		}
	}

	if err := tx.Commit(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to complete activation",
			"details": err.Error(),
		})
	}

	var newBalance float64
	if err := db.DB.QueryRow(
		"SELECT quantity FROM balance WHERE user_id = $1",
		claims.UserID,
	).Scan(&newBalance); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch updated balance",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Promocode activated successfully",
		"balance": newBalance,
	})
}

func GetPastPromocodes(c *fiber.Ctx) error {
	claims, err := getClaimsFromContext(c)
	if err != nil {
		return unauthorizedResponse(c, err)
	}

	activations, err := fetchPromocodeActivations(claims.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch promocode activations",
			"details": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"activations": activations,
	})
}

var errLegacyPromocodeSchema = errors.New("legacy_promocode_schema")

func fetchPromocodeActivations(userID int64) ([]promocodeActivationResponse, error) {
	records, err := fetchPromocodeActivationsNew(userID)
	if err != nil {
		if errors.Is(err, errLegacyPromocodeSchema) {
			return fetchPromocodeActivationsLegacy(userID)
		}
		return nil, err
	}
	return records, nil
}

func fetchPromocodeActivationsNew(userID int64) ([]promocodeActivationResponse, error) {
	query := `
		SELECT pa.promocode_id,
		       p.keyword,
		       p.quantity,
		       pa.enable_time,
		       p.start_time,
		       p.end_time
		FROM promocode_activation pa
		JOIN promocode p ON p.id = pa.promocode_id
		WHERE pa.user_id = $1
		ORDER BY pa.enable_time DESC
	`

	rows, err := db.DB.Query(query, userID)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			switch pqErr.Code {
			case "42P01", "42703":
				return nil, errLegacyPromocodeSchema
			}
		}
		return nil, err
	}
	defer rows.Close()

	results := []promocodeActivationResponse{}
	for rows.Next() {
		var (
			item      promocodeActivationResponse
			quantity  interface{}
			activated sql.NullTime
			start     sql.NullTime
			end       sql.NullTime
		)

		if err := rows.Scan(&item.PromocodeID, &item.Keyword, &quantity, &activated, &start, &end); err != nil {
			return nil, err
		}

		q, convErr := normalizeQuantity(quantity)
		if convErr != nil {
			return nil, convErr
		}
		item.Quantity = q

		if activated.Valid {
			item.ActivatedAt = &activated.Time
		}
		if start.Valid {
			item.StartTime = &start.Time
		}
		if end.Valid {
			item.EndTime = &end.Time
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func fetchPromocodeActivationsLegacy(userID int64) ([]promocodeActivationResponse, error) {
	query := `
		SELECT pa.promocode_id,
		       p.keyword,
		       p.quantity,
		       pa.activated_at
		FROM promocode_activations pa
		JOIN promocodes p ON p.promocode_id = pa.promocode_id
		WHERE pa.user_id = $1
		ORDER BY pa.activated_at DESC
	`

	rows, err := db.DB.Query(query, userID)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "42703" {
			rows, err = db.DB.Query(`
				SELECT pa.promocode_id, p.keyword, p.quantity, pa.activated_at
				FROM promocode_activations pa
				JOIN promocodes p ON p.id = pa.promocode_id
				WHERE pa.user_id = $1
				ORDER BY pa.activated_at DESC
			`, userID)
		}
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	results := []promocodeActivationResponse{}
	for rows.Next() {
		var (
			item      promocodeActivationResponse
			quantity  interface{}
			activated sql.NullTime
		)
		if err := rows.Scan(&item.PromocodeID, &item.Keyword, &quantity, &activated); err != nil {
			return nil, err
		}

		q, convErr := normalizeQuantity(quantity)
		if convErr != nil {
			return nil, convErr
		}
		item.Quantity = q

		if activated.Valid {
			item.ActivatedAt = &activated.Time
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func findPromocodeByKeyword(keyword string) (*promocodeRecord, error) {
	record := &promocodeRecord{}

	var (
		quantity interface{}
		start    sql.NullTime
		end      sql.NullTime
	)

	err := db.DB.QueryRow(
		"SELECT id, name, quantity, start_time, end_time FROM promocode WHERE keyword = $1",
		keyword,
	).Scan(&record.ID, &record.Name, &quantity, &start, &end)

	switch {
	case err == nil:
		q, convErr := normalizeQuantity(quantity)
		if convErr != nil {
			return nil, convErr
		}
		record.Quantity = q
		if start.Valid {
			record.StartTime = &start.Time
		}
		if end.Valid {
			record.EndTime = &end.Time
		}
		record.IsActive = computePromocodeActive(record)
		return record, nil
	case errors.Is(err, sql.ErrNoRows):
		// continue to legacy lookup
	default:
		if pqErr, ok := err.(*pq.Error); ok {
			switch pqErr.Code {
			case "42P01", "42703":
				// legacy lookup below
			default:
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	legacyQuery := `
		SELECT promocode_id, quantity, is_active
		FROM promocodes
		WHERE keyword = $1
	`
	if err := db.DB.QueryRow(legacyQuery, keyword).Scan(&record.ID, &record.Quantity, &record.IsActive); err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			switch pqErr.Code {
			case "42703":
				if err := db.DB.QueryRow(
					"SELECT id, quantity, active FROM promocodes WHERE keyword = $1",
					keyword,
				).Scan(&record.ID, &record.Quantity, &record.IsActive); err != nil {
					return nil, err
				}
				return record, nil
			case "42P01":
				return nil, fmt.Errorf("promocodes table not found")
			}
		}
		return nil, err
	}

	return record, nil
}

func normalizeQuantity(value interface{}) (float64, error) {
	switch v := value.(type) {
	case nil:
		return 0, fmt.Errorf("quantity value is nil")
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case []byte:
		parsed, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported quantity type %T", value)
	}
}

func computePromocodeActive(record *promocodeRecord) bool {
	if record == nil {
		return false
	}

	now := time.Now().UTC()
	if record.StartTime != nil && now.Before(*record.StartTime) {
		return false
	}
	if record.EndTime != nil && now.After(*record.EndTime) {
		return false
	}

	return true
}

func generatePromocodeKeyword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be positive")
	}

	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	byteLength := length
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	for i := range bytes {
		bytes[i] = charset[int(bytes[i])%len(charset)]
	}

	return string(bytes), nil
}
