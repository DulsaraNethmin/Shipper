// Package config loads every runtime setting from the environment (SHIP-8).
//
// Two rules hold here, both from Docs/06 §5.1:
//
//   - All configuration comes from environment variables. Nothing is read from a
//     configuration file at runtime, and nothing that varies by environment is compiled
//     into the binary.
//   - No secret has a usable default outside development. The development defaults below
//     exist so that `make up && make run` works on a fresh clone; Load rejects them
//     outright in staging and production rather than trusting anyone to notice.
//
// deploy/.env.example documents the same variables for humans. If you add one here, add
// it there too — a setting nobody can find is a setting nobody sets.
package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment selects behaviour that legitimately differs between deployments — chiefly
// which adapter implementations are used. In development, email and SMS log to the
// console instead of dispatching (Docs/06 §4.1, SHIP-32, SHIP-35).
type Environment string

const (
	Development Environment = "development"
	Staging     Environment = "staging"
	Production  Environment = "production"
)

func (e Environment) IsDevelopment() bool { return e == Development }

func (e Environment) valid() bool {
	switch e {
	case Development, Staging, Production:
		return true
	}
	return false
}

// Config is the whole of the service's configuration.
type Config struct {
	Env          Environment
	HTTP         HTTP
	Log          Log
	Database     Database
	Redis        Redis
	Kafka        Kafka
	Idempotency  Idempotency
	Passwords    Passwords
	Identity     Identity
	Delivery     Delivery
	Email        Email
	SMS          SMS
	Push         Push
	Geocoding    Geocoding
	Pagination   Pagination
	RateLimits   RateLimits
	TrustedProxy TrustedProxy
	Storage      Storage
	Verification Verification
	Goods        Goods
	App          App
}

// Verification configures Docs/04 §3's provider verification documents (SHIP-159).
//
// # One value, and it is deliberately allowed to be empty
//
// Docs/04 §3 states an open question and gives it to somebody else: "which documents are *legally*
// required rather than merely prudent, and **how often each must be renewed**. Owner: legal and
// insurance advisers… This determines expiry tracking (§5) and retention obligations, and remains
// genuinely outside engineering's competence to settle." It is Track-X row X-4 and it is unanswered.
//
// §3 permits building before the answer arrives — "needed before pilot users are invited, not before
// build begins" — so the constraint on this struct is not that it wait, but that **the platform must
// not enforce a number nobody decided.** Hence: no default, and an empty map is a valid, shipping
// configuration.
//
// # What is configuration here and what is not
//
// **When a document lapses is not configuration.** It is a fact stated at submission and held in
// `provider_verification_documents.expires_at`, and a document that states none never appears on the
// expiry queue at all. Nothing here computes an expiry from a submission date.
//
// **How far ahead of that date a document is worth chasing is.** With no lead time configured for a
// kind, its horizon is *now* — so the queue reports documents that have actually lapsed and not a
// day before. Setting a lead time moves that boundary earlier, and that number is the policy X-4
// owns.
//
// This is `Docs/06` §5.3 applied to a value that has not been decided rather than to one that moves:
// the reason it is server-side is the same, and the reason it has no default is that a default here
// would be the platform answering a question it was asked not to.
type Verification struct {
	// ExpiryLeadTimes is how far ahead of its expiry a document of each kind is worth chasing,
	// keyed by the kind's stored spelling — `licence`, `registration`, `insurance`,
	// `abn_evidence`.
	//
	// **Keyed by string rather than by a document-kind type**, and that is forced rather than
	// stylistic: `internal/config` is infrastructure and may not import a domain (SHIP-15c), so
	// `profiles.Kind` cannot appear here. `cmd/api` translates, and `profiles.NewExpiry` refuses a
	// kind the platform does not have — which is what stops a typo becoming configuration that
	// silently does nothing.
	//
	// Empty by default. See the type's own note: that is X-4 being unanswered, not a value
	// somebody forgot.
	ExpiryLeadTimes map[string]time.Duration
}

// Goods carries the catalogue of goods categories a job may name (SHIP-58).
//
// # Why this is configuration rather than a table or a constant
//
// CLAUDE.md names "category lists" first among the things that live server-side because they
// change under operational pressure, and Docs/06 §5.3 requires such values to move without a
// deploy. A Go constant fails that outright. A table would pass it, and was rejected for a
// narrower reason: the list is not data the product *accumulates*, it is a decision the platform
// *publishes*, and the moment it lives in a table it acquires an administrative surface nobody
// asked for and a migration for every revision.
//
// # The list below is provisional, and the type says so per entry rather than per catalogue
//
// X-9 asked for a finalised prohibited-goods list and depends on X-4, the legal brief, which is
// unanswered. Docs/11 §5 records the way out taken here: the owner approves a provisional list,
// **the list is marked provisional in the reference data itself**, X-4 stays open, and Docs/11 §4
// carries what is still owed. This is the same shape SHIP-139 and SHIP-143 shipped in against an
// FCM project that did not exist.
//
// [GoodsCategory.Provisional] is per entry rather than one flag on the catalogue, and that is a
// judgement about how X-4 will land rather than a preference: a legal adviser is far more likely
// to confirm most of a list and query two entries than to bless or reject thirteen at once. A
// per-entry flag lets that answer arrive incrementally and be seen by a client; a catalogue-wide
// one would have to flip from true to false in a single step that nobody could stage.
//
// # Why the default is in Go, having just argued that a constant fails
//
// Because the two are not the same claim. What Docs/06 §5.3 forbids is a value that *cannot* be
// changed without a release, and one environment variable changes every field of every entry
// here. What it does not ask for is a service that refuses to start until somebody types a
// thirteen-entry JSON document — which is what an empty default would mean, since a catalogue
// with nothing carried can publish nothing at all. Storage.AcceptedContentTypes is the same
// arrangement for the same reason and is likewise policy rather than plumbing.
//
// Verification.ExpiryLeadTimes is the deliberate opposite and the contrast is worth keeping: its
// default is empty because a lead time nobody decided would be *the platform inventing policy*.
// A category list nobody decided is not policy the platform invented — it is Docs/01 §2's
// out-of-scope list and Docs/05 §4's draft position, both already written down and neither
// contentious. The owner approved it; the flag records that a lawyer has not.
type Goods struct {
	// Categories is the whole catalogue, in the order it is served to clients.
	//
	// Order is meaningful and is the configuration's, not this package's: it is what a client
	// renders in a picker, and sorting it here would take that decision away from whoever set
	// it. The default below reads carried first because that is what a customer is choosing
	// between; the refused entries follow so the app can show what Shipper will not take.
	Categories []GoodsCategory
}

// carriedCount is how many categories a job may actually be published in.
func (g Goods) carriedCount() int {
	n := 0
	for _, c := range g.Categories {
		if c.Carried {
			n++
		}
	}
	return n
}

// GoodsCategory is one entry in the catalogue (SHIP-58).
//
// A struct rather than a bare code because the label and the description are served to clients
// and must not be compiled into them — Flutter has no over-the-air path for Dart code, so a
// wording change would otherwise need an app release (CLAUDE.md, Docs/06 §5.3). The same argument
// covers Carried: whether Shipper takes a category is a policy answer that has to be able to move
// on a Tuesday afternoon.
//
// **Declared here rather than in internal/jobs**, and that is forced rather than stylistic:
// internal/config is infrastructure and may not import a domain (SHIP-15c). cmd/api translates
// this into the domain's own catalogue type, exactly as it translates Verification.ExpiryLeadTimes
// into profiles' document kinds, and the domain refuses a catalogue it cannot make sense of.
type GoodsCategory struct {
	// Code is the stored form and the value a client sends — lower snake case, stable.
	//
	// Stable is the load-bearing word. It is written into jobs.goods_category on every job that
	// names it, so renaming a code orphans history; the label is what changes when the wording
	// does. [loader.goods] refuses a code that is not lower snake case for that reason — a code
	// that looks like a label invites somebody to edit it like one.
	Code string `json:"code"`

	// Label is the short human name, in Australian English.
	Label string `json:"label"`

	// Description is the one-line explanation a client shows beneath the label. Optional.
	Description string `json:"description,omitempty"`

	// Carried says whether a job in this category may be published.
	//
	// False does not mean hidden. A refused category is served like any other so that the app
	// can show what Shipper will not carry and SHIP-59 can refuse a publication by name rather
	// than with a generic validation failure — which is what Docs/09's "cannot be published and
	// explains why" asks for. Docs/07 §3 still applies: the app may hide or disable, and the
	// platform decides.
	Carried bool `json:"carried"`

	// Provisional says the entry has not been through the legal review X-4 owns.
	//
	// True for every entry in the default catalogue. See [Goods] for why it is per entry.
	Provisional bool `json:"provisional"`
}

// provisionalGoodsCategories is the catalogue X-9 was accepted in reduced form with.
//
// Every refused entry traces to a line already written down and closed: Docs/01 §2's out-of-scope
// list names dangerous goods, live animals, people and specialist regulated freight, and Docs/05
// §4's draft position adds illegal goods. Two — temperature_controlled and high_value_negotiables
// — are the owner's own additions and are the two most likely to move when X-4 is answered, which
// is precisely what [GoodsCategory.Provisional] is for.
//
// Docs/01 §8's demo gate carries the caveat this list ships under: it "runs on a **provisional**
// category list approved by the owner rather than by a legal adviser, which is enough to
// demonstrate the mechanism and is **not** enough to put in front of public users."
var provisionalGoodsCategories = []GoodsCategory{
	{Code: "general_freight", Label: "General freight",
		Description: "Palletised or boxed goods needing no special handling.",
		Carried:     true, Provisional: true},
	{Code: "furniture", Label: "Furniture and white goods",
		Description: "Household or office furniture, appliances.",
		Carried:     true, Provisional: true},
	{Code: "household_removals", Label: "Household removals",
		Description: "Personal effects, part or whole household.",
		Carried:     true, Provisional: true},
	{Code: "building_materials", Label: "Building materials",
		Description: "Timber, plasterboard, fixtures and fittings.",
		Carried:     true, Provisional: true},
	{Code: "machinery", Label: "Machinery and equipment",
		Description: "Plant and equipment carried on a trailer.",
		Carried:     true, Provisional: true},
	{Code: "retail_stock", Label: "Retail and office stock",
		Description: "Shop stock, fit-out, office relocation.",
		Carried:     true, Provisional: true},

	{Code: "dangerous_goods", Label: "Dangerous goods",
		Description: "Explosives, flammable liquids or gases, corrosives, oxidisers.",
		Carried:     false, Provisional: true},
	{Code: "live_animals", Label: "Live animals",
		Description: "Shipper does not carry livestock or pets.",
		Carried:     false, Provisional: true},
	{Code: "people", Label: "Passengers",
		Description: "Shipper carries freight, not people.",
		Carried:     false, Provisional: true},
	{Code: "regulated_freight", Label: "Specialist regulated freight",
		Description: "Pharmaceuticals, firearms, tobacco or alcohol in commercial quantity.",
		Carried:     false, Provisional: true},
	{Code: "temperature_controlled", Label: "Temperature-controlled goods",
		Description: "There is no cold chain in the MVP.",
		Carried:     false, Provisional: true},
	{Code: "high_value_negotiables", Label: "Cash, bullion and negotiable instruments",
		Description: "These need secure freight, which Shipper does not arrange.",
		Carried:     false, Provisional: true},
	{Code: "illegal_goods", Label: "Unlawful goods",
		Description: "Anything unlawful to possess or transport.",
		Carried:     false, Provisional: true},
}

// Storage configures the private object store that holds proof-of-delivery photographs and
// provider verification documents (SHIP-114).
//
// Added at SHIP-15p rather than at SHIP-114, and for once that is not the usual story of a
// domain branch parking a request against a shared surface after the fact. Docs/11 §9 records
// that three prep tickets in a row absorbed a parked internal/config request, and that the
// conclusion was to **ask each track at dispatch what configuration it expects to need**. Wave 7
// asked, Track B answered with the nine fields below, and this is that answer written down
// before the track opened rather than a wave later.
//
// # Nothing here is an adapter decision
//
// internal/platform/storage/doc.go names two implementations — the filesystem in development and
// S3 for staging and production — and choosing between them is cmd/api's job, from Env, exactly
// as it is for email, SMS and geocoding. These values describe the *store*, not the choice.
//
// # Why the last two fields are here at all
//
// MaxUploadBytes and AcceptedContentTypes are policy rather than plumbing, and a limit compiled
// into the client is a limit that needs an app release to change. Docs/06 §5.3 is explicit:
// Flutter has no over-the-air path for Dart code, so anything expected to move under operational
// pressure lives server-side. A proof photograph that a driver cannot upload is a delivery that
// cannot be completed, which is the failure Docs/01 §4.4 spends a whole section avoiding.
type Storage struct {
	// Endpoint is the S3 API's base URL, and it is always explicit.
	//
	// **The host in it is signed.** SigV4 covers the `host` header, so a pre-signed URL minted
	// against one address and fetched at another is refused as SignatureDoesNotMatch — an error
	// that names neither the address nor the cause. In development that means the *published*
	// port on the host, not `minio:9000` inside the compose network, because whatever resolves
	// the URL is on the host.
	//
	// There is no "leave it empty and let the SDK pick the regional endpoint" state, and that is
	// a decision rather than an oversight: [loader.lookup] treats an empty variable as absent, so
	// an empty value would silently become this field's development default — an object store on
	// somebody's loopback, configured in production, reported as nothing. AWS's own regional
	// endpoint is `https://s3.<region>.amazonaws.com` and is perfectly typeable, so a deployment
	// types it and [loader.validate] refuses a loopback host outside development.
	Endpoint string

	// Bucket holds every object the platform stores. One bucket, prefixed by concern — there is
	// no second bucket for verification documents, because both kinds of object are private
	// evidence under the same access rules and a second bucket is a second set of them.
	//
	// **It is per-worktree in development** (CLAUDE.md's worktree table). A bucket is a cheap
	// namespace, which is precisely what a Kafka topic is not, so the one shared service that
	// can be split per tree is this one.
	Bucket string

	// Region is the S3 region the bucket lives in, and it is signed as part of the credential
	// scope rather than merely routed on. The local MinIO is started with the same value for
	// that reason; a mismatch is a signature failure, not a redirect.
	Region string

	// AccessKeyID and SecretAccessKey authenticate to the store.
	//
	// Static credentials, because the MVP has one deployment shape and no instance-role story
	// yet. Whoever moves the service onto an IAM role deletes these two and the SDK finds its
	// own; nothing else in the section changes.
	AccessKeyID     string
	SecretAccessKey string

	// UsePathStyle addresses a bucket as `endpoint/bucket/key` rather than
	// `bucket.endpoint/key`.
	//
	// True by default because the development store is MinIO on localhost, where the
	// virtual-host form would need `shipper-dev.localhost` to resolve and does not. A
	// deployment against AWS sets it false.
	UsePathStyle bool

	// PresignTTL is how long an issued **upload** URL works for.
	//
	// Short, and bounded at load, for the reason the driver token is: **nothing can revoke one
	// once it is signed.** The URL is the whole of the authorisation — Docs/06 §5.2 puts the
	// bytes outside the API entirely — so its lifetime is the window in which a copied link
	// reaches a photograph of somebody's front door. Fifteen minutes is long enough for a
	// phone on a poor connection to finish an upload it has already started, which is the only
	// thing it has to outlast.
	PresignTTL time.Duration

	// DownloadTTL is how long an issued **download** URL works for, and it is a separate number
	// because the two directions have nothing in common but the mechanism (SHIP-15r).
	//
	// SHIP-114 signed uploads with PresignTTL; SHIP-115 added the downloads and reused it, and both
	// tickets recorded the request rather than parking a flag, because a field here is a
	// shared-surface edit a domain branch may not make. This is that field.
	//
	// **Only one of the two requirements is generous.** An upload link has to outlast a phone
	// finishing a slow PUT on a bad connection — minutes of a transfer that has already started. A
	// download link has to outlast an image rendering, which is seconds. One number serving both
	// therefore errs long in the direction that costs something: every read link is a live,
	// unrevocable link to a photograph of somebody's front door, and it stayed live for as long as
	// an *upload* needed.
	//
	// Five minutes rather than one, so that a customer's tracking view can sit open, a phone can
	// change network mid-fetch, and a moderator can open several photographs from one listing
	// without the first expiring underneath them. Bounded by the same [maxPresignTTL], for the
	// same reason: nothing revokes either kind once it is signed.
	DownloadTTL time.Duration

	// MaxUploadBytes is the largest object the platform will issue an upload URL for.
	//
	// Docs/01 §5.2 requires the client to compress before uploading, so this is a bound on a
	// mistake rather than on ordinary use: a phone photograph compressed for a metered
	// connection is one or two megabytes.
	MaxUploadBytes int64

	// AcceptedContentTypes is the set of media types an upload URL may be issued for, lower
	// case, one `type/subtype` per entry.
	//
	// Server-side because the answer changes: HEIC arrived on iOS without an app release
	// anywhere, and a client-side list would have needed one. It is a list rather than a
	// prefix match so that `image/svg+xml` — a script container that browsers execute — cannot
	// arrive by being an image.
	AcceptedContentTypes []string
}

// Geocoding configures the address-lookup adapter (SHIP-35), first consumed by SHIP-60's
// location value object.
//
// Added at SHIP-15g rather than at SHIP-60, and the delay is the point: internal/config is a
// shared surface a domain branch must not edit, so the jobs lane wrote a documented constant and
// filed a request instead. The same wall was hit twice in one wave — see [Pagination] — which is
// what turned two parked requests into a prep ticket.
//
// # Which implementation is built is [Transport], and no longer the environment
//
// It was geocoding.UseStub(env) — development resolved in-process, staging and production called
// the vendor — and SHIP-192 removed that function. The environment is the wrong thing to read it
// from, for the same reason SHIP-187a gave for email: a demonstration instance runs hardened and
// must not spend on a metered API, and a developer checking one real address should not have to
// claim to be production to do it. Neither case is expressible while the transport is a function
// of Env.
//
// # Why a missing base URL is not an error, and when it becomes one
//
// [TransportStub] needs neither base URL nor credential, so both are empty in the ordinary
// development case and nothing is wrong. Under [TransportHTTP] the base URL is required and
// validate refuses the startup by name.
//
// That refusal is the whole gain. Before this field an unset GEOCODING_BASE_URL in production
// produced a nil geocoder and one warning in the boot log, and nothing failed until somebody
// noticed a month of jobs stored with no coordinates at all.
//
// Nil is still a supported state rather than a degraded one — jobs.NewService documents it as
// "addresses are stored unresolved", and every path copes, because Docs/01 §4.4's SHIP-59a
// requires a failed lookup not to fail the job.
//
// Falling back to the stub outside development is still refused, and this is where that refusal
// now lives. The stub returns stable, plausible, entirely fictional coordinates, and an
// environment quietly full of those is worse than one with no coordinates at all — the first
// looks like it works. A deployment may still choose the stub, but it has to say so.
//
// The field names match [Email] and [SMS] deliberately: all three are the same shape of
// adapter — a base URL, a credential, and a transport named in configuration — and naming the
// third one differently would invite the reader to look for a difference that is not there.
type Geocoding struct {
	// Transport is which implementation resolves an address: stub or http (SHIP-192).
	//
	// Unset it is [TransportStub], in every environment including production — and staging
	// and production then refuse to start until it is named outright. The default and the
	// refusal are one decision read from both ends: nothing reaches a metered vendor by
	// inheriting a string it did not recognise, and nothing serves fictional coordinates to
	// real customers because a variable was forgotten.
	Transport Transport

	// ProviderBaseURL is the provider's HTTP endpoint. Required under [TransportHTTP], and
	// unused under [TransportStub].
	ProviderBaseURL string

	// ProviderAPIKey authenticates to it. Empty is allowed even when the base URL is set,
	// because a local or self-hosted geocoder may need no credential.
	ProviderAPIKey string
}

// Pagination carries the page sizes Docs/10 §4.5 requires to be configurable.
//
// They lived as constants in internal/pagination from SHIP-66, with a comment saying that section
// requires configuration and that a domain branch could not provide it. This is that move. No
// caller changes: callers ask pagination.Limit, which now reads what cmd/api installed at startup.
//
// # Why these are bounded rather than merely positive
//
// A maximum page size is a bound on the work one request can ask the database for, so a
// misconfigured value is a denial-of-service switch rather than a preference. The ceiling is
// enforced here, at load, where it fails on startup rather than on the first large request.
type Pagination struct {
	// DefaultPageSize applies when a request names no ?limit=.
	DefaultPageSize int

	// MaxPageSize is the ceiling a larger ?limit= is narrowed to, never an error.
	MaxPageSize int
}

// RateLimits is the global lever over every rate-limit class (SHIP-183, SHIP-183a).
//
// # Why there is no per-route setting here, and never should be
//
// Docs/12 §7 decided it. The service serves 86 routes; a pair of variables each would be 172 of
// them, and `deploy/.env.example` documents every variable this service reads — nobody maintains
// 172 of those correctly. Worse, a per-route override is a number that can drift from the reason
// written beside it in Docs/12 with nothing to notice, which is the exact failure that document
// exists to prevent, reintroduced through the back door.
//
// What an incident actually wants is **"everything tighter, now"** while something is being
// attacked, or **"looser, now"** because a real customer's NAT is being throttled. Both are global,
// and both are one of these two values.
//
// The revisit trigger is named in Docs/12 §10 rather than left to judgement: the first incident that
// genuinely wants one route different from the rest. At that point the answer is a per-**class**
// override map — seven values a person can hold in their head — and not a per-route one.
//
// # Why two and not one
//
// Capacity governs the burst and interval governs the sustained rate, and they are independent.
// Scaling capacity alone changes what a cold start can absorb and leaves the long-run rate exactly
// where it was; scaling the rate alone does the opposite. A single "tighten by half" cannot say
// which of those it meant, and the two incidents that produce it want different answers.
type RateLimits struct {
	// BurstScale multiplies every class's capacity. 1.0 is the figure Docs/12 §3 argues for.
	BurstScale float64

	// RateScale multiplies every class's sustained rate, which means it divides the interval:
	// at 2.0 a token comes back twice as fast.
	RateScale float64
}

// TrustedProxy decides whether a forwarded header may be believed (SHIP-183b).
//
// # The problem it exists for
//
// Eleven of the 86 routes key their rate limit on the client's network address (Docs/12 §8), and
// an address is the one thing about a request that the request itself does not carry. There are
// two ways to be wrong about it and both are unacceptable in production:
//
//   - **Honour `X-Forwarded-For` unconditionally.** The header is whatever the client wrote unless
//     a proxy overwrote it, so every caller picks their own bucket and the limit is evaded
//     completely — which is worse than having none, because it looks like one.
//   - **Never read it.** Behind a load balancer every request presents the balancer's address, so
//     all eleven routes collapse into one bucket per class and the first caller throttles the
//     world.
//
// The way out is not a better guess. It is a deployment fact — *how many proxies are in front of
// this process, or which addresses they have* — that only whoever runs the deployment knows. This
// is where they say it.
//
// # Trusting nothing is the default, and it is the safe end
//
// Both fields are empty unless a deployment sets one, and empty means [RemoteAddr] alone. That is
// exactly the behaviour SHIP-47 shipped and Docs/11 §9 recorded, so a deployment that says nothing
// gets no new exposure — it gets the shared-bucket problem, which is a throttling fault rather
// than a bypass. **The unsafe direction is the one that needs configuration**, which is the right
// way round for a value nobody may remember to set.
//
// # Why the two are mutually exclusive
//
// They answer the same question by different means and Docs/09's *Done when* offers them as
// alternatives — "a configured trusted-proxy hop count **or** CIDR allow-list". Setting both would
// need a composition rule that nobody has argued for, and the plausible readings disagree: does
// the allow-list gate whether the header is read at all, or does it filter which hops the count
// skips? A deployment that set both would get whichever one this file happened to prefer. Refusing
// at startup is how that stays a decision rather than an accident.
type TrustedProxy struct {
	// Hops is how many proxies sit between the client and this process.
	//
	// It is positional and ignores the values entirely, which is what makes it unspoofable: the
	// chain is read right to left, the rightmost entry is the peer this process actually
	// accepted the connection from, and a client's invented entries can only ever be further
	// left than the real ones. A caller who prepends ten addresses moves the true client
	// address ten places left and the count still lands on it.
	//
	// Zero means no forwarded header is read.
	Hops int

	// Networks is the set of addresses a proxy may present from.
	//
	// The peer must be inside one or the header is ignored outright, which is what stops a
	// caller reaching the service directly and choosing their own bucket. Trusted entries are
	// then skipped from the right, and the first untrusted one is the client.
	//
	// **Keep these to the addresses the proxies actually have.** A range wide enough to contain
	// addresses a client could also write into the header — `10.0.0.0/8` where the balancer is
	// one host — reopens the bypass from inside, because a spoofed entry that looks trusted is
	// skipped rather than believed. [TrustedProxy.Hops] has no equivalent weakness and is the
	// better choice wherever the count is stable.
	//
	// Empty means no forwarded header is read.
	Networks []netip.Prefix
}

// Configured reports whether a deployment has said anything about proxies.
//
// False is the default and means RemoteAddr alone.
func (t TrustedProxy) Configured() bool {
	return t.Hops > 0 || len(t.Networks) > 0
}

// Email configures the transactional email adapter (SHIP-32), first consumed by the
// verification message SHIP-31 sends.
//
// Which implementation is built is decided from Env and not from here: development logs to the
// console and sends nothing; staging and production hand the message to the provider. These
// three values are all the provider needs, because the adapter speaks a generic HTTP contract
// rather than a vendor's SDK — so naming a vendor is setting them and changing no code
// (Docs/11 §7).
//
// They are deliberately not validated here. An empty base URL is correct in development, and in
// staging it is caught where the adapter is constructed, which is at startup and with a message
// that says which variable is missing.
type Email struct {
	// Transport is which implementation carries the message: console, http, or smtp
	// (SHIP-187a, SHIP-187b).
	//
	// Left unset it is derived from SHIPPER_ENV — console in development, http everywhere
	// else — which is exactly what email.UseConsole decided before this field existed. What
	// it adds is the ability to say otherwise: a demonstration instance runs hardened, with
	// real signing keys and every deployment-safety rule in force, and still sends its mail
	// to a local catcher over SMTP. That combination could not be expressed while the
	// transport was a function of the environment.
	Transport Transport

	// ProviderBaseURL is the root of the vendor's API. Empty in development, where nothing
	// reaches it.
	ProviderBaseURL string

	// ProviderAPIKey is presented as a bearer credential and is never logged.
	ProviderAPIKey string

	// Sender is the From address every message is dispatched with. One address for the whole
	// service in the MVP; per-domain senders are a deliverability decision nobody has needed
	// to make yet.
	Sender string

	// HTTP is what the http transport needs beyond the three fields above, and it is all
	// optional: unset, it reproduces the request the adapter sent before any of it was
	// configurable. See internal/platform/email.Options for what each field means and for
	// the argument that the vendor belongs in configuration rather than in Go.
	HTTP HTTPMessaging

	// SMTP is what the smtp transport needs.
	SMTP SMTP
}

// HTTPMessaging is the vendor-shaped half of an HTTP messaging adapter (SHIP-187a).
//
// Shared by [Email] and [SMS] because the three things vendors differ on are the same three
// for both, and a buyer who has to repoint one will usually repoint the other.
type HTTPMessaging struct {
	// Path is appended to the base URL. Defaults to /messages.
	Path string

	// AuthHeader is the header the credential is presented in. Defaults to
	// Authorization (spelling:ok — HTTP header name, RFC 9110).
	AuthHeader string

	// AuthScheme prefixes the credential. Defaults to Bearer; the literal "none" sends the
	// credential bare, which is what Postmark and several SMS gateways expect.
	AuthScheme string

	// ContentType defaults to application/json.
	ContentType string

	// BodyTemplate renders the request body. Interpolate every value with {{json .Field}},
	// which supplies its own quotes — the adapter refuses a template that cannot produce
	// valid JSON when the content type says JSON.
	BodyTemplate string
}

// SMTP configures the SMTP email transport (SHIP-187b).
type SMTP struct {
	// Host and Port of the mail server. Port defaults per Encryption: 587 for starttls,
	// 465 for implicit, 25 for none.
	Host string
	Port int

	// Username and Password authenticate the session. Both empty means no authentication,
	// which is what a local catcher wants and what a real provider refuses.
	Username string
	Password string

	// Encryption is starttls, implicit, or none. Defaults to starttls. A credential over
	// "none" is refused by the adapter rather than warned about.
	Encryption string
}

// Passwords configures how the platform stores a password (SHIP-29, SHIP-15r, SHIP-147a).
//
// # Why this is its own section rather than a field of [Identity]
//
// It was `Identity.Argon2`, read from `IDENTITY_ARGON2_*`, and both names were accurate until
// SHIP-147: argon2id lived in internal/identity and the cost was that domain's. SHIP-15r moved
// the hashing to internal/passwords so that a second domain needing to hash a password would not
// be the reason for a second implementation, and SHIP-147 then hashed an administrator's password
// with the same profile — correctly, because two cost knobs are Docs/10 §3.4's failure one level
// up: two security parameters that agree by comment until somebody raises one.
//
// So there is one platform password cost, and after SHIP-147 it was spelled as one domain's.
// SHIP-147a renames it to what it is. **There is exactly one argon2id cost setting in this
// service**, and a second one appearing here is the defect this section exists to make obvious.
//
// It is configuration rather than a compiled-in constant because it is expected to change
// without a release: the cost is raised as hardware improves. Docs/10 §5 is the specification.
type Passwords struct {
	// Argon2 is the cost new password hashes are written at, wherever the platform writes one —
	// a user's password, an administrator's password, and internal/identity's phone one-time
	// codes, which are stored the same way.
	//
	// Verification does not read it. The parameters travel with each hash in its PHC string,
	// so raising these values leaves every stored password verifiable and upgrades each one at
	// its owner's next sign-in, with no migration (Docs/10 §5).
	Argon2 Argon2
}

// Identity configures credentials and sessions (SHIP-37).
//
// These are configuration rather than compiled-in constants because they are expected to change
// without a release: a signing key is rotated on a schedule or in a hurry. Docs/10 §5 is the
// specification.
//
// **The argon2id cost is deliberately not here.** It moved to [Passwords] at SHIP-147a, because
// internal/admin hashes with the same profile and may not import internal/identity — see that
// section's note.
type Identity struct {
	// AccessTokenTTL is how long an issued access token stays valid. Docs/10 §5 fixes it at
	// fifteen minutes: long enough that a phone on a poor connection is not refreshing
	// constantly, short enough that revoking a device session takes effect quickly — an
	// access token is not checked against the database, so nothing shortens it once issued.
	AccessTokenTTL time.Duration

	// AccessTokenKeys is the HMAC signing keyset, by key identifier.
	//
	// More than one so a key can be rotated by configuration: the incoming key becomes active
	// and signs, while the outgoing one stays in the set and keeps verifying the tokens it
	// already signed until the last of them expires.
	AccessTokenKeys map[string][]byte

	// AccessTokenActiveKID names the key in AccessTokenKeys that signs new tokens. It travels
	// in each token's `kid` header, which is how a verifier knows which key to use before it
	// can trust anything else in the token.
	AccessTokenActiveKID string
}

// Delivery configures the driver's job-scoped token (SHIP-107).
//
// # Why a second keyset rather than a second audience over the first
//
// Docs/10 §5 requires the driver token to have "separate signing key material **and**
// `aud=shipper-driver`", and both halves are here because either alone is weaker than the pair. The
// audience is what refuses a mobile token presented to a driver route and a driver token presented
// to a mobile one; separate key material is what makes that refusal survive a mistake in the
// audience check, because neither verifier can even produce a valid signature over the other's
// tokens. [loader.validate] refuses a configuration in which the two sets share a secret, so a
// deployment cannot arrive at one keyset by copying a value.
//
// The shape mirrors [Identity]'s three access-token fields deliberately: it is the same rotation
// mechanism, read the same way, so an operator rotating one already knows how to rotate the other.
type Delivery struct {
	// DriverTokenTTL is how long a job-scoped link works for.
	//
	// **Seven days, and it is the one number here that is a product judgement rather than a
	// mechanism.** The reasoning, because the obvious comparison — identity's fifteen minutes —
	// is the wrong one:
	//
	//   - There is no refresh. A driver has no account and holds no second credential, so this
	//     TTL is the entire life of their access rather than the short leg of a pair. Fifteen
	//     minutes would mean a link that expires before the provider has finished forwarding it.
	//   - It has to outlast a delivery. Australian road freight is quoted in days across the
	//     long-haul corridors, and a token that lapsed mid-run would strand the driver with
	//     milestones they cannot record — which Docs/02 §3.1 treats as work that must not be
	//     lost.
	//   - It does not have to outlast the *job*. The 72-hour auto-complete (Docs/02 §6.1) runs
	//     after delivery and the driver plays no part in it, so nothing needs the link past the
	//     drop-off.
	//   - Longer is a real cost. The link is forwarded by whatever channel the provider uses, so
	//     it lands in a message thread and stays there; a week means a forwarded link stops
	//     working within a week rather than for the life of the handset.
	//
	// A driver who needs one after it lapses gets a fresh link from the provider, which is
	// SHIP-109 — and that ticket, rather than a longer TTL, is the answer to "it expired".
	DriverTokenTTL time.Duration

	// DriverTokenKeys is the HMAC signing keyset for driver tokens, by key identifier.
	//
	// Rotated exactly as the access-token keyset is, with one difference worth knowing before
	// doing it: an access token lives fifteen minutes, so an outgoing key can be dropped within
	// the hour. A driver token lives a week, and dropping its key sooner breaks every link
	// already forwarded — with nothing at the other end that can refresh.
	DriverTokenKeys map[string][]byte

	// DriverTokenActiveKID names the key in DriverTokenKeys that signs new tokens.
	DriverTokenActiveKID string
}

// SMS configures the text-message adapter (SHIP-35), first consumed by the phone verification
// code SHIP-34 sends.
//
// The same shape as [Email] and chosen the same way — from Env, not from here. It matters more
// here than it does for email: a message costs money and reaches a real handset, so an
// environment that dispatched by accident would be a bill as well as a nuisance to whoever last
// used that number for testing.
type SMS struct {
	// Transport is which implementation carries the message: console or http (SHIP-187a).
	//
	// There is no smtp here for the obvious reason, and no silent fallback either — a
	// deployment that wants an instance not to send real text messages says so by setting
	// this to console, which is a visible choice in a configuration file rather than an
	// inference from the environment.
	Transport Transport

	// ProviderBaseURL is the root of the gateway's API. Empty in development.
	ProviderBaseURL string

	// ProviderAPIKey is presented as a bearer credential and is never logged.
	ProviderAPIKey string

	// Sender is what the message appears to come from — an alphanumeric sender ID or an
	// originating number, depending on what the gateway and the destination country permit.
	// Australia allows both; the choice is made with the vendor.
	Sender string

	// HTTP is the vendor-shaped half, all optional. SMS gateways vary by country as well as
	// by vendor, so this is the adapter a buyer is most likely to have to repoint.
	HTTP HTTPMessaging
}

// Transport names which implementation of a messaging adapter is built (SHIP-187a).
//
// # Why this is configuration and no longer a function of the environment
//
// It was email.UseConsole(env) and sms.UseConsole(env), which answered well for the two
// cases that existed: a developer must not send real mail, and a deployment must not
// silently swallow it. What it could not express is the third case, which arrived with the
// demonstration environment — an instance that is hardened in every respect, running with
// real signing keys under every deployment-safety rule, and deliberately sending its mail
// somewhere free.
//
// The safe default is kept exactly as it was: unset, this resolves to console in
// development and http everywhere else. Choosing wrongly towards the console costs a
// developer a puzzled minute; choosing wrongly towards the provider sends real email, and
// real text messages cost money per send and wake a real handset.
//
// # Geocoding shares the type and not the default
//
// SHIP-192 brought the third adapter of this shape under the same variable, and it defaults
// the other way: unset means [TransportStub], in every environment including production. The
// asymmetry is the metering. An unsent email is noticed by whoever was waiting for it; an
// address resolved against a vendor nobody chose is noticed on an invoice, and by then it has
// already been charged for. So the messaging adapters may infer a transport from the
// environment and geocoding may not — which is why staging and production have to name theirs
// outright rather than inherit one. That rule is with the other deployment-safety rules at the
// foot of validate.
type Transport string

const (
	// TransportConsole writes the message to the log and sends nothing.
	TransportConsole Transport = "console"

	// TransportHTTP posts the message to a vendor's HTTP API.
	TransportHTTP Transport = "http"

	// TransportSMTP hands the message to a mail server. Email only.
	TransportSMTP Transport = "smtp"

	// TransportStub answers in-process and reaches no vendor. Geocoding only.
	//
	// It is not [TransportConsole] under another name, and the difference is the reason
	// this is a separate value rather than a reuse. The console transport declines to act
	// and says so in the log; the stub *answers*, with a coordinate that is stable,
	// plausible, inside Australia and entirely fictional. A deployment running it is not
	// one that sends nothing — it is one whose jobs carry coordinates that mean nothing,
	// and that is far harder to notice than silence.
	TransportStub Transport = "stub"
)

// Push configures the Firebase Cloud Messaging adapter (SHIP-139), consumed by cmd/notifier.
//
// The same shape as [Email] and [SMS] and chosen the same way — from Env, not from here. It matters
// for the same reason SMS does and more sharply: a message reaches a real handset, and unlike an
// email there is no address to inspect afterwards to work out whose.
//
// # Why there is a credential rather than a key file
//
// FCM's HTTP v1 API takes a short-lived OAuth access token, and Google's way of obtaining one is to
// exchange a service-account JSON key for it. **Nothing in this repository performs that exchange**:
// it needs golang.org/x/oauth2/google, which is a module and therefore a go.mod change SHIP-139's
// branch could not make. See internal/platform/push/doc.go.
//
// So this holds the bearer credential itself, and whatever mints it is outside this service today.
// A service-account key is on CLAUDE.md's never-commit list and this variable is not where one
// would go: it is a token, it expires, and it belongs in the CI secret store like every other
// credential (Docs/06 §5.2).
type Push struct {
	// ProjectID is the Firebase project. Empty means the no-op implementation, whatever the
	// environment — which is the state of every deployment today, because no project exists.
	ProjectID string

	// BaseURL overrides Google's, for a test or a proxy. Empty means push.DefaultBaseURL.
	BaseURL string

	// Credential is presented to FCM as a bearer token and is never logged.
	Credential string
}

// Argon2 is the password hashing cost.
//
// It mirrors passwords.Argon2Profile, which is the type the hasher takes — internal/identity's
// until SHIP-15r moved the hashing to infrastructure. Two shapes rather than
// one shared type on purpose: internal/config does not import a domain, the domain does not
// import configuration, and the two meet in cmd/api — which is the rule that keeps every domain
// independently buildable (Docs/06 §4.1).
type Argon2 struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// App is what the mobile client is told about itself at launch (SHIP-167).
//
// It lives in configuration rather than in code because Docs/07 §6 makes raising the floor "an
// operational decision with an owner, not a side effect of a deploy" — and because Flutter has
// no over-the-air path for Dart code, so anything that has to change under pressure has to
// change server-side (Docs/06 §5.3).
type App struct {
	// MinimumIOSBuild and MinimumAndroidBuild are build numbers, not version strings.
	//
	// Docs/07 §8 requires every build to carry a unique, monotonically increasing build
	// number, which makes the comparison an integer one. Comparing semantic versions
	// would mean agreeing on an ordering for pre-release suffixes, and getting that
	// slightly wrong would either lock out a valid build or admit one that should have
	// been blocked.
	MinimumIOSBuild     int
	MinimumAndroidBuild int

	// IOSStoreURL and AndroidStoreURL are where a blocked build sends the user.
	//
	// Empty during the pilot: distribution is TestFlight and Play internal testing with no
	// public listing (Docs/01 §8), so there is no store page to link to yet. The client
	// shows the prompt without a link when these are blank, which is why an empty value is
	// allowed rather than refused at startup.
	IOSStoreURL     string
	AndroidStoreURL string

	// UnsyncedNudgeAfter is how long an update may sit unsynced on a handset before the
	// provider is prompted to go and find signal (SHIP-167a).
	//
	// Docs/02 §3.1's second rung. The document is the authority for four hours; this is the
	// dial that moves it without a store release, which is the whole of SHIP-167a's argument:
	// it is an operations tuning knob, and Flutter has no over-the-air path for Dart code.
	//
	// **The client compiles a default in and that is not a duplicate of this value.** The
	// compiled number is the floor for an install that has never once been online — the only
	// case it is used for — because the prompt fires on a handset that by assumption has no
	// connection and so cannot ask at the moment of use.
	UnsyncedNudgeAfter time.Duration

	// ProofCompressionBudgetBytes is the size a proof photograph is compressed towards on the
	// device before upload (Docs/01 §5.2, SHIP-167a).
	//
	// **A budget, and [Storage.MaxUploadBytes] is a bound. They are different questions.** The
	// bound is the size above which the platform refuses to sign an upload at all; this is what
	// a driver on a metered connection in a yard should be asked to send. The budget is
	// therefore much the smaller of the two, and validate refuses a deployment that inverts
	// them — a client told to aim above what the platform will accept would compress to a size
	// guaranteed to be rejected.
	//
	// It is a target rather than a limit on the client side: Docs/01 §4.4 makes the photograph
	// the difference between a delivery that can be completed and one that cannot, so an image
	// that overshoots is uploaded anyway and the platform's bound is what actually refuses one.
	ProofCompressionBudgetBytes int
}

// HTTP configures the public API listener.
type HTTP struct {
	Port int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// ShutdownTimeout is how long in-flight requests get to finish after a termination
	// signal before they are dropped.
	ShutdownTimeout time.Duration
}

// Addr is the listen address, in the form ":8080".
func (h HTTP) Addr() string { return ":" + strconv.Itoa(h.Port) }

// Log configures structured logging (SHIP-9).
type Log struct {
	Level slog.Level

	// Format is "json" or "text". Anything that ships logs to Datadog must use json
	// (SHIP-174); text exists purely because a developer reading a terminal is not a
	// log aggregator.
	Format string
}

// Database configures the connection to PostgreSQL, which is the source of truth for
// every business decision (Docs/06 §4).
type Database struct {
	URL string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Redis holds refresh-token state, the device registry, idempotency keys, and rate
// limits. It must never be the only copy of a job, bid, or status (Docs/06 §2.1, §4).
type Redis struct {
	URL string
}

// Kafka is the event backbone for notifications, reporting, and background processing.
type Kafka struct {
	Brokers []string

	// ReplicationFactor is how many copies of each partition the topic set is created with
	// (SHIP-135, folded in at SHIP-15m).
	//
	// **One is correct for the single-broker compose stack and wrong everywhere else**; a
	// deployed cluster wants three. It is the only number in the topic set that genuinely
	// differs between environments — the partition count and the retention are properties of
	// the catalogue and are the same on every broker.
	//
	// It arrived as a `cmd/topics -replication` flag rather than as a field, because
	// internal/config is a shared surface a domain branch may not edit (Docs/10 §9.2) and the
	// track that needed it was a domain branch. **The flag is still there and still wins**: an
	// operator applying the topic set to one cluster by hand should not have to set an
	// environment variable to do it. This is its default, which is what makes the deployment
	// that runs the step need no arguments.
	//
	// Read only by cmd/topics. cmd/api and cmd/worker load it and never look at it, which is
	// ordinary — Load reads the whole environment for every binary rather than a subset per
	// command, so that one misconfiguration is refused everywhere rather than in one place.
	ReplicationFactor int
}

// DefaultKafkaReplicationFactor is one, matching internal/events.DefaultReplicationFactor.
//
// Two constants rather than one shared, for the same reason [minimumSigningKeyBytes] is duplicated:
// internal/config has no internal dependencies at all, and it is loaded by every binary. Importing
// the event catalogue to read a `1` would pull internal/events and internal/db — and with them the
// database driver — into the configuration of processes that publish nothing.
//
// The drift that buys is closed where the two meet: cmd/topics imports both, and
// TestConfigurationCarriesTheCatalogueDefault fails there if they stop agreeing.
const DefaultKafkaReplicationFactor = 1

// maxKafkaReplicationFactor is a typo guard rather than a limit anybody should reach.
//
// A factor larger than the cluster is refused by the broker at create time, so the real bound is
// the number of brokers and this cannot know it. What it does catch is 30 typed for 3, before a
// connection is opened and with the variable named in the message.
const maxKafkaReplicationFactor = 10

// Idempotency configures how long the platform remembers what it answered a
// state-changing request, so that a retry replays rather than repeats it (SHIP-15).
type Idempotency struct {
	// TTL is how long a completed response stays replayable.
	//
	// It has to outlast the client's retry behaviour, not the request. A phone can be
	// out of coverage for hours and drain its queue on the drive home (SHIP-124,
	// SHIP-125), so this is measured in hours rather than minutes.
	TTL time.Duration

	// InFlightTTL bounds how long a claim survives with no response recorded against it,
	// which is what happens when the process handling the original request dies. Until
	// it lapses, retries of that request are refused — so it wants to be comfortably
	// longer than the slowest handler and much shorter than TTL.
	InFlightTTL time.Duration
}

// credentialBearingDefaults are variables whose built-in defaults embed a local
// throwaway credential. They make a fresh clone work against docker-compose and are
// refused outside development — see loader.validate.
var credentialBearingDefaults = []string{
	"DATABASE_URL",
	"REDIS_URL",
	"IDENTITY_ACCESS_TOKEN_KEYS",
	"DELIVERY_DRIVER_TOKEN_KEYS",
	// Both halves, unlike DATABASE_URL which carries user and password in one variable. A
	// deployment that set the secret and left the identifier as the local throwaway would
	// authenticate as nobody, and the symptom would be the first upload rather than the start-up.
	"STORAGE_ACCESS_KEY_ID",
	"STORAGE_SECRET_ACCESS_KEY",
}

// The development signing key, which is in the repository and therefore public.
//
// It exists for the same reason the database password does: a fresh clone has to run without
// setup. validate refuses it outside development, so the failure mode it is guarding against —
// a staging deployment signing tokens anybody can forge — is a startup error rather than a
// discovery.
const (
	developmentActiveKID  = "dev"
	developmentSigningKey = "shipper-local-development-signing-key-not-a-secret"

	// The driver token's development key, and it is a **different string** rather than the same
	// one under a second variable (SHIP-107).
	//
	// That is the whole of Docs/10 §5's "separate signing key material", made true in the
	// environment a developer actually runs. Sharing the development key would leave the two
	// token systems separated by the audience alone on every machine anybody works on, which is
	// precisely the configuration the tests deliberately construct to prove the audience is
	// enough — and it should not be the default, because then nothing would be testing the pair.
	//
	// The identifier differs too, so a decoded header says which system signed a token without
	// anybody having to check the key.
	developmentDriverActiveKID  = "driver-dev"
	developmentDriverSigningKey = "shipper-local-development-driver-token-key-not-a-secret"
)

// developmentSigningKeys builds a fresh map each time rather than sharing one package-level
// value, so that a caller holding the loaded configuration cannot change what the next Load
// returns.
func developmentSigningKeys() map[string][]byte {
	return map[string][]byte{developmentActiveKID: []byte(developmentSigningKey)}
}

// developmentDriverSigningKeys is the same, for the driver token's own keyset.
func developmentDriverSigningKeys() map[string][]byte {
	return map[string][]byte{developmentDriverActiveKID: []byte(developmentDriverSigningKey)}
}

// minimumSigningKeyBytes mirrors the same constant in internal/identity, which is where the
// keyset enforces it.
//
// Two copies rather than one shared constant, because internal/config does not import a domain
// and the domain does not import configuration (Docs/06 §4.1). Checking it here as well means a
// short key is refused at startup rather than at the first sign-in.
const minimumSigningKeyBytes = 32

// The development object-store credential, on exactly the terms the signing keys above are on:
// it is in this repository and therefore public, it exists so that a fresh clone runs without
// setup, and validate refuses it outside development.
//
// It matches MINIO_ROOT_USER and MINIO_ROOT_PASSWORD in deploy/docker-compose.yml, the way
// DATABASE_URL matches POSTGRES_USER and POSTGRES_PASSWORD. The password is not `shipper` twice
// over because MinIO refuses a root password shorter than eight characters.
const (
	developmentStorageAccessKeyID     = "shipper"
	developmentStorageSecretAccessKey = "shipperminio"
)

// maxPresignTTL bounds the one credential in this file that is a URL.
//
// Nothing revokes a pre-signed URL once it is signed — Docs/06 §5.2 puts the bytes outside the
// API entirely, so the signature *is* the authorisation and no server-side check runs when it is
// redeemed. The cap is an hour rather than a number close to the fifteen-minute default, for the
// same reason maxDriverTokenTTL is thirty days: the default is a judgement operations may
// legitimately move, and this only refuses the values that would defeat the design.
const maxPresignTTL = time.Hour

// The upload-size bounds, and the range is deliberately wide.
//
// A phone photograph compressed for a metered connection (Docs/01 §5.2) is one or two megabytes,
// so ten is generous. The floor exists because a value below it refuses every real photograph —
// a configuration mistake that presents as a driver unable to finish a delivery — and the ceiling
// because past it the number has stopped describing a photograph and started describing how much
// of somebody's data allowance a single upload may consume.
const (
	defaultMaxUploadBytes  = 10 << 20 // 10 MiB
	smallestMaxUploadBytes = 64 << 10 // 64 KiB
	largestMaxUploadBytes  = 64 << 20 // 64 MiB
)

// The two operational numbers the mobile client is told about itself (SHIP-167a).
//
// They are defaults for the *platform*, and the client compiles its own copies for an install
// that has never been online. The two sets are held together by a test on each side rather than
// by a shared constant — there is no mechanism that could share one, because the point of the
// endpoint is that the values may diverge the moment operations move them.
//
// The nudge's four hours is Docs/02 §3.1's, and the document is the authority for the number.
//
// The proof budget's bounds are the same shape as the upload bounds above and are narrower: a
// budget below the floor would compress a licence plate past legibility, which Docs/01 §4.4 makes
// the difference between a delivery that can be completed and one that cannot; above the ceiling
// the number has stopped describing a photograph a driver on a metered connection should send.
const (
	defaultUnsyncedNudgeAfter = 4 * time.Hour

	defaultProofBudgetBytes  = 1 << 20   // 1 MiB
	smallestProofBudgetBytes = 128 << 10 // 128 KiB
	largestProofBudgetBytes  = 8 << 20   // 8 MiB
)

// maxDriverTokenTTL is a typo guard on the one TTL nothing can shorten once issued.
//
// Thirty days rather than a number close to the seven-day default, because the default is a product
// judgement that operations may legitimately move and this is only refusing the values that would
// defeat the design — `720h` typed for `72h`, or a duration somebody meant as "no expiry".
const maxDriverTokenTTL = 30 * 24 * time.Hour

// Load reads configuration from the process environment.
//
// It reports every problem it finds rather than stopping at the first, because a
// misconfigured deployment usually has more than one thing wrong with it and discovering
// them one restart at a time is miserable.
func Load() (*Config, error) {
	l := &loader{defaulted: map[string]bool{}}

	cfg := &Config{
		Env: Environment(strings.ToLower(l.str("SHIPPER_ENV", string(Development)))),
		HTTP: HTTP{
			Port:            l.port("HTTP_PORT", 8080),
			ReadTimeout:     l.duration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    l.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     l.duration("HTTP_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: l.duration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Log: Log{
			Level:  l.level("LOG_LEVEL", slog.LevelInfo),
			Format: strings.ToLower(l.str("LOG_FORMAT", "json")),
		},
		Database: Database{
			URL:             l.str("DATABASE_URL", "postgres://shipper:shipper@localhost:5432/shipper?sslmode=disable"),
			MaxOpenConns:    l.positiveInt("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    l.positiveInt("DATABASE_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: l.duration("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Redis: Redis{
			URL: l.str("REDIS_URL", "redis://localhost:6379/0"),
		},
		Kafka: Kafka{
			Brokers: l.csv("KAFKA_BROKERS", []string{"localhost:29092"}),
			ReplicationFactor: l.boundedInt("KAFKA_REPLICATION_FACTOR",
				DefaultKafkaReplicationFactor, 1, maxKafkaReplicationFactor),
		},
		Idempotency: Idempotency{
			TTL:         l.duration("IDEMPOTENCY_TTL", 24*time.Hour),
			InFlightTTL: l.duration("IDEMPOTENCY_IN_FLIGHT_TTL", 60*time.Second),
		},
		Passwords: Passwords{
			// m=64 MiB, t=3, p=4 — Docs/10 §5. The bounds are the range
			// internal/passwords will run; a value outside them is refused here rather
			// than at the first sign-in.
			Argon2: Argon2{
				MemoryKiB:   uint32(l.boundedInt("PASSWORDS_ARGON2_MEMORY_KIB", 64*1024, 1024, 1<<20)),
				Iterations:  uint32(l.boundedInt("PASSWORDS_ARGON2_ITERATIONS", 3, 1, 64)),
				Parallelism: uint8(l.boundedInt("PASSWORDS_ARGON2_PARALLELISM", 4, 1, 255)),
			},
		},
		Identity: Identity{
			AccessTokenTTL:       l.duration("IDENTITY_ACCESS_TOKEN_TTL", 15*time.Minute),
			AccessTokenKeys:      l.signingKeys("IDENTITY_ACCESS_TOKEN_KEYS", developmentSigningKeys()),
			AccessTokenActiveKID: l.str("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", developmentActiveKID),
		},
		Delivery: Delivery{
			// Seven days — see [Delivery.DriverTokenTTL] for why it is measured against a
			// delivery rather than against a session.
			DriverTokenTTL:  l.duration("DELIVERY_DRIVER_TOKEN_TTL", 7*24*time.Hour),
			DriverTokenKeys: l.signingKeys("DELIVERY_DRIVER_TOKEN_KEYS", developmentDriverSigningKeys()),
			DriverTokenActiveKID: l.str("DELIVERY_DRIVER_TOKEN_ACTIVE_KID",
				developmentDriverActiveKID),
		},
		Email: Email{
			// Empty is resolved against the environment below, once Env is known.
			Transport:       Transport(l.str("EMAIL_TRANSPORT", "")),
			ProviderBaseURL: l.str("EMAIL_PROVIDER_BASE_URL", ""),
			ProviderAPIKey:  l.str("EMAIL_PROVIDER_API_KEY", ""),
			Sender:          l.str("EMAIL_SENDER", "no-reply@shipper.com.au"),
			HTTP: HTTPMessaging{
				Path:         l.str("EMAIL_PROVIDER_PATH", ""),
				AuthHeader:   l.str("EMAIL_PROVIDER_AUTH_HEADER", ""),
				AuthScheme:   l.str("EMAIL_PROVIDER_AUTH_SCHEME", ""),
				ContentType:  l.str("EMAIL_PROVIDER_CONTENT_TYPE", ""),
				BodyTemplate: l.str("EMAIL_PROVIDER_BODY_TEMPLATE", ""),
			},
			SMTP: SMTP{
				Host:       l.str("EMAIL_SMTP_HOST", ""),
				Port:       l.port("EMAIL_SMTP_PORT", 0),
				Username:   l.str("EMAIL_SMTP_USERNAME", ""),
				Password:   l.str("EMAIL_SMTP_PASSWORD", ""),
				Encryption: l.str("EMAIL_SMTP_ENCRYPTION", ""),
			},
		},
		SMS: SMS{
			Transport:       Transport(l.str("SMS_TRANSPORT", "")),
			ProviderBaseURL: l.str("SMS_PROVIDER_BASE_URL", ""),
			ProviderAPIKey:  l.str("SMS_PROVIDER_API_KEY", ""),
			Sender:          l.str("SMS_SENDER", "Shipper"),
			HTTP: HTTPMessaging{
				Path:         l.str("SMS_PROVIDER_PATH", ""),
				AuthHeader:   l.str("SMS_PROVIDER_AUTH_HEADER", ""),
				AuthScheme:   l.str("SMS_PROVIDER_AUTH_SCHEME", ""),
				ContentType:  l.str("SMS_PROVIDER_CONTENT_TYPE", ""),
				BodyTemplate: l.str("SMS_PROVIDER_BODY_TEMPLATE", ""),
			},
		},
		Push: Push{
			ProjectID:  l.str("PUSH_PROJECT_ID", ""),
			BaseURL:    l.str("PUSH_BASE_URL", ""),
			Credential: l.str("PUSH_CREDENTIAL", ""),
		},
		Geocoding: Geocoding{
			Transport:       Transport(l.str("GEOCODING_TRANSPORT", "")),
			ProviderBaseURL: l.str("GEOCODING_BASE_URL", ""),
			ProviderAPIKey:  l.str("GEOCODING_API_KEY", ""),
		},
		Pagination: Pagination{
			// The bounds are the range the pagination package will run, not taste. One
			// row per page is legal and useless; the ceiling stops a configuration
			// mistake becoming an unbounded query.
			DefaultPageSize: l.boundedInt("PAGINATION_DEFAULT_PAGE_SIZE", 20, 1, 500),
			MaxPageSize:     l.boundedInt("PAGINATION_MAX_PAGE_SIZE", 100, 1, 500),
		},
		RateLimits: RateLimits{
			// The default is the document. A deployment that sets neither runs exactly
			// the figures Docs/12 §3 argues for, which is what makes the reasons there
			// worth reading.
			//
			// The bounds are what an incident could plausibly want rather than taste. A
			// hundredth is a near-freeze that still lets a token through — the class
			// keeps a floor of one, so nothing becomes an off switch — and a hundredfold
			// is well past any legitimate loosening, which makes a misplaced decimal
			// point a startup failure rather than an unlimited API.
			BurstScale: l.boundedFloat("RATE_LIMIT_BURST_SCALE", 1.0, 0.01, 100.0),
			RateScale:  l.boundedFloat("RATE_LIMIT_RATE_SCALE", 1.0, 0.01, 100.0),
		},
		TrustedProxy: TrustedProxy{
			// Both default to trusting nothing, which is RemoteAddr alone and is the
			// behaviour that shipped at SHIP-47. A deployment that says nothing is not
			// exposed by saying nothing; it merely shares a bucket behind a balancer.
			//
			// Eight is well past any real chain — a CDN, a WAF, a load balancer and a
			// service mesh is four — and its job is to make a misplaced digit a startup
			// failure rather than a count that walks off the end of every header and
			// silently falls back.
			Hops:     l.boundedInt("TRUSTED_PROXY_HOPS", 0, 0, 8),
			Networks: l.prefixes("TRUSTED_PROXY_NETWORKS", nil),
		},
		Storage: Storage{
			// The published host port, not the compose-internal name — see [Storage.Endpoint].
			// It matches the Makefile's STORAGE_ENDPOINT, which derives the port rather than
			// assuming it.
			Endpoint:        l.str("STORAGE_ENDPOINT", "http://localhost:9000"),
			Bucket:          l.str("STORAGE_BUCKET", "shipper-dev"),
			Region:          l.str("STORAGE_REGION", "ap-southeast-2"),
			AccessKeyID:     l.str("STORAGE_ACCESS_KEY_ID", developmentStorageAccessKeyID),
			SecretAccessKey: l.str("STORAGE_SECRET_ACCESS_KEY", developmentStorageSecretAccessKey),
			UsePathStyle:    l.boolean("STORAGE_USE_PATH_STYLE", true),
			PresignTTL:      l.duration("STORAGE_PRESIGN_TTL", 15*time.Minute),
			DownloadTTL:     l.duration("STORAGE_DOWNLOAD_TTL", 5*time.Minute),
			MaxUploadBytes: int64(l.boundedInt("STORAGE_MAX_UPLOAD_BYTES",
				defaultMaxUploadBytes, smallestMaxUploadBytes, largestMaxUploadBytes)),
			AcceptedContentTypes: l.csv("STORAGE_ACCEPTED_CONTENT_TYPES",
				[]string{"image/jpeg", "image/png", "image/heic"}),
		},
		Verification: Verification{
			// No default, and the nil map is the shipping configuration until X-4 is
			// answered. See [Verification].
			ExpiryLeadTimes: l.durations("VERIFICATION_EXPIRY_LEAD_TIMES", nil),
		},
		Goods: Goods{
			// The provisional list is the default and one variable replaces it whole.
			// See [Goods] for why this one has a default where ExpiryLeadTimes above
			// deliberately has none.
			Categories: l.goods("GOODS_CATEGORIES", provisionalGoodsCategories),
		},
		App: App{
			MinimumIOSBuild:     l.positiveInt("MIN_SUPPORTED_IOS_BUILD", 1),
			MinimumAndroidBuild: l.positiveInt("MIN_SUPPORTED_ANDROID_BUILD", 1),
			IOSStoreURL:         l.str("IOS_STORE_URL", ""),
			AndroidStoreURL:     l.str("ANDROID_STORE_URL", ""),
			UnsyncedNudgeAfter:  l.duration("UNSYNCED_NUDGE_AFTER", defaultUnsyncedNudgeAfter),
			ProofCompressionBudgetBytes: l.boundedInt("PROOF_COMPRESSION_BUDGET_BYTES",
				defaultProofBudgetBytes, smallestProofBudgetBytes, largestProofBudgetBytes),
		},
	}

	resolveTransports(cfg)
	l.validate(cfg)

	if err := errors.Join(l.errs...); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

// resolveTransports fills in a messaging transport that was not set explicitly (SHIP-187a).
//
// Separate from the loader literal because it needs Env, which is read inside that same
// literal and cannot be referred to from within it. Separate from validate because it
// assigns rather than checks, and a step that does both is one nobody can read.
//
// The rule reproduces email.UseConsole exactly, including its bias: anything that is not
// recognisably a deployed environment gets the console. Choosing wrongly towards the
// console costs a developer a puzzled minute, and choosing wrongly towards the provider
// sends real messages from a machine that should never have had the credential.
func resolveTransports(cfg *Config) {
	def := TransportHTTP
	if cfg.Env.IsDevelopment() || !cfg.Env.valid() {
		def = TransportConsole
	}
	if cfg.Email.Transport == "" {
		cfg.Email.Transport = def
	}
	if cfg.SMS.Transport == "" {
		cfg.SMS.Transport = def
	}

	// Geocoding does not read def, and the difference is deliberate rather than an omission.
	//
	// A messaging transport inferred wrongly is loud in one direction and free in the other:
	// the console keeps a message from arriving, which whoever was waiting for it reports. A
	// geocoding transport inferred wrongly towards the vendor is silent and metered — it
	// resolves every address correctly and bills for it, so the first report is an invoice.
	//
	// So the stub is the default everywhere, and the deployment-safety rule in validate then
	// refuses to let staging or production actually run on it without saying so. Defaulting
	// safe and requiring a deliberate choice are not alternatives here; the second is what
	// stops the first from quietly serving fictional coordinates to real customers.
	if cfg.Geocoding.Transport == "" {
		cfg.Geocoding.Transport = TransportStub
	}
}

// LogValue renders the configuration for a startup log line with credentials removed.
//
// Implementing slog.LogValuer rather than exposing a String method means there is no
// convenient way to print the unredacted struct by accident.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", string(c.Env)),
		slog.Int("http_port", c.HTTP.Port),
		slog.String("log_level", c.Log.Level.String()),
		slog.String("log_format", c.Log.Format),
		slog.String("database_url", redactURL(c.Database.URL)),
		slog.String("redis_url", redactURL(c.Redis.URL)),
		slog.String("kafka_brokers", strings.Join(c.Kafka.Brokers, ",")),
		// Worth a line for the same reason the argon2 cost is: a deployment that has
		// silently fallen back to a single replica is otherwise invisible until a broker
		// is lost.
		slog.Int("kafka_replication_factor", c.Kafka.ReplicationFactor),
		slog.Duration("idempotency_ttl", c.Idempotency.TTL),
		slog.Duration("idempotency_in_flight_ttl", c.Idempotency.InFlightTTL),
		// The cost is worth having in the startup line: a deployment that has silently
		// fallen back to a cheap profile is otherwise invisible.
		slog.String("argon2", fmt.Sprintf("m=%d,t=%d,p=%d",
			c.Passwords.Argon2.MemoryKiB, c.Passwords.Argon2.Iterations, c.Passwords.Argon2.Parallelism)),
		slog.Duration("access_token_ttl", c.Identity.AccessTokenTTL),
		// The identifier and the count, never the key material. Which key is active and how
		// many are loaded is exactly what a rotation needs to confirm from a log line, and
		// neither says anything an attacker can sign with.
		slog.String("access_token_active_kid", c.Identity.AccessTokenActiveKID),
		slog.Int("access_token_keys", len(c.Identity.AccessTokenKeys)),
		// The second keyset, on the same terms. Two identifiers in one startup line is also the
		// cheapest confirmation that the two token systems are configured separately.
		slog.Duration("driver_token_ttl", c.Delivery.DriverTokenTTL),
		slog.String("driver_token_active_kid", c.Delivery.DriverTokenActiveKID),
		slog.Int("driver_token_keys", len(c.Delivery.DriverTokenKeys)),
		// Whether a base URL is set, not what it is, and never the key. "Email is going to
		// the console" is the line somebody needs when a verification message has not
		// arrived, and it is the one thing a hostname would not tell them.
		// The transport is in the startup line because it is the one setting that decides
		// whether anything leaves the building, and an instance quietly on the console is
		// indistinguishable from a working one until somebody waits for a message.
		slog.String("email_transport", string(c.Email.Transport)),
		slog.String("sms_transport", string(c.SMS.Transport)),
		slog.Bool("email_provider_configured", c.Email.ProviderBaseURL != ""),
		slog.String("email_sender", c.Email.Sender),
		slog.Bool("sms_provider_configured", c.SMS.ProviderBaseURL != ""),
		slog.String("sms_sender", c.SMS.Sender),
		// The project, which is not secret and is the value somebody needs when a push has
		// not arrived — a wrong one is a 404 per message, which SHIP-139 reads as the token
		// being dead. Never the credential.
		slog.String("push_project", c.Push.ProjectID),
		slog.Bool("push_credential_configured", c.Push.Credential != ""),
		// The bucket and the endpoint, never the credential. The bucket is the one worth
		// insisting on: it is per-worktree in development, and a process writing into the
		// wrong one succeeds at everything and puts the objects where nobody looks.
		slog.String("storage_endpoint", redactURL(c.Storage.Endpoint)),
		slog.String("storage_bucket", c.Storage.Bucket),
		slog.String("storage_region", c.Storage.Region),
		slog.Bool("storage_path_style", c.Storage.UsePathStyle),
		slog.Duration("storage_presign_ttl", c.Storage.PresignTTL),
		slog.Duration("storage_download_ttl", c.Storage.DownloadTTL),
		slog.Int("verification_expiry_lead_times", len(c.Verification.ExpiryLeadTimes)),

		// Both halves, because the interesting failure is a catalogue that parsed and
		// carries nothing — which a total would hide behind a plausible-looking number.
		slog.Int("goods_categories", len(c.Goods.Categories)),
		slog.Int("goods_categories_carried", c.Goods.carriedCount()),
		slog.Int64("storage_max_upload_bytes", c.Storage.MaxUploadBytes),
	)
}

// redactURL replaces the password in a URL's userinfo with "xxxxx", leaving the rest
// intact so the host and database name stay diagnosable.
//
// A URL that cannot be parsed is reported as "<unparseable>" rather than echoed back:
// the one thing worse than an unhelpful log line is one that leaks the credential it
// failed to parse.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	return u.String()
}

// loader accumulates parse failures and records which variables fell back to a default,
// so validate can refuse credential-bearing defaults outside development.
type loader struct {
	errs      []error
	defaulted map[string]bool
}

func (l *loader) lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		l.defaulted[key] = true
		return "", false
	}
	return strings.TrimSpace(v), true
}

func (l *loader) str(key, def string) string {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	return v
}

func (l *loader) csv(key string, def []string) []string {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		l.errf("%s: must list at least one value", key)
		return def
	}
	return out
}

// prefixes reads a comma-separated list of CIDR networks, accepting a bare address as the network
// containing only itself (SHIP-183b).
//
// A bare address is accepted because that is what a deployment with one fixed balancer will write,
// and making them append /32 to it is a way to be refused for being right. It is normalised to a
// prefix here so that the matching code has one shape to handle rather than two.
//
// Every prefix is stored masked, so `10.1.2.3/8` is read as `10.0.0.0/8` rather than as a prefix
// whose host bits quietly make it match nothing. An unparseable entry is an error rather than a
// skip: a typo in an allow-list is a proxy that stops being trusted, and the symptom of that is
// every request behind it sharing one bucket — a throttling incident whose cause is one character
// in an environment variable nobody would think to re-read.
func (l *loader) prefixes(key string, def []netip.Prefix) []netip.Prefix {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}

	var out []netip.Prefix
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}

		addr, err := netip.ParseAddr(part)
		if err != nil {
			l.errf("%s: %q is neither a network nor an address", key, part)
			continue
		}
		// Unmapped, so that ::ffff:10.0.0.1 and 10.0.0.1 are one network rather than two
		// that never match the same request. The resolver unmaps before it compares.
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}

	if len(out) == 0 {
		l.errf("%s: must list at least one network", key)
		return def
	}
	return out
}

func (l *loader) positiveInt(key string, def int) int {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errf("%s: %q is not a whole number", key, v)
		return def
	}
	if n <= 0 {
		l.errf("%s: must be greater than zero, got %d", key, n)
		return def
	}
	return n
}

// signingKeys reads a keyset written as comma-separated `kid:base64secret` pairs.
//
// One variable rather than one per key, because the number of keys changes during a rotation and
// a scheme that needs a new variable name to add a key is a scheme nobody rotates. Standard
// base64 for the secret so that key material is bytes rather than whatever survived a shell.
//
// No error message here ever contains a secret, only the identifier it was filed under. A
// configuration error that echoes the value it could not parse puts key material in the startup
// log, where it is collected, shipped and retained.
func (l *loader) signingKeys(key string, def map[string][]byte) map[string][]byte {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}

	keys := map[string][]byte{}
	for _, pair := range strings.Split(v, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}

		kid, encoded, separated := strings.Cut(pair, ":")
		kid = strings.TrimSpace(kid)
		if !separated || kid == "" {
			l.errf("%s: expected kid:base64secret pairs; one entry has no key identifier", key)
			continue
		}
		if _, duplicate := keys[kid]; duplicate {
			l.errf("%s: the key identifier %q appears twice", key, kid)
			continue
		}

		secret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			l.errf("%s: the secret for key %q is not standard base64", key, kid)
			continue
		}
		if len(secret) < minimumSigningKeyBytes {
			l.errf("%s: the secret for key %q is %d bytes; HS256 wants at least %d",
				key, kid, len(secret), minimumSigningKeyBytes)
			continue
		}

		keys[kid] = secret
	}

	if len(keys) == 0 {
		l.errf("%s: no usable signing key", key)
		return def
	}
	return keys
}

// boundedInt reads a whole number that has a range, and reports the range when it is missed.
//
// The alternative — accept anything positive and fail later where the value is used — produces
// an error a long way from the variable that caused it. A cost parameter that is out of range is
// a startup problem, and this is startup.
func (l *loader) boundedInt(key string, def, low, high int) int {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errf("%s: %q is not a whole number", key, v)
		return def
	}
	if n < low || n > high {
		l.errf("%s: must be between %d and %d, got %d", key, low, high, n)
		return def
	}
	return n
}

// boundedFloat reads a scaling factor and refuses one outside its range.
//
// Separate from boundedInt rather than a conversion of it because the values this reads are
// deliberately fractional: "half as much burst" is the ordinary incident response, and rounding it
// to a whole number would make the only usable setting "the same or more".
//
// A non-numeric value is an error rather than a fallback to the default, for the reason every
// loader here shares: a deployment that set the variable meant something by it, and quietly running
// the default is how a tightening somebody applied during an incident turns out never to have
// applied.
func (l *loader) boundedFloat(key string, def, low, high float64) float64 {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.errf("%s: %q is not a number", key, v)
		return def
	}
	if math.IsNaN(f) || f < low || f > high {
		l.errf("%s: must be between %g and %g, got %q", key, low, high, v)
		return def
	}
	return f
}

// boolean reads a flag, accepting everything strconv.ParseBool does — 1/0, t/f, true/false,
// TRUE/FALSE — and refusing everything it does not.
//
// Refusing rather than treating an unrecognised value as false, because the values people
// actually type are "yes" and "on", and silently reading either as false would turn a switch
// somebody deliberately set into one they did not. The one setting this reads today decides
// whether a bucket is addressed by path, and getting it wrong is an upload URL that 404s.
func (l *loader) boolean(key string, def bool) bool {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.errf("%s: %q is not true or false", key, v)
		return def
	}
	return b
}

func (l *loader) port(key string, def int) int {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errf("%s: %q is not a whole number", key, v)
		return def
	}
	if n < 1 || n > 65535 {
		l.errf("%s: must be a port between 1 and 65535, got %d", key, n)
		return def
	}
	return n
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errf("%s: %q is not a duration (try 15s, 2m, 1h)", key, v)
		return def
	}
	if d <= 0 {
		l.errf("%s: must be greater than zero, got %s", key, d)
		return def
	}
	return d
}

// durations reads a set of named durations written as comma-separated `name=duration` pairs.
//
// `insurance=720h,registration=2160h`. One variable rather than one per name, on
// [loader.signingKeys]' reasoning: the set of names is the domain's rather than this package's, and a
// scheme that needs a new variable to add a name is a scheme somebody works around.
//
// **An empty value is not the same as an absent one.** Absent returns the default, which for the
// only caller is nil. `VERIFICATION_EXPIRY_LEAD_TIMES=` — set and empty — returns an empty non-nil
// map, so a deployment can state "no lead times, deliberately" and have it read back as a decision
// rather than as a variable somebody forgot to set.
//
// The names are not validated here and cannot be: they belong to a domain this package may not
// import (SHIP-15c). What is validated is the shape — a pair, a name, and a duration that is not
// negative — because those are failures a startup message can name precisely and a domain's panic
// cannot.
//
// A **zero** duration is accepted and a negative one is not. Zero is a horizon of "now", which is
// the same as saying nothing and is a legitimate thing to write down explicitly. Negative is a
// horizon in the past, which would hide documents that have already lapsed — a queue quietly
// under-reporting is the failure that looks exactly like a quiet week.
func (l *loader) durations(key string, def map[string]time.Duration) map[string]time.Duration {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}

	out := map[string]time.Duration{}
	for _, pair := range strings.Split(v, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}

		name, raw, separated := strings.Cut(pair, "=")
		name = strings.TrimSpace(name)
		if !separated || name == "" {
			l.errf("%s: %q is not a name=duration pair", key, pair)
			continue
		}
		if _, repeated := out[name]; repeated {
			l.errf("%s: %q is named twice, and which one applies would depend on the order",
				key, name)
			continue
		}

		d, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			l.errf("%s: %q is not a duration (try 720h)", key, name)
			continue
		}
		if d < 0 {
			l.errf("%s: %s is %s; a negative value would hide what has already lapsed",
				key, name, d)
			continue
		}
		out[name] = d
	}
	return out
}

// goods reads the goods-category catalogue from one variable, as a JSON array (SHIP-58).
//
// # Why JSON, when every other list here is comma-separated
//
// Because every other list here is a list of scalars and this one is a list of records. `csv`
// and `durations` encode one and two fields respectively; a category has five, one of which is a
// free-text description that will contain commas the day somebody writes a good one. Inventing a
// third ad-hoc separator scheme to avoid a parser the standard library already has would be
// choosing a format that breaks on its own content.
//
// # Why a variable and not a file
//
// This package's own doc states the rule — "nothing is read from a configuration file at
// runtime" — and every setting in it follows that. The rule is worth more than the convenience:
// a file is a second thing a deployment has to ship, mount and keep in step with the image, and
// the failure it produces is a service that starts with a catalogue nobody intended. A variable
// is visible in one place beside every other setting.
//
// It is verbose to type, and deploy/.env.example shows the shape. That cost is paid by whoever
// overrides the list, which by design is rare: the default is the approved one until X-4 is
// answered.
//
// # Every refusal below returns the default rather than an empty catalogue
//
// A malformed override is a startup error — [Load] fails on l.errs and the service does not
// start, so the returned value is never actually served. Returning def rather than nil is
// nonetheless deliberate: it keeps this function total, so a caller reading the config in a test
// that ignores the error does not get a nil catalogue that publishes nothing and reports no
// reason. The same argument every other helper here makes when it returns def after errf.
func (l *loader) goods(key string, def []GoodsCategory) []GoodsCategory {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}

	var parsed []GoodsCategory
	if err := json.Unmarshal([]byte(v), &parsed); err != nil {
		l.errf("%s: not a JSON array of categories: %v", key, err)
		return def
	}
	if len(parsed) == 0 {
		l.errf("%s: the catalogue is empty; with no categories no job can be published", key)
		return def
	}

	seen := make(map[string]bool, len(parsed))
	carried := 0
	for i, c := range parsed {
		switch {
		case c.Code == "":
			l.errf("%s: entry %d has no code", key, i)
		case !lowerSnakeCase(c.Code):
			l.errf("%s: %q is not lower snake case; a code is stored on every job that "+
				"names it and is not the field that carries wording", key, c.Code)
		case seen[c.Code]:
			l.errf("%s: %q appears twice, and which one applies would depend on the order",
				key, c.Code)
		default:
			seen[c.Code] = true
		}

		if c.Label == "" {
			l.errf("%s: %q has no label; the label is what a client shows", key, c.Code)
		}
		if c.Carried {
			carried++
		}
	}

	// A catalogue of nothing but refusals is a marketplace that accepts no work at all. It
	// parses, it serves, and every publication fails with a policy refusal — which reads to
	// whoever is watching as a broken product rather than as a configuration mistake, so it is
	// refused at startup where the cause is still legible.
	if carried == 0 {
		l.errf("%s: no category is carried; every publication would be refused", key)
	}

	return parsed
}

// lowerSnakeCase reports whether s is lower-case letters, digits and single underscores.
//
// Hand-written rather than a regexp because this package imports none, and the rule is short
// enough that a pattern would be the less readable of the two. It matches what
// httpx.RegisterCode requires of an error code, for the same reason: these are identifiers that
// travel on the wire and get stored, and a mixed-case one is a bug report six months later.
func lowerSnakeCase(s string) bool {
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '_':
			if s[i-1] == '_' {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (l *loader) level(key string, def slog.Level) slog.Level {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	var lvl slog.Level
	// slog's own text unmarshaller expects upper case; accept any case for the humans.
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(v))); err != nil {
		l.errf("%s: %q is not a level (want debug, info, warn, or error)", key, v)
		return def
	}
	return lvl
}

func (l *loader) errf(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

// validate applies the cross-field rules that no individual parser can see.
func (l *loader) validate(cfg *Config) {
	if !cfg.Env.valid() {
		l.errf("SHIPPER_ENV: %q is not an environment (want development, staging, or production)", cfg.Env)
	}

	if cfg.Log.Format != "json" && cfg.Log.Format != "text" {
		l.errf("LOG_FORMAT: %q is not a format (want json or text)", cfg.Log.Format)
	}

	// Docs/09's SHIP-183b offers these as alternatives — "a configured trusted-proxy hop count
	// *or* CIDR allow-list" — and they answer the same question by different means. Composing
	// them needs a rule nobody has argued for, and the readings disagree about what it would be,
	// so a deployment setting both would get whichever this file happened to prefer. See
	// [TrustedProxy].
	if cfg.TrustedProxy.Hops > 0 && len(cfg.TrustedProxy.Networks) > 0 {
		l.errf("TRUSTED_PROXY_HOPS (%d) and TRUSTED_PROXY_NETWORKS (%d network(s)) cannot both "+
			"be set: they are two ways to answer the same question and there is no agreed rule "+
			"for combining them. Set the hop count where it is stable, the networks otherwise",
			cfg.TrustedProxy.Hops, len(cfg.TrustedProxy.Networks))
	}

	if cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		l.errf("DATABASE_MAX_IDLE_CONNS (%d) cannot exceed DATABASE_MAX_OPEN_CONNS (%d)",
			cfg.Database.MaxIdleConns, cfg.Database.MaxOpenConns)
	}

	// A claim that outlived the replay window would refuse a client's retries for longer
	// than it could ever answer them, which is the worst of both.
	if cfg.Idempotency.InFlightTTL > cfg.Idempotency.TTL {
		l.errf("IDEMPOTENCY_IN_FLIGHT_TTL (%s) cannot exceed IDEMPOTENCY_TTL (%s)",
			cfg.Idempotency.InFlightTTL, cfg.Idempotency.TTL)
	}

	// argon2 divides its memory between the lanes and rounds down, so a profile with more
	// lanes than kibibytes to give them is not a slow hash but an undefined one.
	if cfg.Passwords.Argon2.MemoryKiB < 8*uint32(cfg.Passwords.Argon2.Parallelism) {
		l.errf("PASSWORDS_ARGON2_MEMORY_KIB (%d) leaves less than 8 KiB for each of "+
			"PASSWORDS_ARGON2_PARALLELISM (%d) lanes",
			cfg.Passwords.Argon2.MemoryKiB, cfg.Passwords.Argon2.Parallelism)
	}

	// A signature nobody can produce is not a token anybody can use, and this is the only
	// place it can be caught: the failure is otherwise the first sign-in after a deploy.
	if _, ok := cfg.Identity.AccessTokenKeys[cfg.Identity.AccessTokenActiveKID]; !ok {
		l.errf("IDENTITY_ACCESS_TOKEN_ACTIVE_KID: %q names no key in IDENTITY_ACCESS_TOKEN_KEYS",
			cfg.Identity.AccessTokenActiveKID)
	}

	// An access token is not checked against the database, so nothing can shorten one that has
	// already been issued: a long TTL quietly turns "revoke this device" into "revoke this
	// device, eventually". The cap is generous rather than exact — Docs/10 §5 says fifteen
	// minutes, and this only refuses the values that would defeat the design.
	if cfg.Identity.AccessTokenTTL > time.Hour {
		l.errf("IDENTITY_ACCESS_TOKEN_TTL (%s) is longer than an hour; an issued access token "+
			"cannot be revoked before it expires", cfg.Identity.AccessTokenTTL)
	}

	// The same rule as the access token's active identifier, for the same reason: a signature
	// nobody can produce is not a token anybody can use, and the failure is otherwise the first
	// assignment after a deploy — which is a provider being told the platform is broken.
	if _, ok := cfg.Delivery.DriverTokenKeys[cfg.Delivery.DriverTokenActiveKID]; !ok {
		l.errf("DELIVERY_DRIVER_TOKEN_ACTIVE_KID: %q names no key in DELIVERY_DRIVER_TOKEN_KEYS",
			cfg.Delivery.DriverTokenActiveKID)
	}

	// **The two token systems may not share a secret, in any environment.**
	//
	// Docs/10 §5 requires separate signing key material, and CLAUDE.md makes "neither can be
	// exchanged for the other" an invariant. The audience check enforces it either way — both
	// verifiers pin their own and a test proves each refuses the other's tokens with the keys
	// deliberately shared — so this is the second lock rather than the first. It is worth having
	// because it is the one failure that would leave the invariant resting on a single check
	// while looking entirely correct in every log line and every response.
	//
	// Checked in development too, unlike the rules below it: this is not a deployment-hardening
	// rule but a structural one, and a developer who set the two variables to the same value
	// should be told immediately rather than at their first staging deploy.
	for driverKID, driverSecret := range cfg.Delivery.DriverTokenKeys {
		for accessKID, accessSecret := range cfg.Identity.AccessTokenKeys {
			if bytes.Equal(driverSecret, accessSecret) {
				l.errf("DELIVERY_DRIVER_TOKEN_KEYS: key %q is the same secret as "+
					"IDENTITY_ACCESS_TOKEN_KEYS key %q; the driver token and the mobile "+
					"session token must have separate signing key material (Docs/10 §5)",
					driverKID, accessKID)
			}
		}
	}

	// A driver token cannot be revoked before it expires either — there is nothing to check it
	// against until SHIP-109 — so an over-long TTL is a standing grant to one job's delivery
	// detail sitting in whatever message thread the link was forwarded through. The cap is
	// generous rather than exact: Docs/10 §5 and [Delivery.DriverTokenTTL] settle on seven days,
	// and this refuses only the values that would defeat the design.
	if cfg.Delivery.DriverTokenTTL > maxDriverTokenTTL {
		l.errf("DELIVERY_DRIVER_TOKEN_TTL (%s) is longer than %s; an issued driver token cannot "+
			"be revoked before it expires, so a link forwarded to a driver would outlive the job",
			cfg.Delivery.DriverTokenTTL, maxDriverTokenTTL)
	}

	// A default larger than the ceiling would mean a request that named no ?limit= got a
	// bigger page than one that asked for the maximum, which is the sort of inversion that is
	// obvious in a sentence and invisible in two environment variables.
	if cfg.Pagination.DefaultPageSize > cfg.Pagination.MaxPageSize {
		l.errf("PAGINATION_DEFAULT_PAGE_SIZE (%d) cannot exceed PAGINATION_MAX_PAGE_SIZE (%d)",
			cfg.Pagination.DefaultPageSize, cfg.Pagination.MaxPageSize)
	}

	// An endpoint that is not an absolute http(s) URL cannot be signed against, and the SDK's
	// own complaint about it arrives at the first upload rather than at startup.
	if endpoint, err := url.Parse(cfg.Storage.Endpoint); err != nil {
		l.errf("STORAGE_ENDPOINT: %q is not a URL", cfg.Storage.Endpoint)
	} else if endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		l.errf("STORAGE_ENDPOINT: %q is not an absolute http or https URL "+
			"(AWS S3's own is https://s3.%s.amazonaws.com)", cfg.Storage.Endpoint, cfg.Storage.Region)
	}

	// A bucket name S3 will not accept is a startup problem rather than an upload problem, and
	// the difference matters here more than it usually would: the name is per-worktree in
	// development (CLAUDE.md), so it is a value a person types, and the first thing that would
	// otherwise notice is a driver's proof upload.
	//
	// The subset checked is the one the full rule is made of — length, character set, and both
	// ends alphanumeric. AWS also refuses dotted names that parse as an IP address and names
	// with consecutive dots, which are not worth encoding here: neither is a mistake anybody
	// makes by hand, and the store refuses them anyway.
	if err := checkBucketName(cfg.Storage.Bucket); err != nil {
		l.errf("STORAGE_BUCKET: %q %s", cfg.Storage.Bucket, err)
	}

	// A media type that is not `type/subtype` cannot match anything an upload declares, so the
	// list would silently accept nothing. Checked lower case because HTTP media types are
	// case-insensitive and a comparison somewhere downstream will not be.
	for _, mediaType := range cfg.Storage.AcceptedContentTypes {
		kind, sub, separated := strings.Cut(mediaType, "/")
		if !separated || kind == "" || sub == "" {
			l.errf("STORAGE_ACCEPTED_CONTENT_TYPES: %q is not a type/subtype media type", mediaType)
			continue
		}
		if mediaType != strings.ToLower(mediaType) {
			l.errf("STORAGE_ACCEPTED_CONTENT_TYPES: %q must be lower case", mediaType)
		}
	}

	// See [maxPresignTTL]: a pre-signed URL is the whole of the authorisation and nothing
	// revokes one, so an over-long window is a standing grant to a photograph of somebody's
	// front door for whoever the link reaches.
	//
	// Both directions are bounded, and the download is checked separately rather than by taking
	// the larger of the two: they are independent settings and a deployment that raised only the
	// read window would otherwise pass unnoticed (SHIP-15r).
	if cfg.Storage.PresignTTL > maxPresignTTL {
		l.errf("STORAGE_PRESIGN_TTL (%s) is longer than %s; a pre-signed URL cannot be revoked "+
			"once it is signed, so the window is the whole of the exposure",
			cfg.Storage.PresignTTL, maxPresignTTL)
	}
	if cfg.Storage.DownloadTTL > maxPresignTTL {
		l.errf("STORAGE_DOWNLOAD_TTL (%s) is longer than %s; a pre-signed URL cannot be revoked "+
			"once it is signed, and a read link is a live link to a photograph",
			cfg.Storage.DownloadTTL, maxPresignTTL)
	}

	// Zero is refused for both, and it is a real mistake rather than a hypothetical one: an
	// unparseable duration is reported by [loader.duration] and a *deliberate* `0s` is not, so
	// without this the first symptom is a startup panic from delivery's handler — a configuration
	// fault reported as a wiring one, at the point furthest from the line that caused it.
	if cfg.Storage.PresignTTL <= 0 {
		l.errf("STORAGE_PRESIGN_TTL (%s) must be positive; nothing could be uploaded",
			cfg.Storage.PresignTTL)
	}
	if cfg.Storage.DownloadTTL <= 0 {
		l.errf("STORAGE_DOWNLOAD_TTL (%s) must be positive; no proof could be read back",
			cfg.Storage.DownloadTTL)
	}

	// Zero or negative would prompt a provider about every update the instant it was queued,
	// which is the escalation ladder's first rung wearing the second one's card (Docs/02 §3.1).
	// Refused here for the reason the presign TTLs are: a deliberate `0s` parses, so nothing
	// else would notice.
	if cfg.App.UnsyncedNudgeAfter <= 0 {
		l.errf("UNSYNCED_NUDGE_AFTER (%s) must be positive; a provider would be prompted about "+
			"every update the moment they recorded it", cfg.App.UnsyncedNudgeAfter)
	}

	// The budget is what a client aims at and the bound is what the platform will sign for, so a
	// budget above the bound tells every handset to compress towards a size guaranteed to be
	// refused. The symptom is a driver who cannot finish a delivery and a client-side number
	// nobody would think to look at — the platform's own limit is the one that appears in the
	// refusal.
	if int64(cfg.App.ProofCompressionBudgetBytes) > cfg.Storage.MaxUploadBytes {
		l.errf("PROOF_COMPRESSION_BUDGET_BYTES (%d) cannot exceed STORAGE_MAX_UPLOAD_BYTES (%d); "+
			"the budget is what the client compresses towards and the bound is what the platform "+
			"will accept", cfg.App.ProofCompressionBudgetBytes, cfg.Storage.MaxUploadBytes)
	}

	// An unrecognised transport is refused rather than defaulted, and that direction is
	// deliberate: the defaulting above happens only for a variable nobody set, whereas a
	// value that is present and wrong is somebody's intention spelled incorrectly. Falling
	// back would send real email from a deployment that had asked for the console, or
	// swallow it in one that had asked for a provider — and both are silent.
	switch cfg.Email.Transport {
	case TransportConsole, TransportHTTP, TransportSMTP:
	case TransportStub:
		l.errf("EMAIL_TRANSPORT: stub is a geocoding transport; " +
			"want console to log the message and send nothing")
	default:
		l.errf("EMAIL_TRANSPORT: %q is not a transport (want console, http, or smtp)",
			cfg.Email.Transport)
	}

	switch cfg.SMS.Transport {
	case TransportConsole, TransportHTTP:
	case TransportSMTP:
		l.errf("SMS_TRANSPORT: smtp is an email transport; want console or http")
	case TransportStub:
		l.errf("SMS_TRANSPORT: stub is a geocoding transport; " +
			"want console to log the message and send nothing")
	default:
		l.errf("SMS_TRANSPORT: %q is not a transport (want console or http)", cfg.SMS.Transport)
	}

	// The geocoding transport, on the same terms and with one value in common with neither of
	// the two above: an address is either resolved in-process or fetched, and there is no
	// third thing to do with one.
	switch cfg.Geocoding.Transport {
	case TransportStub:
	case TransportHTTP:
		// Under the stub this is unset and correct. Under http it is where the request
		// goes, and without it cmd/api built nil and logged a warning — which is a
		// deployment silently storing every address unresolved, reported once at boot.
		if cfg.Geocoding.ProviderBaseURL == "" {
			l.errf("GEOCODING_BASE_URL: must be set when GEOCODING_TRANSPORT is http")
		}
	case TransportConsole, TransportSMTP:
		l.errf("GEOCODING_TRANSPORT: %q is a messaging transport; want stub or http",
			cfg.Geocoding.Transport)
	default:
		l.errf("GEOCODING_TRANSPORT: %q is not a transport (want stub or http)",
			cfg.Geocoding.Transport)
	}

	// The smtp transport needs a host, and the failure without this is a panic out of
	// cmd/api's composition root — a configuration fault reported as a wiring one, at the
	// point furthest from the line that caused it. The same argument the presign TTLs make.
	if cfg.Email.Transport == TransportSMTP && cfg.Email.SMTP.Host == "" {
		l.errf("EMAIL_SMTP_HOST: must be set when EMAIL_TRANSPORT is smtp")
	}

	// A credential with nowhere to go is the shape of a half-finished configuration, and the
	// symptom is silence: nothing presents the key, addresses are resolved without it or not
	// at all, and the key sitting in the environment suggests the opposite. The reverse — a
	// base URL with no key — is legitimate and not checked, because a self-hosted geocoder
	// needs no credential.
	if cfg.Geocoding.ProviderAPIKey != "" && cfg.Geocoding.ProviderBaseURL == "" {
		l.errf("GEOCODING_API_KEY is set but GEOCODING_BASE_URL is not; " +
			"there is nowhere to present the credential")
	}

	// Everything below this point is a deployment-safety rule. Development is exempt by
	// design: the whole point of the defaults is that a fresh clone runs without setup.
	if cfg.Env.IsDevelopment() || !cfg.Env.valid() {
		return
	}

	for _, key := range credentialBearingDefaults {
		if l.defaulted[key] {
			l.errf("%s: must be set explicitly when SHIPPER_ENV is %s — "+
				"its default embeds a local development credential", key, cfg.Env)
		}
	}

	// The geocoding transport must be a decision somebody recorded, not one inherited
	// (SHIP-192).
	//
	// Unset resolves to the stub in every environment, which is the safe default for the
	// money and the wrong one for the data: a production deployment that forgot the variable
	// would answer every address with a stable, plausible, entirely fictional coordinate, and
	// look exactly like one that was working. Refusing the boot is the only report that
	// arrives before the jobs do.
	//
	// It is checked here rather than beside the transport switch above because it is a
	// deployment rule and not a parse rule — GEOCODING_TRANSPORT=stub in production is
	// permitted, and is the answer a demonstration instance gives.
	if l.defaulted["GEOCODING_TRANSPORT"] {
		l.errf("GEOCODING_TRANSPORT: must be set explicitly when SHIPPER_ENV is %s — "+
			"unset it is stub, which answers every address with a fictional coordinate",
			cfg.Env)
	}

	// Text logs outside development mean the log aggregator receives unparseable lines,
	// which is discovered during the first incident rather than before it (SHIP-174).
	if cfg.Log.Format == "text" {
		l.errf("LOG_FORMAT: must be json when SHIPPER_ENV is %s", cfg.Env)
	}

	if strings.Contains(cfg.Database.URL, "sslmode=disable") {
		l.errf("DATABASE_URL: sslmode=disable is not permitted when SHIPPER_ENV is %s", cfg.Env)
	}

	// The same shape for the object store, and it is needed here rather than covered by the
	// credential rule above: STORAGE_ENDPOINT carries no credential, so nothing else notices a
	// deployment left pointing at the development container. The symptom would be a proof
	// photograph the platform believes it stored and nobody can ever retrieve.
	if endpoint, err := url.Parse(cfg.Storage.Endpoint); err == nil {
		switch endpoint.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			l.errf("STORAGE_ENDPOINT: %q is a loopback address and cannot be right when "+
				"SHIPPER_ENV is %s (AWS S3's own is https://s3.%s.amazonaws.com)",
				cfg.Storage.Endpoint, cfg.Env, cfg.Storage.Region)
		}
	}

	// The credential-default rule above catches a variable that was never set. This catches
	// the other way in: the development key copied out of deploy/.env.example into a real
	// environment, which is a signing key published in this repository.
	//
	// Only when the variable was set explicitly — an unset one has already been refused above,
	// and reporting it twice would say the same thing in two ways.
	if !l.defaulted["IDENTITY_ACCESS_TOKEN_KEYS"] {
		for kid, secret := range cfg.Identity.AccessTokenKeys {
			if string(secret) == developmentSigningKey {
				l.errf("IDENTITY_ACCESS_TOKEN_KEYS: key %q is the development key from "+
					"deploy/.env.example, which is public; it may not be used when "+
					"SHIPPER_ENV is %s", kid, cfg.Env)
			}
		}
	}

	if !l.defaulted["DELIVERY_DRIVER_TOKEN_KEYS"] {
		for kid, secret := range cfg.Delivery.DriverTokenKeys {
			if string(secret) == developmentDriverSigningKey {
				l.errf("DELIVERY_DRIVER_TOKEN_KEYS: key %q is the development key from "+
					"deploy/.env.example, which is public; it may not be used when "+
					"SHIPPER_ENV is %s", kid, cfg.Env)
			}
		}
	}

	// The same second door, for the object store. The rule above catches a variable nobody set;
	// this catches the local throwaway copied out of deploy/.env.example into a real
	// environment, which would be a published credential against a bucket of private evidence.
	if !l.defaulted["STORAGE_SECRET_ACCESS_KEY"] &&
		cfg.Storage.SecretAccessKey == developmentStorageSecretAccessKey {
		l.errf("STORAGE_SECRET_ACCESS_KEY: this is the development credential from "+
			"deploy/.env.example, which is public; it may not be used when SHIPPER_ENV is %s",
			cfg.Env)
	}
}

// checkBucketName reports why a name is not one S3 will accept, or nil.
//
// The message completes the sentence "STORAGE_BUCKET: %q …", so it reads as a description of the
// name rather than as an instruction.
func checkBucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("is %d characters; a bucket name is between 3 and 63", len(name))
	}

	alphanumeric := func(c byte) bool {
		return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
	}
	if !alphanumeric(name[0]) || !alphanumeric(name[len(name)-1]) {
		return errors.New("must start and end with a lower-case letter or a digit")
	}
	for i := 0; i < len(name); i++ {
		if !alphanumeric(name[i]) && name[i] != '-' && name[i] != '.' {
			return fmt.Errorf("contains %q; a bucket name holds only lower-case letters, "+
				"digits, hyphens and dots", name[i])
		}
	}
	return nil
}
