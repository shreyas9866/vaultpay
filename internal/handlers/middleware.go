package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/shreyas9866/vaultpay/internal/auth"
)

// RequireJWT intercepts requests and checks for a valid Bearer token
func RequireJWT(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Check if they even brought an ID
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// 2. Ensure it's formatted as "Bearer <token>"
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid Authorization format. Expected 'Bearer <token>'", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]

			// 3. Ask the JWTManager to verify the cryptography
			claims, err := jwtManager.VerifyToken(tokenString)
			if err != nil {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// 4. Extract the User ID from the token and attach it to the request context
			// This way, the final handler knows exactly who is making the request!
			userID := claims["sub"].(string)
			ctx := context.WithValue(r.Context(), "userID", userID)

			// 5. Let them through to the vault
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}