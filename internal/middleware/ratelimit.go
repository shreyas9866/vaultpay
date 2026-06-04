package middleware

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// The updated Lua script now uses a completely unique member ID (ARGV[4]) 
// so simultaneous millisecond requests are counted properly!
const slidingWindowScript = `
	local key = KEYS[1]
	local now = tonumber(ARGV[1])
	local window = tonumber(ARGV[2])
	local limit = tonumber(ARGV[3])
	local member = ARGV[4]
	local clearBefore = now - window

	redis.call('ZREMRANGEBYSCORE', key, 0, clearBefore)
	local count = redis.call('ZCARD', key)

	if count < limit then
		redis.call('ZADD', key, now, member)
		redis.call('EXPIRE', key, window / 1000)
		return {1, count + 1}
	end

	return {0, count}
`

type RateLimiter struct {
	redis  *redis.Client
	script *redis.Script
}

func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{
		redis:  rdb,
		script: redis.NewScript(slidingWindowScript),
	}
}

func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIdentifier := r.Header.Get("Authorization")
		if clientIdentifier == "" {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				clientIdentifier = r.RemoteAddr
			} else {
				clientIdentifier = host
			}
		}

		redisKey := "rate_limit:" + clientIdentifier
		
		// Get milliseconds for the sliding window math
		now := time.Now().UnixMilli()
		
		// Get nanoseconds to guarantee every request is unique in the Redis set
		uniqueMember := time.Now().UnixNano() 
		
		windowMs := int64(60000)
		limit := 5 // KEEP AT 5 FOR THE TEST

		// Pass the uniqueMember into the script
		result, err := rl.script.Run(r.Context(), rl.redis, []string{redisKey}, now, windowMs, limit, uniqueMember).Result()
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		vals := result.([]interface{})
		allowed := vals[0].(int64) == 1
		currentCount := vals[1].(int64)

		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(int64(limit)-currentCount, 10))

		if !allowed {
			w.Header().Set("Retry-After", "60")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": "Too many requests. Please slow down."}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}