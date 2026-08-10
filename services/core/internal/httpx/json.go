package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON serialises v as the response body with the given status.
//
// Encoding happens into a buffer before anything is written, so a value that fails to
// marshal produces a clean 500 rather than a 200 followed by a truncated body — the
// latter being far harder to diagnose from the client's end.
//
// This writes success bodies. Failures go through [WriteError], which puts them in the
// standard error contract (SHIP-12).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// The fallback is a literal rather than a marshalled errorEnvelope, because the
		// one thing already established here is that marshalling can fail.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error",` +
			`"message":"Something went wrong at our end."}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Chain applies middleware to h so that the first argument is the outermost wrapper, and
// therefore the first to see a request.
//
// Order matters here: RequestID must wrap Logger for the log record to carry an ID, and
// Recover must sit inside RequestID so a panic is still attributable to a request.
func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}
