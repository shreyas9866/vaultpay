package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	// Add our internal packages here
	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/handlers"
)

func main() {
	// --- 1. POSTGRES CONNECTION ---
	dsn := "postgres://vaultpay_user:vaultpay_password@127.0.0.1:5432/vaultpay?sslmode=disable"

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()
	log.Println("✅ Successfully connected to PostgreSQL!")

	// --- 2. REDIS CONNECTION ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379", // Matches our docker-compose port
		Password: "",               // No password set in docker-compose
		DB:       0,                // Default DB
	})

	// Ping Redis to ensure it's alive
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()
	log.Println("✅ Successfully connected to Redis!")

	// --- 3. INITIALIZE STORES & HANDLERS ---
	store := database.NewStore(db)
	
	// Notice we are now passing Redis into our ChargeHandler
	chargeHandler := handlers.NewChargeHandler(store, rdb)
	authHandler := handlers.NewAuthHandler(store)

	// --- 4. SETUP ROUTER ---
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("VaultPay API is online and ready for transactions!"))
	})

	// Register our API routes
	r.Post("/v1/auth/keys", authHandler.Register)
	r.Post("/charges", chargeHandler.Create)

	// --- 5. START SERVER ---
	port := ":8080"
	log.Printf("🚀 Starting VaultPay server on port %s", port)

	if err := http.ListenAndServe(port, r); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}