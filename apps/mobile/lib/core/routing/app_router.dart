import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/health/health_screen.dart';
import 'package:shipper/core/routing/signed_in_shell.dart';
import 'package:shipper/core/routing/starting_screen.dart';
import 'package:shipper/features/bidding/compare_offers_screen.dart';
import 'package:shipper/features/bidding/my_bids_screen.dart';
import 'package:shipper/features/bidding/negotiation_screen.dart';
import 'package:shipper/features/bidding/place_bid_panel.dart';
import 'package:shipper/features/delivery/delivery_screen.dart';
import 'package:shipper/features/delivery/proof_capture_screen.dart';
import 'package:shipper/features/delivery/tracking_screen.dart';
import 'package:shipper/features/fleet/add_vehicle_screen.dart';
import 'package:shipper/features/fleet/fleet_screen.dart';
import 'package:shipper/features/fleet/vehicle_screen.dart';
import 'package:shipper/features/identity/account_deletion_screen.dart';
import 'package:shipper/features/identity/email_verification_screen.dart';
import 'package:shipper/features/identity/phone_verification_screen.dart';
import 'package:shipper/features/identity/registration_complete_screen.dart';
import 'package:shipper/features/identity/registration_screen.dart';
import 'package:shipper/features/identity/role_selection_screen.dart';
import 'package:shipper/features/identity/sign_in_screen.dart';
import 'package:shipper/features/jobs/job_detail_screen.dart';
import 'package:shipper/features/jobs/job_goods_screen.dart';
import 'package:shipper/features/jobs/job_locations_screen.dart';
import 'package:shipper/features/jobs/open_job_screen.dart';
import 'package:shipper/features/profile/capture_document_screen.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_documents_screen.dart';

/// Route paths, named once.
///
/// String literals scattered through widgets are how a deep link ends up pointing at a route
/// that was renamed six weeks ago. `Docs/07` §5 requires every notification to deep-link to
/// the job, bid or dispute it concerns, so these paths are a contract with the server's
/// notification payloads and not merely internal navigation.
abstract final class Routes {
  /// Where a cold start lands while the keychain is being read.
  static const starting = '/';

  /// The signed-out shell, which **is** the sign-in screen from SHIP-55. Registration hangs off
  /// it (SHIP-51).
  ///
  /// It takes an optional `?email=` so the end of the signup journey can arrive with the address
  /// it just registered. Same mechanism as [verifyEmail]'s token, and for the same reason: a
  /// query parameter survives a redirect through the guard, where a constructor argument does
  /// not.
  static const signIn = '/sign-in';

  /// The first step of signup: which half of the marketplace this account is (SHIP-52).
  static const chooseRole = '/register/role';

  /// The registration form (SHIP-51).
  static const register = '/register';

  /// Confirm the email address (SHIP-53).
  ///
  /// **Also the app's first deep link, and the scheme is `shipper:///verify-email?token=…`.**
  /// `internal/identity/verification.go` left the choice to this ticket and sends a bare value
  /// meanwhile. A custom scheme needs no registered domain and no store account, both of which
  /// are blocked on X-2 and X-3; the production form is an HTTPS universal and app link, which
  /// needs the entitlement work in SHIP-24…27 and resolves to this same route with this same
  /// query parameter.
  static const verifyEmail = '/verify-email';

  /// Confirm the mobile number (SHIP-54).
  ///
  /// No deep link, and that is deliberate rather than unfinished: the code goes to the handset
  /// as six digits by SMS, and a link in a text message is a phishing pattern rather than a
  /// convenience. iOS and Android both offer the code to the keyboard from the message, which is
  /// what `AutofillHints.oneTimeCode` is for.
  static const verifyPhone = '/verify-phone';

  /// Where the signup journey ends (SHIP-51).
  ///
  /// It hands over to [signIn] carrying the address just registered. The account is deliberately
  /// **not** signed in automatically: `POST /v1/auth/register` returns no token, and the only way
  /// to a session is a password this app does not keep after the form that took it.
  static const registered = '/register/done';

  /// The signed-in shell. Role-aware from SHIP-52.
  static const home = '/home';

  /// The first step of publishing a delivery: the two addresses (SHIP-71).
  ///
  /// `/jobs/new` rather than `/jobs/{id}/locations`, because the draft does not exist until this
  /// step saves it — the id arrives in the response and not in the route. Resuming a draft that
  /// already exists is SHIP-75, and gets a route that names one.
  static const newJob = '/jobs/new';

  /// The remaining three steps of describing a delivery (SHIP-72, SHIP-73, SHIP-74).
  ///
  /// **Each carries the draft's id, unlike [newJob], and that is the whole of what makes a draft
  /// resumable** (SHIP-75). The first step has no id because the draft does not exist until it
  /// saves; every step after it is editing a job the platform already holds, so the id belongs in
  /// the path — which makes each step a location the customer's own job list can send them back
  /// to after they closed the app a week ago.
  ///
  /// Two segments, so none of them collides with [jobDetail], which matches exactly one.
  static const jobGoods = '/jobs/:id/goods';

  /// [jobGoods] for one draft.
  static String jobGoodsFor(String jobId) => '/jobs/$jobId/goods';


  /// One delivery in full, to the customer who owns it (SHIP-77).
  ///
  /// **The id is in the path rather than in a constructor argument**, and that is what makes the
  /// screen deep-linkable: `Docs/07` §5 requires every notification to open the exact job it
  /// concerns, so SHIP-145 delivers a payload to this path and nothing about the screen changes.
  ///
  /// Declared **after** [newJob] in the router, which is what keeps `/jobs/new` meaning the
  /// wizard: go_router takes the first route that matches, and `new` would otherwise be read as
  /// an identifier.
  static const jobDetail = '/jobs/:id';

  /// [jobDetail] for one job.
  static String jobDetailFor(String jobId) => '/jobs/$jobId';

  /// One open job as a **provider** sees it, and the offer they make on it (SHIP-100).
  ///
  /// A separate route from [jobDetail] rather than one screen that branches on the role, and that
  /// is the same decision the platform made twice: `GET /v1/jobs/{id}` and `GET /v1/fleet/jobs/{id}`
  /// are two endpoints answering with two response shapes, because one shape carrying the budget
  /// "when the caller owns it" is the arrangement `Docs/01` §4.3 is hardest to keep. Two routes,
  /// two screens, two types, and no flag anywhere that decides which half of the marketplace is
  /// looking.
  ///
  /// **This in-app path deliberately did not move when SHIP-83a moved the endpoint.** A location is
  /// not an endpoint. The platform's path had to change because `open` sitting in the `{id}`
  /// position made `GET /v1/jobs/{id}/<literal>` unregisterable service-wide; nothing of the sort
  /// applies here, where the two patterns differ in segment count and cannot overlap at all. What
  /// this path *is* is a deep-link target (`Docs/07` §5), so moving it would strand links already
  /// sent, for no gain.
  ///
  /// It does not collide with [jobDetail]: `/jobs/:id` matches exactly one segment, and this has
  /// two. `/jobs/open` on its own would be read as a job whose id is the word "open", and there is
  /// deliberately no such route — the feed is the provider half of [home] rather than a location.
  static const openJobDetail = '/jobs/open/:id';

  /// [openJobDetail] for one job.
  static String openJobDetailFor(String jobId) => '/jobs/open/$jobId';

  /// The offers on one of the customer's own jobs, compared side by side (SHIP-102).
  ///
  /// **`/offers` rather than `/bids`, and the client's word differs from the platform's on purpose.**
  /// The endpoint behind it is `GET /v1/jobs/{id}/bids/received`, which is itself five segments
  /// because `GET /v1/jobs/{id}/bids` could not be registered beside the feed's old
  /// `GET /v1/jobs/open/{id}` — SHIP-83a has since freed that space and the published endpoint stays
  /// where it is. A location is not an endpoint, so this one takes the word a customer would use:
  /// they are reading the offers they have received, not browsing a collection called `bids`.
  ///
  /// It does not collide with [jobDetail], which matches exactly one segment; it sits **after**
  /// `/jobs/open/:id` in the router for the same reason every two-segment job route does.
  static const jobOffers = '/jobs/:id/offers';

  /// [jobOffers] for one job.
  static String jobOffersFor(String jobId) => '/jobs/$jobId/offers';

  /// One negotiation on one job, as either party sees it (SHIP-103).
  ///
  /// **Two identifiers in the path, which is the first route in this app to take a second**, and
  /// both are load-bearing. A job can hold offers from several providers at once and they are not in
  /// one room: each pair — this customer and one provider — has its own chain and its own
  /// conversation, and no provider can see another's. So a job identifier alone does not name a
  /// negotiation, and a path that pretended it did would be one screen for several exchanges.
  ///
  /// **The bid identifier is an address rather than a subject.** The platform resolves the whole
  /// chain from *any* offer in it — "the one you are holding will do, however old" — so this stays
  /// valid as counters supersede one another and never has to be rewritten by the screen holding it.
  /// That is what makes it safe as a deep-link target (`Docs/07` §5): a notification sent when the
  /// first offer was placed still opens the right conversation four counters later.
  ///
  /// **One route for both parties, deliberately, where `jobDetail` and `openJobDetail` are two.**
  /// Those two are separate because the customer's job and the provider's view of it are different
  /// *responses* — one carries the budget and the other must never be able to. A negotiation is the
  /// opposite case: four endpoints, each serving both sides, each working out which from the
  /// credential. Two routes here would be two renderings of one exchange, and the first thing they
  /// would disagree about is whose turn it is.
  ///
  /// It collides with nothing. `/jobs/:id` matches one segment; `/jobs/:id/delivery/proof` has the
  /// same shape but a literal third segment that differs.
  static const negotiation = '/jobs/:id/negotiation/:bidId';

  /// [negotiation] for one offer in one job's chain.
  static String negotiationFor(String jobId, String bidId) =>
      '/jobs/$jobId/negotiation/$bidId';

  /// Recording the milestones of one delivery, as the awarded **provider** (SHIP-129).
  ///
  /// A third route under `/jobs/` rather than a tab on either of the two above, and for the reason
  /// that made those two separate: they are three readers of three different things. [jobDetail] is
  /// the customer's own job, [openJobDetail] is a job a provider may bid on — and stops answering
  /// the moment they win it — and this is the delivery of a job already awarded.
  ///
  /// **It is reached by deep link today and by nothing else, which is recorded rather than hidden.**
  /// `Docs/07` §5 makes that a first-class way in: SHIP-145 tells an awarded provider they have won
  /// by push, and the payload opens the job it concerns. What does not exist is a *list* — the
  /// provider half of the shell shows open work to bid on (SHIP-99), and no endpoint serves a
  /// provider the jobs they have been awarded. `Docs/11` §3 names what would close it.
  ///
  /// It does not collide with [jobDetail], which matches exactly one segment.
  static const delivery = '/jobs/:id/delivery';

  /// [delivery] for one job.
  static String deliveryFor(String jobId) => '/jobs/$jobId/delivery';

  /// How one delivery is going, as the **customer** who owns it (SHIP-133).
  ///
  /// A fourth route under `/jobs/` and the second reader of the same delivery, which is the
  /// arrangement the three before it established: [jobDetail] is the customer's own job,
  /// [openJobDetail] is a job a provider may bid on, [delivery] is the provider *recording* a
  /// delivery, and this is the customer *watching* one.
  ///
  /// **Not a tab on [jobDetail], and not a section of it.** The two read different endpoints with
  /// different failure modes — `GET /v1/jobs/{id}` is owner-only and this shelf admits both parties
  /// — and a customer refreshing a photograph should not be re-reading their whole job to do it.
  /// Keeping them apart is also what lets the delivery read be added to a job screen that already
  /// works, rather than making that screen's first load wait on three more requests.
  ///
  /// It does not collide with [jobDetail], which matches exactly one segment, nor with [delivery],
  /// whose second segment is the literal `delivery`.
  static const tracking = '/jobs/:id/tracking';

  /// [tracking] for one job.
  static String trackingFor(String jobId) => '/jobs/$jobId/tracking';

  /// Photographing one delivery (SHIP-130).
  ///
  /// Under [delivery] rather than beside it, because it is a step of that screen's job and not a
  /// second way in: it is reached from the Delivered button and by nothing else. Three segments, so
  /// it collides with neither [jobDetail] nor [delivery].
  static const deliveryProof = '/jobs/:id/delivery/proof';

  /// [deliveryProof] for one job.
  static String deliveryProofFor(String jobId) => '/jobs/$jobId/delivery/proof';

  /// The provider's own bids, across every job (SHIP-101).
  ///
  /// `/bids` rather than `/fleet/bids`, and the divergence from the endpoint's own path is
  /// deliberate. `GET /v1/fleet/bids` sits under `/v1/fleet` because that is where a provider's own
  /// **records** live on the platform — their vehicles, their service area, their profile — and
  /// because a four-segment `GET /v1/jobs/{id}/<literal>` panicked Go's `ServeMux` while
  /// `GET /v1/jobs/open/{id}` existed (SHIP-83a has since moved that feed to `/v1/fleet/jobs/{id}`,
  /// which is where a provider's records were always going). Neither reason is a fact about this
  /// app's navigation: [fleet]
  /// here means the vehicles screen, so `/fleet/bids` would read as a third thing under the
  /// vehicles, which is what it is not.
  ///
  /// A URL is a person's map of the product. This is a top-level place a provider goes.
  static const myBids = '/bids';

  /// The provider's own fleet (SHIP-98).
  ///
  /// `/fleet/vehicles` rather than `/fleet`, because the fleet is not the only thing that domain
  /// holds: SHIP-79's service area and specialties are a provider *profile*, served from
  /// `/v1/fleet/profile`. Naming the collection now is what stops that arriving as a second meaning
  /// for one path.
  static const fleet = '/fleet/vehicles';

  /// Adding a vehicle (SHIP-98).
  ///
  /// Declared **before** [vehicleDetail] in the router, for the reason [newJob] is declared before
  /// [jobDetail]: go_router takes the first route that matches, and `new` would otherwise be read
  /// as a vehicle's identifier.
  static const newVehicle = '/fleet/vehicles/new';

  /// One vehicle, to the provider who owns it (SHIP-98).
  ///
  /// The id is in the path rather than in a constructor argument, which is what makes the screen
  /// deep-linkable — the same decision, and the same reason, as [jobDetail].
  static const vehicleDetail = '/fleet/vehicles/:id';

  /// [vehicleDetail] for one vehicle.
  static String vehicleDetailFor(String vehicleId) => '/fleet/vehicles/$vehicleId';

  /// Deleting this account (SHIP-173).
  ///
  /// `/account/deletion` mirrors the endpoint's own path, which is deliberate here where
  /// [myBids] deliberately does not: those diverge because a URL is a person's map of the product
  /// and `/v1/fleet/bids` names where a provider's *records* live on the platform. This is the
  /// account itself, and the two words mean the same thing on both sides.
  ///
  /// **Not under `/settings`, because there is none.** `features/profile` is a doc-only stub and
  /// this app has no settings screen; inventing one to hold a single action would be building the
  /// screen a later ticket has to reconcile with. It is reached from the signed-in shell's app
  /// bar, which is what makes deletion discoverable in-app — Apple's actual requirement.
  ///
  /// Two segments, and it collides with nothing: no other route begins `/account`.
  static const accountDeletion = '/account/deletion';

  /// The provider's verification documents (SHIP-81c).
  ///
  /// `/verification/documents` rather than `/profile/verification`, and the divergence from the
  /// feature's folder name is deliberate — a URL is a person's map of the product, and "profile" is
  /// not what a provider is doing here. It mirrors the endpoint's own path minus its `/provider`
  /// segment, which the app does not need because there is only one account signed in.
  ///
  /// Two segments, and it collides with nothing: no other route begins `/verification`.
  static const verificationDocuments = '/verification/documents';

  /// Photographing one of the four (SHIP-81c).
  ///
  /// Under [verificationDocuments] rather than beside it, because it is a step of that screen's job
  /// and not a second way in. The kind is in the path rather than in a constructor argument, which
  /// is what makes the screen deep-linkable in the same way [jobDetail] is — and it is the *wire*
  /// spelling, so a link naming `abn_evidence` is naming the contract rather than a label.
  static const captureDocument = '/verification/documents/:kind';

  /// [captureDocument] for one kind, named by its wire spelling.
  static String captureDocumentFor(String kind) => '/verification/documents/$kind';

  /// The connectivity check (SHIP-19).
  ///
  /// Reachable from **both** shells on purpose. It is the only screen that demonstrates build
  /// flavour, base URL, transport, decoding and failure mapping end to end on a real device,
  /// and putting it behind the session would have made SHIP-19 undemonstrable on a fresh
  /// install — which is precisely when somebody needs to know whether the app can reach the
  /// API at all.
  static const health = '/health';
}

/// Locations either shell may show. See [Routes.health].
const _sessionAgnostic = <String>{Routes.health};

/// Locations a signed-out user may be at (SHIP-51).
///
/// **Signing up happens entirely while signed out, and that is the platform's design rather
/// than an oversight.** `POST /v1/auth/register` returns an account and no token — registering
/// is not signing in — so every screen in the journey runs with no session at all. A guard that
/// sent a signed-out user to the sign-in shell from *every* location, which is what SHIP-49 did
/// while there was nothing else to reach, would bounce the user out of registration on the first
/// redirect.
const _signedOutLocations = <String>{
  Routes.signIn,
  Routes.chooseRole,
  Routes.register,
  Routes.verifyEmail,
  Routes.verifyPhone,
  Routes.registered,
};

/// Locations a signed-in user may be at (SHIP-71).
///
/// It exists for the same reason [_signedOutLocations] does. SHIP-49's guard sent a signed-in
/// user to the home shell from *every* other location, which was right while the shell was the
/// only thing to reach; the first screen that hangs off it would otherwise be redirected away
/// the instant it was opened, and the symptom — a button that appears to do nothing — points
/// nowhere near the guard.
///
/// **Adding a location here grants no permission.** What the account may actually do is decided
/// server-side on every request; this decides only where the app is willing to draw.
///
/// **It is deliberately blind to the role**, and that is worth saying because SHIP-98 added the
/// first surface only one half of the marketplace has any use for. Two reasons, and the second is
/// the one that would have produced a bug: a guard that decided who may be where would be an
/// authorisation control living on the device, which `Docs/07` §3 forbids; and the role is `null`
/// for the first round trip of a restored cold start (SHIP-50), so a role-aware redirect would
/// bounce a provider off their own fleet every time they opened the app from a notification. The
/// role decides what a screen *draws* — `ProviderOnly` — not where the router is willing to go.
const _signedInLocations = <String>{
  Routes.accountDeletion,
  Routes.home,
  Routes.newJob,
  Routes.myBids,
  Routes.fleet,
  Routes.newVehicle,
  Routes.verificationDocuments,
};

/// Locations a signed-in user may be at whose path carries an identifier (SHIP-77).
///
/// A second collection rather than a cleverer first one, because a set of fixed strings is the
/// readable form and most routes are one. This is for the routes that cannot be: `/jobs/{id}`
/// names a job, so there is no constant to put in the set above.
///
/// **It matches shape and nothing else, and that is deliberate.** It does not check that the id
/// is a UUID, or that the job exists, or that the caller owns it. `Docs/07` §3 puts all three on
/// the platform — `GET /v1/jobs/{id}` answers `404` for a stranger's job byte-identically to one
/// that does not exist — and a client-side pattern that looked authoritative is exactly how a
/// guard stops being navigation and starts being a control nobody audited.
final _signedInPatterns = <RegExp>[
  // `/jobs/new` is matched by the set above first, so the wizard is never read as a job id.
  RegExp(r'^/jobs/[^/]+$'),

  // The provider's view of one open job (SHIP-100). A second pattern rather than a widening of the
  // one above, because `[^/]+` is one segment on purpose: a pattern loose enough to cover both
  // would also admit every path under `/jobs/` that anybody adds later, and this collection decides
  // where the app is willing to *draw* — the looser it is, the less it says.
  RegExp(r'^/jobs/open/[^/]+$'),

  // The awarded provider recording one delivery's milestones (SHIP-129). A third pattern for the
  // same reason as the second: the segment after the id is fixed, so this admits exactly one more
  // location rather than everything under `/jobs/{id}/`. The route is deep-link only today, which
  // makes forgetting this line a link that silently lands on the home shell rather than a card that
  // does nothing.
  RegExp(r'^/jobs/[^/]+/delivery$'),

  // Photographing that delivery (SHIP-130). A fourth pattern for the third time and for the same
  // reason: the segments after the id are fixed, so this admits exactly one more location. It is
  // pushed from the delivery screen rather than deep-linked, which makes forgetting this line a
  // button that appears to do nothing.
  RegExp(r'^/jobs/[^/]+/delivery/proof$'),

  // The customer watching that same delivery (SHIP-133). A fifth pattern, same reasoning, and this
  // one is deep-linked as well as pushed: `Docs/07` §5 sends a milestone notification to the job it
  // concerns, so forgetting this line is a notification that lands on the home shell.
  RegExp(r'^/jobs/[^/]+/tracking$'),

  // The customer comparing the offers on their own job (SHIP-102). A sixth pattern, same
  // reasoning, and it is pushed from the job screen rather than deep-linked — which makes
  // forgetting this line a **button that appears to do nothing**, since the guard silently
  // redirects to the home shell rather than failing. `compare_offers_test.dart` reaches the screen
  // by tapping that button for exactly this reason, and it caught the omission on the first run.
  RegExp(r'^/jobs/[^/]+/offers$'),

  // One negotiation on one job (SHIP-103). A seventh pattern, same reasoning, and the **first with
  // two identifiers in it** — a job identifier alone does not name a negotiation, because a job can
  // hold one exchange per provider and no provider may see another's. It is pushed from both
  // parties' screens and is a deep-link target besides (`Docs/07` §5), so forgetting this line is a
  // button that appears to do nothing on one side and a notification that lands on the home shell
  // on the other. `negotiation_test.dart` reaches it by tapping, on both sides, for that reason.
  RegExp(r'^/jobs/[^/]+/negotiation/[^/]+$'),

  // The goods step of the job wizard (SHIP-72), and the first route to carry a draft id. Its own
  // line rather than an alternation shared with the steps that follow it: one line admits one
  // location, which is the reading every pattern above establishes, and an alternation is a line
  // whose count somebody has to work out. It is pushed from the locations step, so forgetting it
  // is a **button that appears to do nothing** — the guard redirects to the home shell rather
  // than failing.
  RegExp(r'^/jobs/[^/]+/goods$'),

  // `/fleet/vehicles/new` likewise (SHIP-98). Forgetting this line is the failure run 1 named: a
  // route reachable only through an identifier looks, from the outside, like a card that does
  // nothing when it is tapped.
  RegExp(r'^/fleet/vehicles/[^/]+$'),

  // Photographing one verification document (SHIP-81c). Same reasoning again, and the segment after
  // `documents` is a *kind* rather than an identifier — which changes nothing about this line and is
  // worth saying, because `[^/]+` is deliberately not a list of the four: this collection decides
  // where the router is willing to draw, and `Docs/07` §3 puts every decision about what a value may
  // be on the platform. A `kind` outside the four is refused by `422` there and by
  // `VerificationDocumentKind.fromWire` here, which is a parse rather than an authorisation.
  RegExp(r'^/verification/documents/[^/]+$'),
];

/// Whether a signed-in user may be at [location].
bool _signedInMayBeAt(String location) {
  return _signedInLocations.contains(location) ||
      _signedInPatterns.any((pattern) => pattern.hasMatch(location));
}

/// Where the session says this location should be, or `null` to leave it alone.
///
/// A pure function of the session and the location, separated from [routerProvider] so it can
/// be read as a table and tested as one. Every routing bug in a guard of this shape is a cell
/// in that table nobody thought about, and the cells are only obvious when they are written
/// down together.
///
/// **This is navigation, not authorisation.** `Docs/07` §3 and `CLAUDE.md` both put every
/// authorisation decision on the platform: keeping a signed-out user off a shell that would be
/// empty anyway is a convenience, and reaching one by any means — a deep link, a stale route,
/// a modified build — still fails server-side on the first request it makes. Nothing here is
/// load-bearing for security, and nothing should be made load-bearing for it later.
@visibleForTesting
String? redirectFor(SessionState session, String location) {
  if (_sessionAgnostic.contains(location)) return null;

  return switch (session) {
    // Hold the splash until the keychain answers. Where a deep link that arrives during the
    // read is *remembered* is [Redirector] — see the note there.
    SessionRestoring() => location == Routes.starting ? null : Routes.starting,
    SessionSignedOut() =>
      _signedOutLocations.contains(location) ? null : Routes.signIn,
    SessionSignedIn() => _signedInMayBeAt(location) ? null : Routes.home,
  };
}

/// The redirect the router actually installs: [redirectFor], plus the one thing a pure function
/// of the session and the location cannot do (SHIP-53).
///
/// **SHIP-49 wrote down that a deep link arriving during the keychain read is dropped**, judged
/// it acceptable while nothing deep-linked, and named SHIP-143 — push payloads — as the ticket
/// that would have to remember the arriving location. SHIP-53 turned out to be the first ticket
/// that deep-links: `shipper:///verify-email?token=…` arrives at a cold start, which is exactly
/// when the session is restoring, so it is *always* the dropped case rather than a rare one.
/// The person taps the link in their email and lands on the sign-in screen.
///
/// So the location is held for the few milliseconds the keychain takes and reissued when the
/// answer arrives. Two properties are worth stating because they are what make this safe:
///
/// - **The held location is still put through [redirectFor].** Holding a location does not
///   exempt it from the guard — a link to the home shell arriving on a signed-out device still
///   goes to the sign-in screen.
/// - **It holds the whole URI, query and all.** Holding only the path would deliver somebody to
///   the verification screen with no token in it, which is worse than not delivering them.
///
/// It is a class rather than a closure so it can be tested as a sequence, which is what it is —
/// its whole behaviour is what the second call does about the first.
@visibleForTesting
class Redirector {
  String? _held;

  /// Where this redirect should go, or `null` to leave it alone.
  String? call(SessionState session, Uri uri) {
    final location = uri.path;

    if (session is SessionRestoring) {
      // Anything other than the splash during the restore is a location somebody asked for and
      // the app is not ready to show yet.
      if (location != Routes.starting) _held = uri.toString();
      return redirectFor(session, location);
    }

    final held = _held;
    if (held != null) {
      // Cleared unconditionally: a location held across one restore must not be reissued on
      // some later sign-out, which would take somebody back to a screen they had left.
      _held = null;
      if (location == Routes.starting) {
        return redirectFor(session, Uri.parse(held).path) ?? held;
      }
    }

    return redirectFor(session, location);
  }
}

/// The router, as a provider.
///
/// A provider rather than a top-level constant because navigation depends on session state:
/// `Docs/07` §1 requires customer and provider surfaces to stay genuinely separate, and
/// `Docs/07` §3 makes the session the thing that decides which the user is in.
///
/// It **reads** the session and listens for changes rather than watching it. Watching would
/// rebuild the provider, which constructs a new [GoRouter] and a new navigator, discarding the
/// history and every open route on each sign-in and sign-out. `refreshListenable` is go_router's
/// answer to exactly that: one router for the life of the app, re-evaluating its redirect when
/// told to.
final routerProvider = Provider<GoRouter>((ref) {
  final sessionChanged = ValueNotifier<SessionState>(ref.read(sessionProvider));
  ref.onDispose(sessionChanged.dispose);
  ref.listen(sessionProvider, (_, next) => sessionChanged.value = next);

  // One per router, and the router is one per application: the hold has to survive every
  // redirect of a single cold start and nothing longer.
  final redirector = Redirector();

  return GoRouter(
    // Only the starting location when the platform has not supplied one. go_router prefers the
    // platform's default route, which is how a cold-start deep link arrives.
    initialLocation: Routes.starting,
    refreshListenable: sessionChanged,
    redirect: (context, state) => redirector(ref.read(sessionProvider), state.uri),
    routes: <RouteBase>[
      GoRoute(
        path: Routes.starting,
        builder: (context, state) => const StartingScreen(),
      ),
      GoRoute(
        path: Routes.signIn,
        builder: (context, state) => SignInScreen(
          prefilledEmail: state.uri.queryParameters['email'],
        ),
      ),
      GoRoute(
        path: Routes.chooseRole,
        builder: (context, state) => const RoleSelectionScreen(),
      ),
      GoRoute(
        path: Routes.register,
        builder: (context, state) => const RegistrationScreen(),
      ),
      GoRoute(
        path: Routes.verifyEmail,
        // The token arrives in the query string, which is the form a deep link of either kind
        // resolves to — a custom scheme now, an HTTPS universal and app link once the domain
        // and the entitlements exist. `matchedLocation` excludes the query, so the guard above
        // sees `/verify-email` either way.
        builder: (context, state) => EmailVerificationScreen(
          deepLinkedToken: state.uri.queryParameters['token'],
        ),
      ),
      GoRoute(
        path: Routes.verifyPhone,
        builder: (context, state) => const PhoneVerificationScreen(),
      ),
      GoRoute(
        path: Routes.registered,
        builder: (context, state) => const RegistrationCompleteScreen(),
      ),
      GoRoute(
        path: Routes.home,
        builder: (context, state) => const SignedInShell(),
      ),
      GoRoute(
        path: Routes.newJob,
        builder: (context, state) => const JobLocationsScreen(),
      ),
      // The rest of the wizard, before `jobDetail` for the same reason `newJob` is: everything
      // more specific under `/jobs` is declared ahead of `/jobs/:id`. They do not actually collide
      // — these carry two segments and `/jobs/:id` matches one — and the order is kept so the
      // reading holds for whoever adds the next one.
      GoRoute(
        path: Routes.jobGoods,
        builder: (context, state) => JobGoodsScreen(jobId: state.pathParameters['id'] ?? ''),
      ),
      // Before `jobDetail`, in the same spirit as `newJob`. It does not actually collide —
      // `/jobs/:id` matches one segment and this has two — and it is declared first anyway, so that
      // the reading "everything more specific under /jobs comes before /jobs/:id" holds for the next
      // person to add one.
      //
      // **This is where the two features that make up SHIP-100 meet, and nowhere else.** `Docs/07`
      // §2 forbids `features/jobs` and `features/bidding` importing one another, so the job detail
      // declares that it needs a panel and the router — which is `core` — supplies the one from
      // `bidding`. The Go side calls this the composition root and puts it in `cmd/api`.
      GoRoute(
        path: Routes.openJobDetail,
        builder: (context, state) {
          final id = state.pathParameters['id'] ?? '';
          return OpenJobScreen(jobId: id, bidPanel: PlaceBidPanel(jobId: id));
        },
      ),
      // Before `jobDetail` as well, and it does not collide either — `/jobs/:id` is one segment
      // and this is two. Declared here so that "everything more specific under /jobs comes before
      // /jobs/:id" keeps holding for whoever adds the next one.
      // Before `delivery` as well: go_router takes the first route that matches, and three
      // segments declared after two is a path that never wins.
      GoRoute(
        path: Routes.deliveryProof,
        builder: (context, state) => ProofCaptureScreen(
          jobId: state.pathParameters['id'] ?? '',
        ),
      ),
      GoRoute(
        path: Routes.delivery,
        builder: (context, state) => DeliveryScreen(
          jobId: state.pathParameters['id'] ?? '',
        ),
      ),
      // Three segments under the job, like `delivery/proof`, and declared with them rather than
      // after `jobDetail` so that "everything more specific under /jobs comes before /jobs/:id"
      // keeps holding. It cannot collide with `deliveryProof`: that route's third segment is the
      // literal `delivery`, and this one's is the literal `negotiation`.
      GoRoute(
        path: Routes.negotiation,
        builder: (context, state) => NegotiationScreen(
          jobId: state.pathParameters['id'] ?? '',
          bidId: state.pathParameters['bidId'] ?? '',
        ),
      ),
      // Two segments, like `delivery` and `open/{id}`, and declared before `jobDetail` for the same
      // reason they are: "everything more specific under /jobs comes before /jobs/:id".
      GoRoute(
        path: Routes.tracking,
        builder: (context, state) => CustomerTrackingScreen(
          jobId: state.pathParameters['id'] ?? '',
        ),
      ),
      // After `newJob`, deliberately. go_router takes the first route that matches, so declaring
      // `/jobs/:id` first would make `/jobs/new` a job whose id is the word "new".
      GoRoute(
        path: Routes.jobDetail,
        builder: (context, state) => JobDetailScreen(
          jobId: state.pathParameters['id'] ?? '',
        ),
      ),
      GoRoute(
        path: Routes.jobOffers,
        builder: (context, state) => CompareOffersScreen(
          jobId: state.pathParameters['id'] ?? '',
        ),
      ),
      GoRoute(
        path: Routes.myBids,
        builder: (context, state) => const MyBidsScreen(),
      ),
      GoRoute(
        path: Routes.fleet,
        builder: (context, state) => const FleetScreen(),
      ),
      // Before `vehicleDetail`, deliberately, exactly as `newJob` precedes `jobDetail`: go_router
      // takes the first route that matches, so `/fleet/vehicles/:id` declared first would make
      // `/fleet/vehicles/new` a vehicle whose id is the word "new".
      GoRoute(
        path: Routes.newVehicle,
        builder: (context, state) => const AddVehicleScreen(),
      ),
      GoRoute(
        path: Routes.vehicleDetail,
        builder: (context, state) => VehicleScreen(
          vehicleId: state.pathParameters['id'] ?? '',
        ),
      ),
      // Declared **before** the list route for the reason `newVehicle` is declared before
      // `vehicleDetail`: go_router takes the first route that matches. These two cannot actually
      // collide — `/verification/documents` is two segments and this is three — but the pair is
      // written in the order the next one added would need.
      GoRoute(
        path: Routes.captureDocument,
        builder: (context, state) {
          final kind = VerificationDocumentKind.fromWire(state.pathParameters['kind'] ?? '');
          // A kind this build does not know is a link from a later build, or a typed URL. Sending
          // it to the list is the same answer `VerificationDocument.fromJson` gives for the same
          // input, and it is better than a screen captioned with an empty string.
          if (kind == null) return const VerificationDocumentsScreen();
          return CaptureDocumentScreen(kind: kind);
        },
      ),
      GoRoute(
        path: Routes.verificationDocuments,
        builder: (context, state) => const VerificationDocumentsScreen(),
      ),
      GoRoute(
        path: Routes.accountDeletion,
        builder: (context, state) => const AccountDeletionScreen(),
      ),
      GoRoute(
        path: Routes.health,
        builder: (context, state) => const HealthScreen(),
      ),
    ],
  );
});
