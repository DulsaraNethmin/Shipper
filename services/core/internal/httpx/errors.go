package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// The API error contract (SHIP-12).
//
// Every failure this service returns — from a validation error to a panic to net/http's
// own 404 — has the same shape:
//
//	{
//	  "error": {
//	    "code": "validation_failed",
//	    "message": "The job could not be published.",
//	    "request_id": "9f2c1b...",
//	    "details": [
//	      {"field": "pickup.postcode", "code": "invalid_format", "message": "..."}
//	    ]
//	  }
//	}
//
// # Why the code and not the message
//
// The message is for a person reading a log or a support ticket. The code is what a client
// branches on, and it is the only part of the body a client may depend on: messages get
// reworded, translated, and made friendlier, and a Flutter build that switched on message
// text would break on a copy edit — from a store build already installed on devices, with
// no over-the-air fix available (Docs/06 §5.3).
//
// # Why the request ID is in the body as well as the header
//
// A user reports a problem with a screenshot, and support needs the one string that finds
// the request in Datadog (SHIP-174). A header does not survive a screenshot.
//
// # What is never in here
//
// No stack trace, no SQL, no panic message, no internal identifier the caller did not
// already have. The cause is attached with [Error.WithCause] and reaches the log, never
// the response.

// Code is the machine-readable error code. Codes are lower_snake_case, stable once
// published, and are part of the API contract in the same way a field name is.
type Code string

const (
	// CodeBadRequest is a malformed request: unparseable JSON, a missing header, a
	// path parameter that is not the shape it must be.
	CodeBadRequest Code = "bad_request"

	// CodeValidationFailed is a well-formed request the domain rejects. It is the code
	// that carries Details, one entry per offending field.
	CodeValidationFailed Code = "validation_failed"

	// CodeUnauthenticated means no usable credential was presented. Distinct from
	// CodeForbidden so a client knows whether to refresh a token or to give up.
	CodeUnauthenticated Code = "unauthenticated"

	// CodeForbidden means the caller is known and is not permitted. Used where the
	// caller may know the resource exists — their own job, someone else's bid.
	CodeForbidden Code = "forbidden"

	// CodeNotFound covers both "no such resource" and "not yours to see", wherever
	// distinguishing the two would confirm the existence of something the caller has no
	// business knowing about.
	CodeNotFound Code = "not_found"

	CodeMethodNotAllowed Code = "method_not_allowed"

	// CodeConflict is a request that is valid but contradicts the current state — a bid
	// placed on a job that has just been awarded, a second award on the same job.
	CodeConflict Code = "conflict"

	CodeUnsupportedMediaType Code = "unsupported_media_type"
	CodePayloadTooLarge      Code = "payload_too_large"

	// CodeRateLimited is returned with a Retry-After header wherever one can be given
	// honestly (SHIP-47, SHIP-183).
	CodeRateLimited Code = "rate_limited"

	// CodeInternal is the only code for a failure the caller cannot act on. It carries
	// no detail beyond the request ID, which is the thing that makes it diagnosable.
	CodeInternal Code = "internal_error"

	// CodeUnavailable means a dependency this request needs is not answering. Unlike
	// CodeInternal it says "try again", and it is what a middleware returns rather than
	// proceeding without the guarantee it exists to provide.
	CodeUnavailable Code = "service_unavailable"
)

// errorEnvelope is the wire format. The single "error" member keeps failure and success
// bodies structurally distinct, so no client can mistake one for the other.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      Code         `json:"code"`
	Message   string       `json:"message"`
	RequestID string       `json:"request_id,omitempty"`
	Details   []FieldError `json:"details,omitempty"`
}

// FieldError names one thing wrong with a request, so a client can put the message beside
// the input that caused it rather than at the top of the form.
type FieldError struct {
	// Field is a dotted path into the request body — "pickup.postcode",
	// "goods.weight_kg" — matching the JSON the client sent, not the Go field name.
	Field   string `json:"field"`
	Code    Code   `json:"code,omitempty"`
	Message string `json:"message"`
}

// Error is a failure with an HTTP status and an API code attached. Handlers return it;
// [WriteError] serialises it.
type Error struct {
	Status  int
	Code    Code
	Message string
	Details []FieldError

	// cause is the underlying failure. It is logged and never serialised, which is what
	// lets a handler attach a database error to a 500 without leaking the schema.
	cause error
}

// NewError builds an API error. The message is user-facing: write it in Australian English
// as something a person could act on, and keep anything internal for WithCause.
func NewError(status int, code Code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, a...)}
}

// WithDetails attaches per-field failures, which is what makes CodeValidationFailed useful
// to a client rather than merely accurate.
func (e *Error) WithDetails(details ...FieldError) *Error {
	e.Details = append(e.Details, details...)
	return e
}

// WithCause attaches the underlying failure for the log. It is never serialised.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s (%d %s): %v", e.Code, e.Status, e.Message, e.cause)
	}
	return fmt.Sprintf("%s (%d %s)", e.Code, e.Status, e.Message)
}

// Unwrap exposes the cause to errors.Is and errors.As, so a handler can attach a sentinel
// error and something further out can still recognise it.
func (e *Error) Unwrap() error { return e.cause }

// WriteError serialises err in the standard shape.
//
// A *[Error] is written as itself. Anything else becomes an opaque 500: an error that has
// not been given a status and a code has not been considered, and guessing on its behalf
// is how an internal message reaches a client.
//
// The unmapped error is logged on its way to that 500 (SHIP-15i). The *response* is
// unchanged and deliberately says nothing — but until this existed the service said nothing
// either, recording `status 500` and no cause at all, which is how a sign-in against a stale
// local database cost an afternoon with the handler as the only route to the answer.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr, ok := err.(*Error)
	if !ok {
		apiErr = StatusError(http.StatusInternalServerError)
		logUnmapped(r, err)
	}

	body := errorEnvelope{Error: errorBody{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Details: apiErr.Details,
	}}
	if r != nil {
		body.Error.RequestID = RequestIDFrom(r.Context())
	}

	WriteJSON(w, apiErr.Status, body)
}

// logUnmapped records the cause of a fallback 500, which is the one path where the platform
// has no more idea than the client does about what went wrong.
//
// It logs through [LoggerFrom], so the record carries the request ID the middleware bound
// (SHIP-14) and a support ticket quoting that ID finds the cause rather than only the status.
// A handler that builds its error with [NewError] and [Error.WithCause] is already saying what
// happened; this is for the errors nobody considered, which are exactly the ones worth seeing.
//
// r may be nil — [WriteError] already guards its request ID for that, and a caller writing an
// error outside a request must not lose the cause as well as the correlation. With no request
// there is no request-scoped logger either, so the record goes to slog.Default.
func logUnmapped(r *http.Request, err error) {
	ctx := context.Background()

	attrs := []slog.Attr{slog.Any("error", err)}
	if r != nil {
		ctx = r.Context()
		attrs = append(attrs, slog.String("method", r.Method))
		if r.URL != nil {
			// The path only. The query string is left out for the reason Logger gives:
			// it is the part of a URL most likely to carry a token or an address.
			attrs = append(attrs, slog.String("path", r.URL.Path))
		}
	}

	LoggerFrom(ctx).LogAttrs(ctx, slog.LevelError, "unmapped error written as 500", attrs...)
}

// StatusError builds the default error for a status code, for the cases where there is
// nothing to add beyond what the status already says.
func StatusError(status int) *Error {
	return &Error{Status: status, Code: CodeForStatus(status), Message: messageForStatus(status)}
}

// CodeForStatus is the default code for a status code.
//
// It exists so that a response produced by net/http rather than by a handler — ServeMux's
// 404 and 405, chiefly — still arrives with a code a client can branch on. A handler that
// knows more should say so with an explicit code.
func CodeForStatus(status int) Code {
	switch status {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusMethodNotAllowed:
		return CodeMethodNotAllowed
	case http.StatusConflict:
		return CodeConflict
	case http.StatusRequestEntityTooLarge:
		return CodePayloadTooLarge
	case http.StatusUnsupportedMediaType:
		return CodeUnsupportedMediaType
	case http.StatusUnprocessableEntity:
		return CodeValidationFailed
	case http.StatusTooManyRequests:
		return CodeRateLimited
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return CodeUnavailable
	default:
		if status >= 500 {
			return CodeInternal
		}
		return CodeBadRequest
	}
}

// messageForStatus gives each default code a sentence rather than a status name.
//
// "Not Found" tells a developer nothing they did not have from the status line, and it is
// the string that ends up in front of a user when a client renders the message it was
// given.
func messageForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "The request could not be understood."
	case http.StatusUnauthorized:
		return "Sign in to continue."
	case http.StatusForbidden:
		return "You do not have access to this."
	case http.StatusNotFound:
		return "There is nothing here."
	case http.StatusMethodNotAllowed:
		return "That method is not allowed on this endpoint."
	case http.StatusConflict:
		return "This conflicts with the current state."
	case http.StatusRequestEntityTooLarge:
		return "The request is too large."
	case http.StatusUnsupportedMediaType:
		return "This endpoint accepts application/json."
	case http.StatusTooManyRequests:
		return "Too many requests. Try again shortly."
	case http.StatusServiceUnavailable:
		return "The service is temporarily unavailable. Try again shortly."
	default:
		if status >= 500 {
			return "Something went wrong at our end. Quote the request ID if you contact support."
		}
		return http.StatusText(status)
	}
}

// StandardErrors rewrites failures that did not come from a handler into the contract.
//
// net/http produces plain-text errors of its own — ServeMux answers an unmatched path with
// "404 page not found" and a method mismatch with "405 Method Not Allowed" — and so does
// any code reaching for http.Error. A client parsing every response as JSON then gets a
// syntax error instead of a code, on exactly the routes it is most likely to hit while
// being written.
//
// Rewriting them here rather than registering catch-all handlers keeps ServeMux's own
// matching intact, including the Allow header it sets on a 405. Only the body changes.
//
// A response whose Content-Type is already JSON is left alone: that came from a handler
// that has said what it means.
func StandardErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&errorNormaliser{ResponseWriter: w, req: r}, r)
	})
}

// errorNormaliser replaces a non-JSON error body with the standard one.
type errorNormaliser struct {
	http.ResponseWriter
	req *http.Request

	replacing bool
	wrote     bool
}

func (n *errorNormaliser) WriteHeader(status int) {
	if n.wrote {
		return
	}
	n.wrote = true

	if status < 400 || isJSON(n.Header().Get("Content-Type")) {
		n.ResponseWriter.WriteHeader(status)
		return
	}

	// From here the original body is discarded. Content-Length would describe it rather
	// than the replacement, so it has to go before anything is written.
	n.Header().Del("Content-Length")
	n.replacing = true
	WriteError(n.ResponseWriter, n.req, StatusError(status))
}

func (n *errorNormaliser) Write(b []byte) (int, error) {
	if !n.wrote {
		n.WriteHeader(http.StatusOK)
	}
	if n.replacing {
		// Claim the write so the caller sees no short-write error, and drop it: the
		// standard body has already been sent in its place.
		return len(b), nil
	}
	return n.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer through this wrapper.
func (n *errorNormaliser) Unwrap() http.ResponseWriter { return n.ResponseWriter }

func isJSON(contentType string) bool {
	return strings.HasPrefix(contentType, "application/json")
}
