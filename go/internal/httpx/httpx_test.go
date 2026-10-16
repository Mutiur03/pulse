package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	msg := `bad "quote" and \ backslash`
	WriteError(w, 418, msg)
	if w.Code != 418 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("code %d type %q", w.Code, w.Header().Get("Content-Type"))
	}
	var b ErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || b.Error != msg {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, 201, map[string]int{"n": 1})
	if w.Code != 201 || w.Body.String() != "{\"n\":1}\n" {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
}
