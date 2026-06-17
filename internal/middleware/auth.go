package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/shreyas9866/vaultpay/internal/handlers"
)

// RequireAuth intercepts the request and checks for a valid Secret Key
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For local testing, we provide a default mock key if environment variable isn't set
		expectedKey := os.Getenv("VAULTPAY_SECRET_KEY")
		if expectedKey == "" {
			expectedKey = "sk_test_vaultpay123456789"
		}

		// 1. Grab the Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			handlers.RespondWithError(w, http.StatusUnauthorized, "Unauthorized", "Missing Authorization header. Please provide a valid Bearer token.")
			return
		}

		// 2. The header must look like "Bearer sk_test_..."
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			handlers.RespondWithError(w, http.StatusUnauthorized, "Malformed Header", "Authorization header must be in the format: Bearer <API_KEY>")
			return
		}

		// 3. Compare the keys
		providedKey := parts[1]
		if providedKey != expectedKey {
			handlers.RespondWithError(w, http.StatusUnauthorized, "Invalid API Key", "The provided API key is incorrect or has been revoked.")
			return
		}

		// 4. If the key matches, open the door!
		next(w, r)
	}
}