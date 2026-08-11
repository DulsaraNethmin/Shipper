package httpx

import "net/http"

// H adapts a handler that returns an error to one that writes a response.
//
// Returning an error rather than writing one removes the "forgot to return after writing"
// defect, which otherwise emits two response bodies and a status the client cannot make sense
// of. The returned error goes through [WriteError], so every failure leaves this service in the
// one shape SHIP-12 specifies, whether the handler built it with [NewError] or let a sentinel
// through.
//
// A nil error means the handler has already written its response and this adds nothing.
//
//	Route{Method: "POST", Pattern: "/jobs", Auth: RequireUser, Handler: httpx.H(h.create)}
//
// # Why the name is one letter
//
// Docs/10 §4.3 specifies it, and it appears once per route in a list of routes where the
// interesting part is the handler being wrapped rather than the wrapping.
//
// This is the second mechanism Docs/10 named, Docs/11 §3 listed as built, and nobody had
// written — httpx.RegisterCode was the first, absent until SHIP-15c. SHIP-30 needed it, could
// not edit a shared surface mid-wave, and wrote it unexported inside internal/identity as
// apiHandler. It was promoted here at SHIP-15e, before a second domain copied it: two domains
// with two adapters is two answers to what happens when a handler both writes and returns.
func H(fn func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			WriteError(w, r, err)
		}
	})
}
