package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shreyas9866/vaultpay/internal/models"
	"github.com/stretchr/testify/assert"
)

// --- 1. THE FAKE AUTH DATABASE ---
type MockAuthStore struct{}

func (m *MockAuthStore) CreateUser(ctx context.Context, user *models.User) error {
	user.ID = "fake-user-uuid" // Pretend Postgres generated an ID
	return nil
}

func (m *MockAuthStore) CreateAPIKey(ctx context.Context, apiKey *models.APIKey) error {
	apiKey.ID = "fake-key-uuid"
	return nil
}
// ---------------------------------

func TestAuthHandler_Register(t *testing.T) {
	// 1. Inject the fake database
	mockDB := &MockAuthStore{}
	handler := NewAuthHandler(mockDB)

	t.Run("Successful Registration", func(t *testing.T) {
		// 2. Create the test payload
		payload := []byte(`{"email": "test@vaultpay.io"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/keys", bytes.NewBuffer(payload))

		// 3. Execute the handler
		rr := httptest.NewRecorder()
		handler.Register(rr, req)

		// 4. Assert success
		assert.Equal(t, http.StatusCreated, rr.Code)
		
		// The response should contain the raw Stripe-style key we generated
		assert.Contains(t, rr.Body.String(), "sk_test_")
		assert.Contains(t, rr.Body.String(), "test@vaultpay.io")
	})
	
	t.Run("Missing Email", func(t *testing.T) {
		payload := []byte(`{"email": ""}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/keys", bytes.NewBuffer(payload))

		rr := httptest.NewRecorder()
		handler.Register(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Contains(t, rr.Body.String(), "Email is required")
	})
}