package handlers

import (
	"encoding/json"
	"net/http"
)

// ProblemDetail represents a standard RFC 7807 error object
type ProblemDetail struct {
	Type     string `json:"type"`   // A URI reference that identifies the problem type
	Title    string `json:"title"`  // A short, human-readable summary
	Status   int    `json:"status"` // The HTTP status code
	Detail   string `json:"detail"` // A human-readable explanation specific to this occurrence
	Instance string `json:"instance,omitempty"` // A URI reference that identifies the specific occurrence
}

// RespondWithError is a helper to consistently send RFC 7807 errors
func RespondWithError(w http.ResponseWriter, status int, title, detail string) {
	// The official content type for this specific RFC
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	problem := ProblemDetail{
		Type:   "about:blank", // Can be updated later to point to your actual Swagger docs!
		Title:  title,
		Status: status,
		Detail: detail,
	}

	json.NewEncoder(w).Encode(problem)
}