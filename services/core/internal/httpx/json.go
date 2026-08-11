package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// maxRequestBody is the largest request body this service reads — both the largest [DecodeJSON]
// will hand to a handler and the largest [Idempotent] will fingerprint.
//
// It is one constant rather than two equal ones, and that is the point. Docs/10 §4.3 requires
// the two limits to agree, because the idempotency middleware reads and fingerprints the body
// before the handler ever sees it: a handler permitted the larger body would be replayed against
// a fingerprint computed over something it never read. Two constants can drift and a test can
// only notice afterwards; one cannot drift at all.
//
// The API takes JSON. Images go directly to object storage through pre-signed URLs and never
// through this service (Docs/06 §5.2), so nothing legitimate comes close to a megabyte.
const maxRequestBody = 1 << 20 // 1 MiB

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

// DecodeJSON reads a request body into v, refusing anything it does not fully understand.
//
// Unknown fields are rejected (Docs/10 §4.3). Responses stay additive so that an old build
// tolerates a new field, but a request is the other direction: a client that sends `pasword` has
// made a mistake that will otherwise look like a validation failure on a field it believes it
// supplied.
//
// Every failure it returns is already in the error contract, so a handler can return it
// unexamined:
//
//	var req createJobRequest
//	if err := httpx.DecodeJSON(r, &req); err != nil {
//	    return err
//	}
//
// Promoted from internal/identity at SHIP-15e — see [H] for why it was written there first.
func DecodeJSON(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONContentType(ct) {
		return NewError(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
			"This endpoint accepts application/json.")
	}

	if r.Body == nil {
		return NewError(http.StatusBadRequest, CodeBadRequest,
			"The request needs a JSON body.")
	}

	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody+1))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		var unknown *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			return NewError(http.StatusBadRequest, CodeBadRequest,
				"The request needs a JSON body.").WithCause(err)
		case errors.As(err, &unknown):
			return NewError(http.StatusBadRequest, CodeBadRequest,
				"The %s field is not the expected type.", unknown.Field).WithCause(err)
		case strings.Contains(err.Error(), "unknown field"):
			// encoding/json reports this as a plain error with no type of its own, so
			// there is nothing else to match on. The message is quoted rather than
			// reworded because it names the offending field.
			return NewError(http.StatusBadRequest, CodeBadRequest,
				"The request contains a field this endpoint does not accept: %s",
				strings.TrimPrefix(err.Error(), "json: ")).WithCause(err)
		default:
			return NewError(http.StatusBadRequest, CodeBadRequest,
				"The request body is not valid JSON.").WithCause(err)
		}
	}

	// A second value in the stream means the client sent two documents, which is never what
	// was intended and would otherwise be silently ignored.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return NewError(http.StatusBadRequest, CodeBadRequest,
			"The request body must be a single JSON object.")
	}
	return nil
}

func isJSONContentType(value string) bool {
	media, _, _ := strings.Cut(value, ";")
	return strings.EqualFold(strings.TrimSpace(media), "application/json")
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
