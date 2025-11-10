package main

import (
	"log"
	"speak/db"
	"speak/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("Warning: Could not load .env file: %v", err)
	}

	// Initialize database
	if err := db.Init(); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.DB.Close()

	app := fiber.New()

	// Configure CORS to allow requests from frontend
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "http://localhost,http://localhost:80,http://localhost:8000,http://62.171.170.236,http://62.171.170.236:80,http://62.171.170.236:8080",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: true,
	}))

	app.Get("/api/alive", handlers.Alive)
	app.Post("/api/registerviaemail", handlers.RegisterViaEmail)
	app.Post("/api/verifyemail", handlers.VerifyEmail)
	app.Post("/api/loginviaemail", handlers.LoginViaEmail)
	app.Post("/api/loginviaemailverify", handlers.LoginViaEmailVerify)
	app.Post("/api/tokenverify", handlers.TokenVerify)

	app.Listen(":3000")
}
