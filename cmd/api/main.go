package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hibiken/asynq"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/handlers"
	vpmiddleware "github.com/shreyas9866/vaultpay/internal/middleware"
	"github.com/shreyas9866/vaultpay/internal/worker"

	// NEW: OpenTelemetry Imports
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	// --- 0. INITIALIZE TELEMETRY ---
	ctx := context.Background()
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint("jaeger:4318"), // Points to our Docker Jaeger container
	)
	if err != nil {
		log.Fatalf("❌ Failed to create OTel exporter: %v", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			"",
			attribute.String("service.name", "vaultpay-api"),
		)),
	)
	// Set global tracing configurations
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer tp.Shutdown(ctx)
	log.Println("✅ OpenTelemetry Tracer Initialized!")

	// --- 1. POSTGRES CONNECTION ---
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://vaultpay_user:vaultpay_password@127.0.0.1:5432/vaultpay?sslmode=disable"
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	// --- 2. REDIS CONNECTION ---
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()

	// --- 3. INITIALIZE STORES & QUEUES ---
	store := database.NewStore(db)
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	defer asynqClient.Close()

	chargeHandler := handlers.NewChargeHandler(store, rdb, asynqClient)
	authHandler := handlers.NewAuthHandler(store)

	asynqServer := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{Concurrency: 10, Queues: map[string]int{"default": 10}},
	)
	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TaskTypeWebhookDelivery, worker.ProcessWebhookDelivery)
	go asynqServer.Run(mux)

	rateLimiter := vpmiddleware.NewRateLimiter(rdb)

	// --- 4. SETUP ROUTER ---
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(rateLimiter.Limit)

	// NEW: Automatically trace all incoming HTTP requests
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewMiddleware("vaultpay-router")(next)
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("VaultPay API is online!"))
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