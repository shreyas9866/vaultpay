package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/shreyas9866/vaultpay/internal/auth"
)

// RequireJWT intercepts requests and checks for a valid, non-blacklisted Bearer token
func RequireJWT(jwtManager *auth.JWTManager, redisClient *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid Authorization format", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]

			// --- NEW: REDIS BLACKLIST CHECK ---
			// Check if this exact token string exists in our Redis vault
			isBlacklisted, err := redisClient.Exists(r.Context(), tokenString).Result()
			if err == nil && isBlacklisted > 0 {
				http.Error(w, "Token has been revoked (Logged Out)", http.StatusUnauthorized)
				return
			}
			// ----------------------------------

			claims, err := jwtManager.VerifyToken(tokenString)
			if err != nil {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			userID := claims["sub"].(string)
			ctx := context.WithValue(r.Context(), "userID", userID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}