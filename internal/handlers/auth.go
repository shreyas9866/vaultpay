package handlers

import (
	"encoding/json"
	"net/http"
	"strings" 
	"time"    

	"github.com/redis/go-redis/v9"
	"github.com/shreyas9866/vaultpay/internal/auth"
	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/models" // <-- The missing link!
)

// AuthHandler handles all authentication routes
type AuthHandler struct {
	store       *database.Store
	jwtManager  *auth.JWTManager
	redisClient *redis.Client // <-- The missing Redis client!
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(store *database.Store, jwtManager *auth.JWTManager, redisClient *redis.Client) *AuthHandler {
	return &AuthHandler{
		store:       store,
		jwtManager:  jwtManager,
		redisClient: redisClient,
	}
}

type RegisterUserRequest struct {
	Email string `json:"email"`
}

type RegisterUserResponse struct {
	User   models.User   `json:"user"`
	APIKey models.APIKey `json:"api_key"`
	RawKey string        `json:"raw_key"` // Shown ONLY ONCE right here
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	user := models.User{Email: req.Email}
	if err := h.store.CreateUser(r.Context(), &user); err != nil {
		http.Error(w, "Failed to create user profile", http.StatusInternalServerError)
		return
	}

	rawKey, hash, err := auth.GenerateAPIKey("sk_test")
	if err != nil {
		http.Error(w, "Crypto sub-system engine failure", http.StatusInternalServerError)
		return
	}

	apiKey := models.APIKey{
		UserID:    user.ID,
		KeyPrefix: "sk_test",
		KeyHash:   hash,
	}

	if err := h.store.CreateAPIKey(r.Context(), &apiKey); err != nil {
		http.Error(w, "Failed to persist authentication keys", http.StatusInternalServerError)
		return
	}

	resp := RegisterUserResponse{
		User:   user,
		APIKey: apiKey,
		RawKey: rawKey,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// --- NEW: THE LOGIN ENDPOINT ---

type LoginRequest struct {
	Email string `json:"email"`
	// Note: In a real enterprise app, you would also accept and hash a password here!
}

type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// 1. Verify the user actually exists in our database
	user, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		http.Error(w, "Invalid credentials or user not found", http.StatusUnauthorized)
		return
	}

	// 2. MINT THE TOKENS! (Using your shiny new RSA Private Key)
	accessToken, refreshToken, err := h.jwtManager.GenerateTokens(user.ID)
	if err != nil {
		http.Error(w, "Failed to generate security tokens", http.StatusInternalServerError)
		return
	}

	// 3. Hand the keys to the user
	resp := LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
// Logout invalidates the current JWT by adding it to the Redis Blacklist
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// 1. Grab the auth header
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	tokenString := parts[1] // We know this is safe because the bouncer already checked it!

	// 2. Add the token to Redis with a 15-minute expiration
	// After 15 mins, the token naturally expires anyway, so Redis automatically cleans it up to save memory.
	err := h.redisClient.Set(r.Context(), tokenString, "revoked", 15*time.Minute).Err()
	if err != nil {
		http.Error(w, "Failed to process logout", http.StatusInternalServerError)
		return
	}

	// 3. Confirm the lockdown
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Successfully logged out. Token revoked."}`))
}
// Refresh handles rotating the access token using a valid refresh token
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	// 1. Parse the incoming JSON to grab the refresh token
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// 2. Mathematically verify the refresh token signature using the Bouncer logic
	claims, err := h.jwtManager.VerifyToken(req.RefreshToken)
	if err != nil {
		http.Error(w, "Invalid or expired refresh token", http.StatusUnauthorized)
		return
	}

	// 3. Extract the User ID from the valid token
	userID, ok := claims["sub"].(string)
	if !ok {
		http.Error(w, "Invalid token claims", http.StatusUnauthorized)
		return
	}

	// 4. Mint a fresh pair of keys! 
	// NOTE: If you called your token generation function something different in your Login handler, 
	// just update this line to match it!
	accessToken, newRefreshToken, err := h.jwtManager.GenerateTokens(userID) 
	if err != nil {
		http.Error(w, "Failed to generate new tokens", http.StatusInternalServerError)
		return
	}

	// 5. Send the new keys back to the client
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
	})
}