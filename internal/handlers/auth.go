package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/shreyas9866/vaultpay/internal/auth"
	"github.com/shreyas9866/vaultpay/internal/models"
)

// 1. UPDATED: Added GetUserByEmail to our interface contract
type AuthStore interface {
	CreateUser(ctx context.Context, user *models.User) error
	CreateAPIKey(ctx context.Context, apiKey *models.APIKey) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
}

// 2. UPDATED: Added the JWT Token Factory
type AuthHandler struct {
	store      AuthStore
	jwtManager *auth.JWTManager
}

// 3. UPDATED: Constructor now accepts the JWT Token Factory
func NewAuthHandler(store AuthStore, jwtManager *auth.JWTManager) *AuthHandler {
	return &AuthHandler{
		store:      store,
		jwtManager: jwtManager,
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