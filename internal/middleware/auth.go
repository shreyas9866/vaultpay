package middleware

import (
	"net/http"
	"os"
	"strings"
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized: Missing API Key"}`))
			return
		}

		// 2. The header must look like "Bearer sk_test_..."
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized: Malformed Authorization header"}`))
			return
		}

		// 3. Compare the keys
		providedKey := parts[1]
		if providedKey != expectedKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized: Invalid API Key"}`))
			return
		}

		// 4. If the key matches, open the door!
		next.ServeHTTP(w, r)
	}
}
