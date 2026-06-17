package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/hibiken/asynq"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/handlers"
	"github.com/shreyas9866/vaultpay/internal/metrics"
	vpmiddleware "github.com/shreyas9866/vaultpay/internal/middleware"
	"github.com/shreyas9866/vaultpay/internal/worker"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	ctx := context.Background()
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint("jaeger:4318"),
	)
	if err != nil {
		log.Fatalf("❌ Failed to create OTel exporter: %v", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes("", attribute.String("service.name", "vaultpay-api"))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer tp.Shutdown(ctx)
	log.Println("✅ OpenTelemetry Tracer Initialized!")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://vaultpay_user:vaultpay_password@127.0.0.1:5432/vaultpay?sslmode=disable"
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	
	// NEW: Grab the password from the cloud environment
	redisPass := os.Getenv("REDIS_PASSWORD") 

	// NEW: Pass the password to the Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPass, 
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	defer rdb.Close()

	store := database.NewStore(db)
	
	// NEW: Create a shared Redis option config that includes the password for Asynq
	asynqRedisOpt := asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: redisPass,
	}

	asynqClient := asynq.NewClient(asynqRedisOpt)
	defer asynqClient.Close()

	chargeHandler := handlers.NewChargeHandler(store, rdb, asynqClient)
	authHandler := handlers.NewAuthHandler(store)

	asynqServer := asynq.NewServer(asynqRedisOpt, asynq.Config{Concurrency: 10, Queues: map[string]int{"default": 10}})
	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TaskTypeWebhookDelivery, worker.ProcessWebhookDelivery)
	go asynqServer.Run(mux)

	rateLimiter := vpmiddleware.NewRateLimiter(rdb)

	r := chi.NewRouter()

	// --- 1. GLOBAL MIDDLEWARES (MUST COME FIRST) ---
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"}, // Allows browser-based testing
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Idempotency-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(rateLimiter.Limit)

	// Prometheus Metrics Middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, req.ProtoMajor)
			next.ServeHTTP(ww, req)

			// Don't track the /metrics or /health endpoint itself to keep data clean
			if req.URL.Path != "/metrics" && req.URL.Path != "/health" {
				duration := time.Since(start).Seconds()
				status := strconv.Itoa(ww.Status())
				metrics.RequestsTotal.WithLabelValues(req.Method, req.URL.Path, status).Inc()
				metrics.RequestDuration.WithLabelValues(req.Method, req.URL.Path).Observe(duration)
			}
		})
	})

	r.Use(func(next http.Handler) http.Handler { return otelhttp.NewMiddleware("vaultpay-router")(next) })

	// --- 2. INITIALIZE HANDLERS ---
	subHandler := handlers.NewSubscriptionHandler(store)

	// --- 3. ROUTES (MUST COME AFTER ALL MIDDLEWARES) ---
	
	// Expose the scorecard to Prometheus
	r.Handle("/metrics", promhttp.Handler())

	// Public Routes
	r.Get("/health", func(w http.ResponseWriter, req *http.Request) { w.Write([]byte("VaultPay API is online!")) })
	r.Post("/v1/auth/keys", authHandler.Register)

	// 🔒 Secured Routes Protected by the Master API Key
	r.Post("/v1/charges", vpmiddleware.RequireAuth(chargeHandler.Create))
	r.Post("/v1/charges/{id}/refund", vpmiddleware.RequireAuth(chargeHandler.Refund))
	r.Get("/v1/charges/{id}/timeline", vpmiddleware.RequireAuth(chargeHandler.GetTimeline))
	
	// Subscriptions can remain unprotected for now or wrapped later
	r.Post("/v1/subscriptions/upgrade", subHandler.Upgrade)

	// --- 4. START THE SERVER ---
	
	// Look for the cloud platform's assigned port, default to 8080 locally
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Starting VaultPay server on port %s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
