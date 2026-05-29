package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shreyas9866/vaultpay/internal/auth"
	"github.com/shreyas9866/vaultpay/internal/database"
	"github.com/shreyas9866/vaultpay/internal/models"
)

type AuthHandler struct {
	store *database.Store
}

func NewAuthHandler(store *database.Store) *AuthHandler {
	return &AuthHandler{store: store}
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

	// 1. Create the user entity
	user := models.User{Email: req.Email}
	if err := h.store.CreateUser(r.Context(), &user); err != nil {
		http.Error(w, "Failed to create user profile", http.StatusInternalServerError)
		return
	}

	// 2. Generate the Stripe-style secure key pair using our crypto engine
	rawKey, hash, err := auth.GenerateAPIKey("sk_test")
	if err != nil {
		http.Error(w, "Crypto sub-system engine failure", http.StatusInternalServerError)
		return
	}

	// 3. Prepare the key database model with the secure hash
	apiKey := models.APIKey{
		UserID:    user.ID,
		KeyPrefix: "sk_test",
		KeyHash:   hash,
	}

	// 4. Persist the key metadata and hash to PostgreSQL
	if err := h.store.CreateAPIKey(r.Context(), &apiKey); err != nil {
		http.Error(w, "Failed to persist authentication keys", http.StatusInternalServerError)
		return
	}

	// 5. Build the complete response payload
	resp := RegisterUserResponse{
		User:   user,
		APIKey: apiKey,
		RawKey: rawKey,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}