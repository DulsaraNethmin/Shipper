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
	"errors"
	"fmt"
	"log/slog"
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
	Env         Environment
	HTTP        HTTP
	Log         Log
	Database    Database
	Redis       Redis
	Kafka       Kafka
	Idempotency Idempotency
	Identity    Identity
	Delivery    Delivery
	Email       Email
	SMS         SMS
	Geocoding   Geocoding
	Pagination  Pagination
	Storage     Storage
	App         App
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
// # Why a missing base URL is not an error
//
// Empty means no provider is configured, and cmd/api then builds the console stub in development
// and nil outside it. Nil is a supported state, not a degraded one: jobs.NewService documents it
// as "addresses are stored unresolved", and every path copes because Docs/01 §4.4's SHIP-59a
// requires a failed lookup not to fail the job.
//
// Falling back to the stub in staging was considered and rejected. The stub returns stable,
// plausible, entirely fictional coordinates, and a staging environment quietly full of those is
// worse than one with no coordinates at all — the first looks like it works.
// The field names match [Email] and [SMS] deliberately: all three are the same shape of
// adapter — a base URL, a credential, and an implementation chosen from Env — and naming the
// third one differently would invite the reader to look for a difference that is not there.
type Geocoding struct {
	// ProviderBaseURL is the provider's HTTP endpoint. Empty disables lookup entirely.
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
	// ProviderBaseURL is the root of the vendor's API. Empty in development, where nothing
	// reaches it.
	ProviderBaseURL string

	// ProviderAPIKey is presented as a bearer credential and is never logged.
	ProviderAPIKey string

	// Sender is the From address every message is dispatched with. One address for the whole
	// service in the MVP; per-domain senders are a deliverability decision nobody has needed
	// to make yet.
	Sender string
}

// Identity configures credentials and sessions (SHIP-29, SHIP-37).
//
// Both halves are configuration rather than compiled-in constants because both are expected to
// change without a release: the argon2id cost is raised as hardware improves, and a signing key
// is rotated on a schedule or in a hurry. Docs/10 §5 is the specification.
type Identity struct {
	// Argon2 is the cost new password hashes are written at.
	//
	// Verification does not read it. The parameters travel with each hash in its PHC string,
	// so raising these values leaves every stored password verifiable and upgrades each one at
	// its owner's next sign-in, with no migration (Docs/10 §5).
	Argon2 Argon2

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
	// ProviderBaseURL is the root of the gateway's API. Empty in development.
	ProviderBaseURL string

	// ProviderAPIKey is presented as a bearer credential and is never logged.
	ProviderAPIKey string

	// Sender is what the message appears to come from — an alphanumeric sender ID or an
	// originating number, depending on what the gateway and the destination country permit.
	// Australia allows both; the choice is made with the vendor.
	Sender string
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
		Identity: Identity{
			// m=64 MiB, t=3, p=4 — Docs/10 §5. The bounds are the range the identity
			// package will run; a value outside them is refused here rather than at the
			// first sign-in.
			Argon2: Argon2{
				MemoryKiB:   uint32(l.boundedInt("IDENTITY_ARGON2_MEMORY_KIB", 64*1024, 1024, 1<<20)),
				Iterations:  uint32(l.boundedInt("IDENTITY_ARGON2_ITERATIONS", 3, 1, 64)),
				Parallelism: uint8(l.boundedInt("IDENTITY_ARGON2_PARALLELISM", 4, 1, 255)),
			},
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
			ProviderBaseURL: l.str("EMAIL_PROVIDER_BASE_URL", ""),
			ProviderAPIKey:  l.str("EMAIL_PROVIDER_API_KEY", ""),
			Sender:          l.str("EMAIL_SENDER", "no-reply@shipper.com.au"),
		},
		SMS: SMS{
			ProviderBaseURL: l.str("SMS_PROVIDER_BASE_URL", ""),
			ProviderAPIKey:  l.str("SMS_PROVIDER_API_KEY", ""),
			Sender:          l.str("SMS_SENDER", "Shipper"),
		},
		Geocoding: Geocoding{
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
		App: App{
			MinimumIOSBuild:     l.positiveInt("MIN_SUPPORTED_IOS_BUILD", 1),
			MinimumAndroidBuild: l.positiveInt("MIN_SUPPORTED_ANDROID_BUILD", 1),
			IOSStoreURL:         l.str("IOS_STORE_URL", ""),
			AndroidStoreURL:     l.str("ANDROID_STORE_URL", ""),
		},
	}

	l.validate(cfg)

	if err := errors.Join(l.errs...); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
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
			c.Identity.Argon2.MemoryKiB, c.Identity.Argon2.Iterations, c.Identity.Argon2.Parallelism)),
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
		slog.Bool("email_provider_configured", c.Email.ProviderBaseURL != ""),
		slog.String("email_sender", c.Email.Sender),
		slog.Bool("sms_provider_configured", c.SMS.ProviderBaseURL != ""),
		slog.String("sms_sender", c.SMS.Sender),
		// The bucket and the endpoint, never the credential. The bucket is the one worth
		// insisting on: it is per-worktree in development, and a process writing into the
		// wrong one succeeds at everything and puts the objects where nobody looks.
		slog.String("storage_endpoint", redactURL(c.Storage.Endpoint)),
		slog.String("storage_bucket", c.Storage.Bucket),
		slog.String("storage_region", c.Storage.Region),
		slog.Bool("storage_path_style", c.Storage.UsePathStyle),
		slog.Duration("storage_presign_ttl", c.Storage.PresignTTL),
		slog.Duration("storage_download_ttl", c.Storage.DownloadTTL),
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
	if cfg.Identity.Argon2.MemoryKiB < 8*uint32(cfg.Identity.Argon2.Parallelism) {
		l.errf("IDENTITY_ARGON2_MEMORY_KIB (%d) leaves less than 8 KiB for each of "+
			"IDENTITY_ARGON2_PARALLELISM (%d) lanes",
			cfg.Identity.Argon2.MemoryKiB, cfg.Identity.Argon2.Parallelism)
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

	// A credential with nowhere to go is the shape of a half-finished configuration, and the
	// symptom is silence: cmd/api builds no geocoder, addresses are stored unresolved, and the
	// key sitting in the environment suggests the opposite. The reverse — a base URL with no
	// key — is legitimate and not checked, because a self-hosted geocoder needs no credential.
	if cfg.Geocoding.ProviderAPIKey != "" && cfg.Geocoding.ProviderBaseURL == "" {
		l.errf("GEOCODING_API_KEY is set but GEOCODING_BASE_URL is not; " +
			"no geocoder is built, so addresses would be stored unresolved")
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
