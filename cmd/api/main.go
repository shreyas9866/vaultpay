package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // The underscore means we import it for its side-effects (registering the driver)
)

func main() {
	// 1. Database Connection String (Matches our docker-compose.yml credentials)
	dsn := "postgres://vaultpay_user:vaultpay_password@localhost:5432/vaultpay?sslmode=disable"

	// 2. Connect to PostgreSQL and ping it to ensure it's alive
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close() // Ensures the connection closes when the application stops

	log.Println("✅ Successfully connected to PostgreSQL!")

	// 3. Initialize the Chi Router
	r := chi.NewRouter()

	// 4. Add Built-in Middleware
	r.Use(middleware.Logger)    // Logs every API request to the terminal
	r.Use(middleware.Recoverer) // Prevents the API from crashing if a handler panics

	// 5. Create a Health Check Route
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("VaultPay API is online and ready for transactions!"))
	})

	// 6. Start the Server
	port := ":8080"
	log.Printf("🚀 Starting VaultPay server on port %s", port)
	
	if err := http.ListenAndServe(port, r); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}