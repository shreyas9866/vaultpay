package main

import (
	"context"
	"log"
	"net/http"
	"os" // <-- NEW: Required for reading Environment Variables

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/handlers"
	vpmiddleware "github.com/shreyas9866/vaultpay/internal/middleware"
	"github.com/shreyas9866/vaultpay/internal/worker"
)

func main() {
	// --- 1. POSTGRES CONNECTION ---
	// NEW: Check Docker environment variables first, fallback to localhost
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://vaultpay_user:vaultpay_password@127.0.0.1:5432/vaultpay?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()
	log.Println("✅ Successfully connected to PostgreSQL!")

	// --- 2. REDIS CONNECTION ---
	// NEW: Check Docker environment variables first, fallback to localhost
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()
	log.Println("✅ Successfully connected to Redis!")

	// --- 3. INITIALIZE STORES & HANDLERS ---
	store := database.NewStore(db)

	chargeHandler := handlers.NewChargeHandler(store, rdb)
	authHandler := handlers.NewAuthHandler(store)

	webhookWorker := worker.NewWebhookWorker(store)
	go webhookWorker.Start(context.Background())

	rateLimiter := vpmiddleware.NewRateLimiter(rdb)

	// --- 4. SETUP ROUTER ---
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(rateLimiter.Limit)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("VaultPay API is online and ready for transactions!"))
	})

	r.Post("/v1/auth/keys", authHandler.Register)
	r.Post("/charges", chargeHandler.Create)
	r.Post("/v1/refunds", chargeHandler.Refund)

	// --- 5. START SERVER ---
	port := ":8080"
	log.Printf("🚀 Starting VaultPay server on port %s", port)

	if err := http.ListenAndServe(port, r); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}