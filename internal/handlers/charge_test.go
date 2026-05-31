package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/shreyas9866/vaultpay/internal/models"
	"github.com/stretchr/testify/assert"
)

// --- 1. THE FAKE DATABASE ---
type MockStore struct{}

// This perfectly matches the ChargeStore interface contract!
func (m *MockStore) CreateCharge(ctx context.Context, charge *models.Charge) error {
	// We just pretend it saved successfully and return no errors
	return nil
}
// ----------------------------

func TestChargeHandler_Create_Validation(t *testing.T) {
	tests := []struct {
		name           string
		idempotencyKey string
		payload        string
		expectedCode   int
		expectedError  string
	}{
		{
			name:           "Missing Idempotency Key",
			idempotencyKey: "",
			payload:        `{"amount": 1000, "currency": "USD"}`,
			expectedCode:   http.StatusBadRequest,
			expectedError:  "Missing Idempotency-Key header",
		},
		{
			name:           "Invalid JSON",
			idempotencyKey: "idemp_test_123",
			payload:        `{"amount": 1000, "currency": }`, 
			expectedCode:   http.StatusBadRequest,
			expectedError:  "Invalid JSON payload",
		},
		{
			name:           "Negative Amount",
			idempotencyKey: "idemp_test_123",
			payload:        `{"amount": -500, "currency": "USD"}`,
			expectedCode:   http.StatusBadRequest,
			expectedError:  "Amount must be greater than zero",
		},
		{
			name:           "Invalid Currency Code",
			idempotencyKey: "idemp_test_123",
			payload:        `{"amount": 1000, "currency": "US"}`, 
			expectedCode:   http.StatusBadRequest,
			expectedError:  "Currency must be a 3-letter ISO code",
		},
		// --- 2. NEW: THE HAPPY PATH ---
		{
			name:           "Successful Charge Creation",
			idempotencyKey: "idemp_success_999",
			payload:        `{"amount": 5000, "currency": "USD"}`,
			expectedCode:   http.StatusCreated,
			expectedError:  `"amount":5000`, // We expect the JSON response to contain the amount
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh Redis Mock
			rdb, mock := redismock.NewClientMock()
			if tt.idempotencyKey != "" {
				mock.ExpectSetNX("idemp:charge:"+tt.idempotencyKey, "processing", 24*time.Hour).SetVal(true)
			}

			// 3. INJECT THE FAKE DATABASE
			mockDB := &MockStore{}
			handler := NewChargeHandler(mockDB, rdb)

			req := httptest.NewRequest(http.MethodPost, "/charges", bytes.NewBuffer([]byte(tt.payload)))
			if tt.idempotencyKey != "" {
				req.Header.Set("Idempotency-Key", tt.idempotencyKey)
			}

			rr := httptest.NewRecorder()
			handler.Create(rr, req)

			assert.Equal(t, tt.expectedCode, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.expectedError)
			
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("there were unfulfilled expectations: %s", err)
			}
		})
	}
}