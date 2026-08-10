// Package clock supplies the current time as a dependency rather than as a global.
//
// Four scheduled tasks in the backlog turn on elapsed time — job expiry at the earlier of
// fourteen days or the pickup date (SHIP-68), the warning forty-eight hours ahead of it
// (SHIP-69), bid expiry (SHIP-89), and the seventy-two hour auto-complete (SHIP-119) — and
// every token in the system has a lifetime. None of that is testable against a wall clock
// without either sleeping or waiting three days.
//
// The rule is that a constructor takes a Clock. An inline time.Now() is invisible until
// somebody tries to test around it, at which point it is threaded through anyway, only later
// and across more files.
package clock

import "time"

// Clock reports the current time.
//
// It is deliberately one method. Anything richer — timers, tickers, sleeping — belongs to
// the caller that needs it, and a wider interface would make every fake implement machinery
// no test uses.
type Clock interface {
	Now() time.Time
}

// System is the real clock, and the only one that should reach production.
type System struct{}

// Now returns the current time in UTC.
//
// UTC rather than local: every timestamp this service stores is timestamptz and every
// comparison it makes is against another instant, so a location here would only be a
// difference between machines. Day-first presentation is the client's job (Docs/10 §3.3).
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a clock stopped at an instant, for tests.
//
// It is not safe for concurrent use while being advanced. A test that advances time from one
// goroutine while another reads it should guard the clock itself, which is rare enough not to
// justify a mutex on every read here.
type Fixed struct {
	Instant time.Time
}

// NewFixed returns a clock stopped at t.
func NewFixed(t time.Time) *Fixed { return &Fixed{Instant: t} }

// Now returns the instant this clock is stopped at.
func (f *Fixed) Now() time.Time { return f.Instant }

// Advance moves the clock forward by d. A negative d moves it back, which is occasionally
// what a test of out-of-order arrival needs (Docs/02 §3.1).
func (f *Fixed) Advance(d time.Duration) { f.Instant = f.Instant.Add(d) }
