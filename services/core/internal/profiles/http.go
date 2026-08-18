// The HTTP surface of the profiles domain.
//
// Handlers live in the domain rather than in `cmd/api` (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps `cmd/api/routes.go` free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing `internal/httpx` is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # Every route here is the caller's own record, and there is no parameter for whose
//
// The provider is whoever the token says is calling. A provider identifier in the path would be an
// authorisation decision made from client input, which Docs/07 §3 puts on the platform — and it would
// turn "a provider reads their own state" into a filter somebody could forget. There is nothing to
// forget: another provider's record is not refused, it is never selected.
//
// **The administrator's side of this is not here and is not this domain's to serve.** SHIP-153's
// queue and SHIP-154's decision are `internal/admin` endpoints under `/v1/admin`, on the separate
// administrator credential (SHIP-147). What they will call is [Service.Decide], through a port
// `internal/admin` declares for itself — this package exports the transition and does not export a
// route to it.
//
// The blank line below keeps this a file note rather than a second package comment.

package profiles

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Handler serves this domain's routes.
type Handler struct {
	svc  *Service
	docs *Documents
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewHandler wires the handlers to the two services this domain has.
//
// # Two collaborators rather than one widened service, and the split is the point
//
// [Service] moves a verification state and [Documents] mints URLs into the evidence bucket. They are
// separate types because they are separate capabilities: `cmd/api/routes_admin.go` constructs a
// [Service] to run SHIP-154's decision and has no business holding a signer, and nothing that can
// sign should thereby be able to decide. Both arrive here because both are served on the provider's
// own credential, and this is the only place they meet.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database on
// purpose — a rolling deployment during a failover would otherwise take every instance down at once —
// so a nil pool is a condition the handlers answer 503 to for as long as it lasts.
func NewHandler(svc *Service, docs *Documents, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("profiles: a handler needs a service")
	}
	if docs == nil {
		return nil, errors.New("profiles: a handler needs somewhere to put a document")
	}
	if log == nil {
		return nil, errors.New("profiles: a handler needs a logger")
	}
	return &Handler{svc: svc, docs: docs, pool: pool, log: log}, nil
}

// verificationResponse is a provider's own verification standing.
//
// # What is here, and the two things that are deliberately not
//
// **No administrator.** Docs/04 §4 requires a *reason* be communicated to a rejected provider and
// says nothing about a name; the identity of the person who rejected somebody is what turns a
// moderation decision into a personal one.
//
// **No "may I bid" flag.** That question is answered by `internal/fleet`'s eligibility predicate, in
// one place, and a boolean here would be a second answer — computed from one of the four filters, so
// wrong for every provider who is Verified with no vehicle or no declared service area. A client that
// wants to know whether there is work available asks for the feed, which is what SHIP-82 serves and
// what an empty page already says.
type verificationResponse struct {
	State string `json:"state"`

	// Reason is why, and is omitted rather than sent empty for a provider nobody has decided
	// anything about yet — which is every provider on the day they register.
	Reason string `json:"reason,omitempty"`

	// DecidedAt is when the current state was decided, omitted for the same providers.
	DecidedAt *time.Time `json:"decided_at,omitempty"`

	// SubmittedAt is when the record was created, which is the answer to "how long have I been
	// waiting" — the one question a Pending screen exists to answer.
	SubmittedAt time.Time `json:"submitted_at"`
}

func verificationFrom(v Verification) verificationResponse {
	out := verificationResponse{
		State:       string(v.State),
		Reason:      v.Reason,
		SubmittedAt: v.SubmittedAt,
	}
	if v.Decided() {
		decided := v.DecidedAt
		out.DecidedAt = &decided
	}
	return out
}

// Verification handles GET /v1/provider/verification (SHIP-81a).
//
// The provider's own record and nobody else's. A caller who is not a provider is refused with
// `profiles_provider_only` rather than answered an empty record — SHIP-78a's reading, applied to a
// surface built after it.
func (h *Handler) Verification() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		found, err := h.svc.VerificationFor(r.Context(), pool, providerID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, verificationFrom(found))
		return nil
	})
}

// documentUploadRequest is the body of POST /v1/provider/verification/documents/uploads.
//
//	{"content_type": "image/jpeg", "content_length": 1874233}
//
// **Both fields are signed into the URL**, which is why they are required and why neither is a hint.
// A URL that did not bind them would authorise any body at all, and the platform's size limit and
// accepted-type list would be advice the client gave itself.
//
// `content_length` is the exact size, not a maximum. A pre-signed PUT has one bound available to it
// and that is the signed `Content-Length` header, so an upload of any other size is refused by the
// store. The client has the file, so it knows.
//
// **There is no `kind` here and no `provider_id`.** The provider is the token. The kind is not needed
// to sign anything — the object key does not carry it, deliberately (see [documentObjectKey]) — so
// asking for it at this moment would collect a value the platform could neither record nor check. It
// is supplied when the document is submitted, which is when it is written down.
type documentUploadRequest struct {
	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`
}

// documentUploadResponse is one place to put one document image.
//
// # It carries a credential, and it is scoped like one
//
// `upload_url` is the whole of the authorisation to write that object — anyone holding it can, until
// `expires_at`, and nothing can revoke it. The idempotency middleware stores this response under
// `idem:v1:user:<provider>:<key>` (SHIP-44), so even a replay reaches only the provider who asked.
//
// The two echoed fields are instructions rather than confirmations: `content_type` and
// `content_length` are what the client **must** send on the PUT, byte for byte, because they are
// inside the signature. A client that normalised its own media type differently — `IMAGE/JPEG`, or a
// `; charset=` parameter — would otherwise get a signature failure from the store with no
// explanation. The method is stated for the same reason: a PUT is not guessable from a JSON body.
type documentUploadResponse struct {
	ObjectKey string `json:"object_key"`

	UploadURL string `json:"upload_url"`
	Method    string `json:"method"`

	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`

	ExpiresAt time.Time `json:"expires_at"`
}

func documentUploadFrom(u DocumentUpload) documentUploadResponse {
	return documentUploadResponse{
		ObjectKey: u.ObjectKey,

		UploadURL: u.URL,
		Method:    http.MethodPut,

		ContentType:   u.ContentType,
		ContentLength: u.ContentLength,

		ExpiresAt: u.ExpiresAt,
	}
}

// PresignDocumentUpload handles POST /v1/provider/verification/documents/uploads (SHIP-81b).
//
// # 200 rather than 201, and the difference is not pedantry
//
// Nothing was created. The platform holds no record of this URL, wrote no row and reserved no object
// — the same request a minute later produces a different key, and the bucket is untouched until the
// client PUTs. What happened is that a credential was issued. [Handler.SubmitDocument] is the request
// that creates something, and it answers 201.
//
// # POST on a read-shaped request, and it is state-changing enough to need a key
//
// It reads nothing and writes nothing, so `GET` was available and is wrong twice over: the request
// carries a body the platform signs, and issuing a credential that cannot be revoked is not a safe
// method whatever the database did. It therefore requires an `Idempotency-Key` like every other
// state-changing route (SHIP-15), and a retry inside the replay window gets the identical URL with
// its expiry already running down — which is correct, because one intent bought one upload slot.
//
// # The bytes do not come back through here, and that is the ticket
//
// A successful response is the end of this service's involvement. The client PUTs to `upload_url`
// directly (Docs/06 §5.2, Docs/04 §3.1) and the API sees neither the request nor the image — which is
// what `scripts/verify/62-profiles.sh` demonstrates by uploading to a host that is not the API's.
func (h *Handler) PresignDocumentUpload() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		var req documentUploadRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		upload, err := h.docs.PresignUpload(r.Context(), pool, providerID, DocumentUploadRequest{
			ContentType:   req.ContentType,
			ContentLength: req.ContentLength,
		})
		if err != nil {
			return apiError(err)
		}

		// Logged because the URL is otherwise invisible to operations: the upload it authorises
		// never touches this service, so this line and the store's own access log are the only two
		// records that it was issued. **The URL itself is not logged and never is** — it is the
		// credential, and a log line carrying one is a credential in whatever collects the logs.
		httpx.LoggerFrom(r.Context()).Info("an upload URL was issued for a verification document",
			slog.String("object_key", upload.ObjectKey),
			slog.String("content_type", upload.ContentType),
			slog.Int64("content_length", upload.ContentLength),
			slog.Time("expires_at", upload.ExpiresAt))

		httpx.WriteJSON(w, http.StatusOK, documentUploadFrom(upload))
		return nil
	})
}

// documentSubmission is the body of POST /v1/provider/verification/documents.
//
//	{"kind": "licence", "object_key": "verification/<provider>/<uuid>"}
//
// `object_key` is what the upload response answered with and nothing else: the platform chose it, so
// a client that could name the object it recorded could name one holding somebody else's licence.
// [keyBelongsToProvider] is what refuses that, and it decides from the string alone.
//
// There is no `provider_id`. It is the token — an identifier in the body would be an authorisation
// decision made from client input, which Docs/07 §3 puts on the platform.
type documentSubmission struct {
	Kind      string `json:"kind"`
	ObjectKey string `json:"object_key"`
}

// documentResponse is one submitted document.
//
// # There is no object key on the wire, and that is a narrowing rather than an oversight
//
// `delivery`'s proof response carries one, because a client that has just uploaded a photograph is
// holding that key already and a support conversation about a delivery is often about an object. A
// verification document is different in the way that matters: the key is a handle into the bucket
// holding identity documents, a client has no operation that takes one it did not just receive, and
// **every read here is answered with a fresh signed URL instead**. Sending a durable handle beside a
// short-lived credential would be handing out the thing the credential exists to make temporary.
type documentResponse struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`

	// ContentType and ContentLength are what the object store reported, not what the client said
	// it would send.
	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`

	SubmittedAt time.Time `json:"submitted_at"`

	// DownloadURL is a freshly signed URL to the image, and DownloadExpiresAt is when it stops
	// working.
	//
	// Both are omitted on the 201 that records a submission, and present on every read. That is
	// deliberate: the submission response is stored by the idempotency middleware and replayed on
	// a retry, so a download URL in it would be a credential at rest in Redis with a clock nobody
	// was watching. A reader asks for one and gets one minted on the request that asked.
	DownloadURL       string     `json:"download_url,omitempty"`
	DownloadExpiresAt *time.Time `json:"download_expires_at,omitempty"`
}

func documentFrom(d Document) documentResponse {
	return documentResponse{
		ID:            d.ID.String(),
		Kind:          string(d.Kind),
		ContentType:   d.ContentType,
		ContentLength: d.ContentLength,
		SubmittedAt:   d.SubmittedAt,
	}
}

func documentLinkFrom(l DocumentLink) documentResponse {
	out := documentFrom(l.Document)
	expires := l.ExpiresAt
	out.DownloadURL = l.URL
	out.DownloadExpiresAt = &expires
	return out
}

// documentListResponse is the collection envelope of Docs/10 §4.5.
//
// `next_cursor` is always null and `has_more` always false: there are four kinds and a provider
// re-photographs each a handful of times at most, so this is bounded by the domain rather than by a
// page size. The envelope is here anyway, because a client tells a collection from a single resource
// by its shape rather than by knowing which endpoint it called — the reading `delivery`'s proof list
// already takes of its own bound.
type documentListResponse struct {
	Data       []documentResponse `json:"data"`
	NextCursor *string            `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
}

func documentListFrom(links []DocumentLink) documentListResponse {
	// A non-nil empty slice, so a provider who has submitted nothing answers `[]` rather than
	// `null`. Reachable and ordinary: every provider is in this state on the day they register.
	out := documentListResponse{Data: make([]documentResponse, 0, len(links))}
	for _, link := range links {
		out.Data = append(out.Data, documentLinkFrom(link))
	}
	return out
}

// SubmitDocument handles POST /v1/provider/verification/documents (SHIP-81b).
//
// 201, because this is the request that creates something: the record that turns an object in a
// bucket into one of Docs/04 §3's four documents. Until it commits, the object is bytes with a key
// and the platform holds no opinion about whose they are.
//
// # The store is asked before the row is written, and the handler is not what asks
//
// [Documents.Submit] does, against the pool. That is a network request to another service, so it is
// deliberately not inside a transaction — a database transaction held open across a call to another
// service is a pool connection hostage to that service's worst day.
func (h *Handler) SubmitDocument() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		var req documentSubmission
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		document, err := h.docs.Submit(r.Context(), pool, providerID, Kind(req.Kind), req.ObjectKey)
		if err != nil {
			return apiError(err)
		}

		httpx.LoggerFrom(r.Context()).Info("a verification document was submitted",
			slog.String("document_id", document.ID.String()),
			slog.String("kind", document.Kind.String()),
			slog.String("object_key", document.ObjectKey),
			slog.Int64("content_length", document.ContentLength))

		httpx.WriteJSON(w, http.StatusCreated, documentFrom(document))
		return nil
	})
}

// ProviderDocuments handles GET /v1/provider/verification/documents (SHIP-81b).
//
// The caller's own evidence and nobody else's, each with a URL minted on this request. There is no
// parameter for whose: another provider's documents are not refused here, they are never selected.
//
// This is where "reachable only by a fresh signed URL" is demonstrable rather than asserted. Nothing
// stored holds a URL, so every link has its own window, and the object is refused without a
// signature — which `scripts/verify/62-profiles.sh` checks against the store directly.
func (h *Handler) ProviderDocuments() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		links, err := h.docs.For(r.Context(), pool, providerID)
		if err != nil {
			return apiError(err)
		}

		// Logged for the reason the upload line is: the fetch that follows never touches this
		// service, so this and the store's own access log are the only two records that a
		// document was reachable. **No URL is logged** — it is the credential.
		//
		// It is deliberately not the access log Docs/04 §6 requires of an administrator viewing
		// evidence. That is a durable record with a decision attached to it, and it is SHIP-155's.
		httpx.LoggerFrom(r.Context()).Info("a provider read their own verification documents",
			slog.Int("document_count", len(links)))

		httpx.WriteJSON(w, http.StatusOK, documentListFrom(links))
		return nil
	})
}

// callerID is the authenticated subject's identifier.
//
// `RequireUser` has already refused the request without one, so a subject that is not a UUID is a
// defect in whatever issued the token rather than anything the caller did — hence 500 rather than
// 401, with the cause logged and nothing about it in the body.
func callerID(ctx context.Context) (uuid.UUID, error) {
	subject := authctx.MustSubject(ctx)

	id, err := uuid.Parse(subject.UserID)
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
			"Something went wrong at our end.").WithCause(err)
	}
	return id, nil
}

// database returns the pool, or the answer to give while there is not one.
//
// 503 rather than 500, because the two say different things to a mobile client: retry, or surface a
// failure to the person holding the phone.
func (h *Handler) database(r *http.Request) (*pgxpool.Pool, error) {
	if h.pool != nil {
		return h.pool, nil
	}

	httpx.LoggerFrom(r.Context()).Warn("a profiles request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and which
// HTTP status a caller deserves is not a question the domain has an opinion about. A *httpx.Error
// passes straight through, because validation already produced one in the right shape.
func apiError(err error) error {
	var already *httpx.Error
	if errors.As(err, &already) {
		return err
	}

	switch {
	case errors.Is(err, ErrNotProvider):
		return httpx.NewError(http.StatusForbidden, CodeProviderOnly,
			"Only a provider account has a verification record.").WithCause(err)

	case errors.Is(err, ErrNoSuchProvider):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such provider.").WithCause(err)

	case errors.Is(err, ErrAlreadyInState):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"That provider is already in that state.").WithCause(err)

	case errors.Is(err, ErrDocumentNotForThisProvider):
		// 422 with the field named, because it is the caller's own body disagreeing with the
		// caller's own credential — the same treatment `delivery` gives a proof key issued for
		// another job. It discloses nothing: the key names the other provider in plain text and
		// the caller is the one who sent it.
		var problems validate.Errors
		problems.Add("object_key", validate.CodeInvalid,
			"That object_key was issued to a different provider. Ask for an upload URL and send "+
				"the key it answers with.")
		return problems.Err()

	case errors.Is(err, ErrDocumentNotUploaded):
		return httpx.NewError(http.StatusConflict, CodeDocumentNotUploaded,
			"That document has not reached us. Finish uploading it, then submit it again.").
			WithCause(err)

	case errors.Is(err, ErrDocumentRejected):
		return httpx.NewError(http.StatusConflict, CodeDocumentRejected,
			"That file is not a document this platform accepts.").WithCause(err)

	case errors.Is(err, ErrDocumentAlreadyRecorded):
		return httpx.NewError(http.StatusConflict, CodeDocumentAlreadyRecorded,
			"That image has already been submitted. Ask for a new upload URL and send it "+
				"again.").WithCause(err)

	default:
		// Including [ErrActorNotRecorded] and [ErrNotInTransaction], both of which are wiring
		// faults rather than anything a client did: an opaque 500 with the cause logged is the
		// honest answer, because there is nothing the caller could change.
		return err
	}
}
