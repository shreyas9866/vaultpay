package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	
	// Make sure this matches your project path!
	"github.com/shreyas9866/vaultpay/internal/handlers"
)

// responseRecorder is a custom wrapper that secretly captures the response body and status code
type responseRecorder struct {
	http.ResponseWriter
	Status int
	Body   *bytes.Buffer
}

// WriteHeader intercepts the status code (like 200 OK or 400 Bad Request)
func (r *responseRecorder) WriteHeader(statusCode int) {
	r.Status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write intercepts the actual JSON body being sent back
func (r *responseRecorder) Write(b []byte) (int, error) {
	r.Body.Write(b)
	return r.ResponseWriter.Write(b)
}

// CachedResponse is the object we actually store inside Redis
type CachedResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// Idempotency checks Redis for duplicate requests and caches new responses
func Idempotency(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. We only care about POST requests (mutations like charging or refunding)
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			// 2. Grab the key from the headers
			idemKey := r.Header.Get("Idempotency-Key")
			if idemKey == "" {
				handlers.RespondWithError(w, http.StatusBadRequest, "Missing Idempotency Key", "The Idempotency-Key header is required for this request to prevent double-charges.")
				return
			}

			ctx := context.Background()
			redisKey := "idemp:" + idemKey

			// 3. Ask Redis: "Have we seen this key recently?"
			cachedBytes, err := rdb.Get(ctx, redisKey).Bytes()
			if err == nil {
				// 🛑 MATCH FOUND! Stop the request and return the cached response.
				var cached CachedResponse
				json.Unmarshal(cachedBytes, &cached)

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Idempotent-Replay", "true") // Custom header so the client knows it's a cached result
				w.WriteHeader(cached.Status)
				w.Write(cached.Body)
				return
			}

			// 4. NO MATCH: Create our secret recorder and let the request process normally
			rec := &responseRecorder{
				ResponseWriter: w,
				Status:         http.StatusOK, // Default starting point
				Body:           &bytes.Buffer{},
			}

			next.ServeHTTP(rec, r)

			// 5. ON THE WAY OUT: Save the intercepted response to Redis for 24 hours
			// (We only cache successes and client errors. We don't cache 500 Server Errors so they can retry those!)
			if rec.Status >= 200 && rec.Status < 500 {
				cached := CachedResponse{
					Status: rec.Status,
					Body:   rec.Body.Bytes(),
				}
				toSave, _ := json.Marshal(cached)
				rdb.Set(ctx, redisKey, toSave, 24*time.Hour)
			}
		})
	}
}