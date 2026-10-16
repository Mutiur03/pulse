// Package httpx holds small HTTP response helpers shared by the services.
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ErrorBody is the JSON shape of every error response: {"error": "..."}.
type ErrorBody struct {
	Error string `json:"error"`
}

// WriteError writes {"error": msg} with the given status code. The message
// is JSON-encoded, so quotes and backslashes in it are safe.
func WriteError(w http.ResponseWriter, code int, msg string) {
	WriteJSON(w, code, ErrorBody{Error: msg})
}
