package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

// HeaderIdempotencyKey is the client-generated key that identifies one logical request
// across all of its retries.
const HeaderIdempotencyKey = "Idempotency-Key"

// HeaderIdempotencyReplayed marks a response that came from the store rather than from
// running the handler. Clients do not need it; it is there so that "did the retry
// actually replay" is answerable from a capture or a log without guessing.
const HeaderIdempotencyReplayed = "Idempotency-Replayed"

const (
	// CodeIdempotencyKeyRequired means a state-changing request arrived without a key.
	CodeIdempotencyKeyRequired Code = "idempotency_key_required"

	// CodeIdempotencyKeyReused means the key has already been used for a different
	// request. Answering with the first request's response would be worse than refusing.
	CodeIdempotencyKeyReused Code = "idempotency_key_reused"

	// CodeIdempotencyInProgress means the original request is still being handled. The
	// client should retry, which is what it was already doing.
	CodeIdempotencyInProgress Code = "idempotency_request_in_progress"
)

const (
	// maxIdempotencyKeyLen bounds what becomes part of a Redis key.
	maxIdempotencyKeyLen = 255

	// maxReplayableResponse bounds what is held in Redis per key. A response above it is
	// still sent, but is not stored — see the comment at the call site.
	maxReplayableResponse = 64 << 10 // 64 KiB

	// idempotencyKeyPrefix namespaces these keys within the shared Redis instance, and
	// carries a version so the stored shape can change without colliding with entries
	// written by the previous release during a rolling deployment.
	idempotencyKeyPrefix = "idem:v1:"
)

// IdempotencyStore is what this middleware needs of a store. It is declared here, by the
// consumer, and satisfied by *idempotency.RedisStore — the same rule the domain packages
// follow (Docs/06 §4.1).
type IdempotencyStore interface {
	// Begin claims key for a request with the given fingerprint. When claimed is true
	// the caller owns the key and must go on to call Complete or Release.
	Begin(ctx context.Context, key, fingerprint string) (entry *idempotency.Entry, claimed bool, err error)
	Complete(ctx context.Context, key, fingerprint string, resp idempotency.Response) error
	Release(ctx context.Context, key string) error
}

// Idempotent returns middleware that makes a repeated state-changing request return the
// original response instead of executing again (SHIP-15).
//
// # What it does
//
// GET, HEAD, and OPTIONS pass straight through. Everything else must carry an
// [HeaderIdempotencyKey]; Docs/01 §5.2 and Docs/02 §3.1 both state the rule as *every*
// state-changing request carrying one, so a missing key is refused rather than quietly
// waived. The first request with a given key runs, and its response is stored and
// returned. Every later request with that key gets that same response back without the
// handler being called again.
//
// # Why it fails closed
//
// If Redis cannot be reached, the request is refused with 503 rather than executed
// without protection. Executing is the more available choice and the wrong one: the
// request that arrives when Redis is unavailable is disproportionately likely to be a
// retry, because both a network problem and a broken retry loop are what put load there
// in the first place. A refusal the client retries costs a moment; a duplicate award or a
// duplicated milestone is a support case and, in the award's case, two providers who each
// believe they have the job.
//
// # scope
//
// Keys are namespaced by scope so that one caller's key cannot collide with — or read —
// another's. Passing nil scopes everything together, which is only safe while no endpoint
// under the middleware is authenticated.
//
// SHIP-44 must pass the authenticated subject here as it introduces the first protected
// endpoints. Without it, a client that guesses another client's key gets that client's
// response body.
func Idempotent(store IdempotencyStore, scope func(*http.Request) string) func(http.Handler) http.Handler {
	if scope == nil {
		scope = func(*http.Request) string { return "anonymous" }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isStateChanging(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get(HeaderIdempotencyKey)
			if key == "" {
				WriteError(w, r, NewError(http.StatusBadRequest, CodeIdempotencyKeyRequired,
					"This request must carry an %s header. Generate one value per action "+
						"and reuse it for every retry of that action.", HeaderIdempotencyKey))
				return
			}
			if !validIdempotencyKey(key) {
				WriteError(w, r, NewError(http.StatusBadRequest, CodeBadRequest,
					"The %s header must be 1 to %d printable characters without spaces.",
					HeaderIdempotencyKey, maxIdempotencyKeyLen))
				return
			}

			body, err := readIdempotentBody(r)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			// The handler still has to read the body this middleware consumed.
			r.Body = io.NopCloser(bytes.NewReader(body))

			ctx := r.Context()
			log := LoggerFrom(ctx)
			storeKey := idempotencyKeyPrefix + scope(r) + ":" + key
			fingerprint := fingerprintRequest(r.Method, r.URL.Path, body)

			entry, claimed, err := store.Begin(ctx, storeKey, fingerprint)
			if err != nil {
				log.LogAttrs(ctx, slog.LevelError, "idempotency store unavailable",
					slog.String("error", err.Error()))
				WriteError(w, r, NewError(http.StatusServiceUnavailable, CodeUnavailable,
					"This request cannot be accepted safely right now. Try again shortly.").
					WithCause(err))
				return
			}

			if !claimed {
				replayOrRefuse(w, r, entry, fingerprint)
				return
			}

			// From here the key is held. Every path out of this function has to either
			// record a response against it or give it back, including a panic on its way
			// to the Recover middleware — otherwise the client's retries are refused
			// until the in-flight TTL expires.
			settled := false
			defer func() {
				if settled {
					return
				}
				if err := store.Release(context.WithoutCancel(ctx), storeKey); err != nil {
					log.LogAttrs(ctx, slog.LevelError, "could not release the idempotency key",
						slog.String("error", err.Error()))
				}
			}()

			buffered := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
			next.ServeHTTP(buffered, r)
			buffered.flush(w)

			// A 5xx says "this might work if you try again", so the key goes back. Any
			// settled outcome — including a 4xx the client caused — is the answer, and
			// replaying it is what stops a retry loop turning one rejected bid into ten.
			if buffered.status >= 500 {
				return
			}

			// An oversized response is sent but not stored: holding megabytes per key
			// would put the shared cache at the mercy of one endpoint's payload. A retry
			// then re-executes, which the domain constraints still make safe, and the
			// warning says which endpoint to look at.
			if len(buffered.body) > maxReplayableResponse {
				log.LogAttrs(ctx, slog.LevelWarn, "response too large to replay",
					slog.String("path", r.URL.Path),
					slog.Int("bytes", len(buffered.body)),
					slog.Int("limit", maxReplayableResponse))
				return
			}

			// context.WithoutCancel: the client disconnecting is the very case this
			// exists for. The work is done, and the response has to be stored so their
			// retry gets it back.
			if err := store.Complete(context.WithoutCancel(ctx), storeKey, fingerprint,
				idempotency.Response{
					Status:      buffered.status,
					ContentType: buffered.header.Get("Content-Type"),
					Location:    buffered.header.Get("Location"),
					Body:        buffered.body,
				}); err != nil {
				log.LogAttrs(ctx, slog.LevelError, "could not store the idempotent response",
					slog.String("error", err.Error()))
				return
			}
			settled = true
		})
	}
}

// replayOrRefuse answers a request whose key is already held.
func replayOrRefuse(w http.ResponseWriter, r *http.Request, entry *idempotency.Entry, fingerprint string) {
	switch {
	case entry.Fingerprint != fingerprint:
		// Answering with the first request's response would tell the client that
		// something it never sent had succeeded.
		WriteError(w, r, NewError(http.StatusConflict, CodeIdempotencyKeyReused,
			"This %s has already been used for a different request. Generate a new key "+
				"for each action.", HeaderIdempotencyKey))

	case entry.Response == nil:
		// The original is still running. Retrying is the right thing to do, and is
		// already what the client was doing.
		w.Header().Set("Retry-After", "1")
		WriteError(w, r, NewError(http.StatusConflict, CodeIdempotencyInProgress,
			"An identical request is still being processed. Try again in a moment."))

	default:
		replay(w, *entry.Response)
	}
}

// replay writes a stored response.
func replay(w http.ResponseWriter, resp idempotency.Response) {
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	if resp.Location != "" {
		w.Header().Set("Location", resp.Location)
	}
	w.Header().Set(HeaderIdempotencyReplayed, "true")
	w.WriteHeader(resp.Status)
	_, _ = w.Write(resp.Body)
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// validIdempotencyKey accepts printable ASCII with no spaces, up to a bounded length.
//
// That admits every key format a client would generate — a UUID, a ULID, a hex string —
// and excludes whitespace and control characters, which have no place in a Redis key or
// in a log record.
func validIdempotencyKey(key string) bool {
	if key == "" || len(key) > maxIdempotencyKeyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		if c := key[i]; c <= ' ' || c > '~' {
			return false
		}
	}
	return true
}

// readIdempotentBody reads the request body so it can be fingerprinted, bounded so that a
// large upload cannot be turned into a large allocation.
//
// The bound is maxRequestBody, the same constant [DecodeJSON] reads a handler's body with, and
// it is deliberately one constant rather than two that happen to agree: this runs before the
// handler, so a handler allowed the larger body would be fingerprinted on bytes it never saw
// (Docs/10 §4.3). Until SHIP-15e there were two literals, one here and one in internal/identity,
// each with a comment asking the other not to move.
func readIdempotentBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		return nil, NewError(http.StatusBadRequest, CodeBadRequest,
			"The request body could not be read.").WithCause(err)
	}
	if len(body) > maxRequestBody {
		return nil, NewError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			"The request body may be at most %d bytes.", maxRequestBody)
	}
	return body, nil
}

// fingerprintRequest identifies the request a key was claimed for.
//
// Method and path are included as well as the body so that one key used against two
// different endpoints is caught, which is the shape a copy-pasted key usually takes. The
// digest is stored rather than the request, because the body routinely contains an
// address and a recipient's name and this is a shared cache.
func fingerprintRequest(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// bufferedResponse captures a handler's response so it can be stored before being sent.
//
// The response has to be complete and known-good before it is written to the wire: once
// the first byte reaches the client there is no way to record what they were told. That
// rules out streaming from any endpoint behind this middleware, which is not a cost here
// — the API returns JSON, and files move through pre-signed URLs (Docs/06 §5.2).
type bufferedResponse struct {
	header      http.Header
	status      int
	body        []byte
	wroteHeader bool
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.wroteHeader {
		return
	}
	b.status = status
	b.wroteHeader = true
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
	b.body = append(b.body, p...)
	return len(p), nil
}

// flush writes the captured response to the real writer.
func (b *bufferedResponse) flush(w http.ResponseWriter) {
	for name, values := range b.header {
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	w.WriteHeader(b.status)
	if len(b.body) > 0 {
		_, _ = w.Write(b.body)
	}
}
