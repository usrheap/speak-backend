package handlers

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"speak/db"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/ssh"
)

type LoginViaEmailRequest struct {
	Email string `json:"email"`
}

func LoginViaEmail(c *fiber.Ctx) error {
	var req LoginViaEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}

	if req.Email == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Email is required"})
	}

	// Check if email exists in users table
	var userID int64
	err := db.DB.QueryRow("SELECT user_id FROM users WHERE email = $1", req.Email).Scan(&userID)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Email not found"})
	}
	if err != nil {
		fmt.Printf("Error checking email: %v\n", err)
		return c.Status(500).JSON(fiber.Map{
			"error":   "Database error",
			"details": err.Error(),
		})
	}

	// Start transaction
	tx, err := db.DB.Begin()
	if err != nil {
		fmt.Printf("Failed to begin transaction: %v\n", err)
		return c.Status(500).JSON(fiber.Map{
			"error":   "Database error",
			"details": err.Error(),
		})
	}
	defer tx.Rollback()

	// Delete any existing verification for this user
	_, err = tx.Exec("DELETE FROM verifications WHERE user_id = $1", userID)
	if err != nil {
		fmt.Printf("Failed to delete existing verification: %v\n", err)
		return c.Status(500).JSON(fiber.Map{
			"error":   "Database error",
			"details": err.Error(),
		})
	}

	// Generate 6-digit code
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	code := fmt.Sprintf("%06d", rng.Intn(900000)+100000)

	// Set expiration to 10 minutes from now
	expireTime := time.Now().Add(10 * time.Minute)

	// Insert verification
	_, err = tx.Exec(
		"INSERT INTO verifications (user_id, email, issue_time, expire_time, type, code) VALUES ($1, $2, $3, $4, $5, $6)",
		userID, req.Email, time.Now(), expireTime, "email", code,
	)
	if err != nil {
		fmt.Printf("Failed to create verification: %v\n", err)
		return c.Status(500).JSON(fiber.Map{
			"error":   "Failed to create verification",
			"details": err.Error(),
		})
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		fmt.Printf("Failed to commit transaction: %v\n", err)
		return c.Status(500).JSON(fiber.Map{
			"error":   "Database error",
			"details": err.Error(),
		})
	}

	// Send verification email
	if err := sendLoginVerificationEmail(req.Email, code); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Failed to send email: %v\n", err)
	}

	return c.JSON(fiber.Map{"message": "Verification code sent to email"})
}

func sendLoginVerificationEmail(to, code string) error {
	sshHost := os.Getenv("SSH_HOST")
	sshUser := os.Getenv("SSH_USER")
	sshPassword := os.Getenv("SSH_PASSWORD")
	sshPort := os.Getenv("SSH_PORT")
	if sshPort == "" {
		sshPort = "22"
	}

	// SSH config
	config := &ssh.ClientConfig{
		User: sshUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(sshPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// Connect to SSH server
	addr := fmt.Sprintf("%s:%s", sshHost, sshPort)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer client.Close()

	// Create session
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	// Create modern, professional email content
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background-color:#f5f7fa;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">
<table role="presentation" style="width:100%%;border-collapse:collapse;border-spacing:0;background-color:#f5f7fa;padding:40px 20px;">
<tr>
<td align="center" style="padding:0;">
<table role="presentation" style="max-width:600px;width:100%%;background-color:#ffffff;border-radius:12px;box-shadow:0 2px 8px rgba(0,0,0,0.08);overflow:hidden;">
<tr>
<td style="padding:48px 40px;text-align:center;background:linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);">
<h1 style="margin:0;color:#ffffff;font-size:28px;font-weight:600;letter-spacing:-0.5px;">SpeakAllRight</h1>
</td>
</tr>
<tr>
<td style="padding:48px 40px;">
<h2 style="margin:0 0 16px 0;color:#1a202c;font-size:24px;font-weight:600;line-height:1.3;">Login Verification</h2>
<p style="margin:0 0 32px 0;color:#4a5568;font-size:16px;line-height:1.6;">Please use the verification code below to complete your login:</p>
<div style="background-color:#f7fafc;border:2px dashed #cbd5e0;border-radius:8px;padding:24px;margin:32px 0;text-align:center;">
<div style="font-size:36px;font-weight:700;color:#667eea;letter-spacing:8px;font-family:'Courier New',monospace;line-height:1.2;">%s</div>
</div>
<p style="margin:16px 0 0 0;color:#718096;font-size:14px;line-height:1.5;">This code will expire in <strong style="color:#4a5568;">10 minutes</strong> for security reasons.</p>
</td>
</tr>
<tr>
<td style="padding:32px 40px;background-color:#f7fafc;border-top:1px solid #e2e8f0;">
<p style="margin:0 0 8px 0;color:#718096;font-size:14px;line-height:1.5;">Didn't request this code? You can safely ignore this email.</p>
<p style="margin:16px 0 0 0;color:#718096;font-size:14px;line-height:1.5;">Need help? <a href="mailto:support@speakallright.uz" style="color:#667eea;text-decoration:none;font-weight:500;">Contact Support</a></p>
</td>
</tr>
</table>
</td>
</tr>
</table>
</body>
</html>`, code)
	
	textContent := fmt.Sprintf("SpeakAllRight - Login Verification\n\nYour login verification code is: %s\n\nThis code will expire in 10 minutes.\n\nNeed help? Contact support@speakallright.uz", code)

	// Use Python to send email properly - write HTML to avoid escaping issues
	pythonScript := fmt.Sprintf(`python3 << 'PYEOF'
import smtplib
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText

html_content = """%s"""

text_content = """%s"""

msg = MIMEMultipart('alternative')
msg['From'] = 'SpeakAllRight <noreply@speakallright.uz>'
msg['To'] = '%s'
msg['Subject'] = 'Login to your SpeakAllRight account'
msg['List-Unsubscribe'] = '<mailto:support@speakallright.uz>'
msg['X-Entity-Type'] = 'transactional'

part1 = MIMEText(text_content, 'plain')
part2 = MIMEText(html_content, 'html')

msg.attach(part1)
msg.attach(part2)

s = smtplib.SMTP('localhost', 25)
s.sendmail(msg['From'], [msg['To']], msg.as_string())
s.quit()
PYEOF`, strings.ReplaceAll(htmlContent, `"""`, `\"\"\"`), textContent, to)
	
	err = session.Run(pythonScript)
	if err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}

	return nil
}

