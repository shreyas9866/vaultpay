package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// GenerateAPIKey creates a cryptographically secure key and its bcrypt hash.
// It returns (rawKey, hash, error). The rawKey is shown to the user ONLY ONCE.
func GenerateAPIKey(prefix string) (string, string, error) {
	// 1. Generate 32 bytes of cryptographically secure random data
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// 2. Encode to a URL-safe Base64 string and attach the prefix (e.g., "sk_test_")
	secureString := base64.RawURLEncoding.EncodeToString(randomBytes)
	rawKey := fmt.Sprintf("%s_%s", prefix, secureString)

	// 3. Hash the raw key using bcrypt (DefaultCost is currently 10 rounds)
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("failed to hash api key: %w", err)
	}

	return rawKey, string(hashBytes), nil
}

// CheckAPIKeyHash securely compares a provided raw key against the stored hash
func CheckAPIKeyHash(rawKey, storedHash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(rawKey))
	return err == nil
}