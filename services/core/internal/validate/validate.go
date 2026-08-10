// Package validate builds field-level validation errors in the shape the API already
// promises.
//
// Docs/10 §4.6 rejects a struct-tag validation library for two reasons. The error contract
// from SHIP-12 already specifies dotted JSON paths with their own machine-readable codes, and
// a library brings a second taxonomy that has to be translated into the first at every call
// site. More importantly, Docs/06 §5.3 requires validation limits to be changeable under
// operational pressure without a deploy — the app has no over-the-air path for Dart code, so
// limits live server-side — and a limit baked into a compile-time tag cannot move.
//
// So validators are ordinary functions, and this package is the small amount of shared
// machinery they have in common: a collector, and the handful of checks every domain repeats.
//
// A validator gathers every problem before answering. Returning at the first one makes a
// six-field form take six round trips to fill in, on a phone, in a truck yard.
package validate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// Field-level codes. These are narrower than the request-level codes in httpx: the request
// failed with validation_failed, and each of these says what was wrong with one field.
const (
	CodeRequired   httpx.Code = "required"
	CodeTooShort   httpx.Code = "too_short"
	CodeTooLong    httpx.Code = "too_long"
	CodeOutOfRange httpx.Code = "out_of_range"
	CodeInvalid    httpx.Code = "invalid_format"
	CodeNotAllowed httpx.Code = "not_allowed"
)

// Errors collects field errors as a validator walks a request.
//
// The zero value is ready to use.
type Errors struct {
	fields []httpx.FieldError
}

// Add records a problem with one field.
//
// field is the dotted path into the JSON the client sent — "pickup.postcode", not
// "Pickup.Postcode". The client puts the message beside the input, so the path has to match
// what the client serialised.
func (e *Errors) Add(field string, code httpx.Code, format string, a ...any) {
	e.fields = append(e.fields, httpx.FieldError{
		Field:   field,
		Code:    code,
		Message: fmt.Sprintf(format, a...),
	})
}

// Any reports whether anything was recorded.
func (e *Errors) Any() bool { return len(e.fields) > 0 }

// Fields returns what was recorded, in the order it was recorded.
func (e *Errors) Fields() []httpx.FieldError { return e.fields }

// Err returns the request-level error carrying every field problem, or nil when there are
// none.
//
// Returning a typed nil would be a trap here: a handler writing `return v.Err()` must get a
// nil interface when the request was fine, or httpx.H would treat a successful validation as a
// failure. The explicit nil return is what makes that safe.
func (e *Errors) Err() error {
	if !e.Any() {
		return nil
	}
	return httpx.NewError(
		422,
		httpx.CodeValidationFailed,
		"Some of the details you entered need attention.",
	).WithDetails(e.fields...)
}

// Required records a problem when value is empty or only whitespace.
//
// It returns whether the value was present, so a caller can skip checks that only make sense
// on a value that exists and avoid reporting "too short" about a field the user left blank.
func (e *Errors) Required(field, value string) bool {
	if strings.TrimSpace(value) == "" {
		e.Add(field, CodeRequired, "This is required.")
		return false
	}
	return true
}

// Length records a problem when value's length in characters falls outside [minimum, maximum].
//
// Characters, not bytes. A description in a language that is not English should not fail a
// limit that a client counted differently, and Docs/01 makes no promise that job text is
// ASCII.
func (e *Errors) Length(field, value string, minimum, maximum int) {
	n := utf8.RuneCountInString(strings.TrimSpace(value))
	switch {
	case n < minimum:
		e.Add(field, CodeTooShort, "Enter at least %d characters.", minimum)
	case n > maximum:
		e.Add(field, CodeTooLong, "Keep this to %d characters or fewer.", maximum)
	}
}

// Range records a problem when value falls outside [minimum, maximum].
func (e *Errors) Range(field string, value, minimum, maximum int64) {
	if value < minimum || value > maximum {
		e.Add(field, CodeOutOfRange, "Enter a value between %d and %d.", minimum, maximum)
	}
}

// OneOf records a problem when value is not among allowed.
//
// The allowed values are not repeated back in the message. Category lists and vehicle types
// are reference data served to the client (SHIP-58), so the client already has the list and a
// long enumeration in an error message is noise on a phone screen.
func (e *Errors) OneOf(field, value string, allowed []string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	e.Add(field, CodeNotAllowed, "That is not one of the available options.")
}
