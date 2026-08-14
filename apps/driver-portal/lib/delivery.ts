/**
 * The one request this portal makes, and what the driver is told when the platform refuses it
 * (SHIP-120).
 *
 * # There is exactly one request, and that is a property of the build rather than a rule
 *
 * `deliveryPath` is the only URL builder in the application and [openDelivery] is the only function
 * that sends the token anywhere. The path is a template with one hole, the hole takes a value that
 * has already been recognised as a job identifier, and it is a **relative** path — so a token
 * cannot be sent to another origin by editing a string, and cannot be sent to another endpoint
 * without adding a second function that somebody would have to write on purpose.
 *
 * That relative path is served by this application's own route handler, which forwards to
 * `GET /v1/driver/jobs/{id}` and to nothing else. See `app/api/driver/jobs/[jobId]/route.ts` for
 * why the request goes through this origin rather than straight to the platform.
 *
 * # What a driver is told, and what the messages are careful not to say
 *
 * The platform gives four answers this portal has to render honestly, and the difference between
 * two of them is the whole reason SHIP-108 minted an error code of its own.
 *
 * - **The link has expired.** `401` with `delivery_driver_link_expired`. A driver has nothing to
 *   refresh and no account to sign back into, so the only useful instruction is "ask for a new
 *   one" — which is exactly why this is not `token_expired`, whose meaning is "refresh and retry"
 *   and which would put this portal in a loop.
 * - **The link is not one.** `401` with anything else — a broken signature, a truncated URL, a
 *   mobile session token, a link signed with the wrong key. The platform deliberately does not
 *   distinguish these and neither does this page.
 * - **The link no longer opens a delivery.** `404`. This is one answer to two questions and it
 *   must stay one: a valid link presented on a **different job**, and a link whose driver has been
 *   **stood down**. The platform gives `404` rather than `403` so that a link-holder cannot
 *   confirm that a competitor's job exists, and a page that split the two apart would hand back
 *   exactly what the status code was chosen to withhold.
 * - **Try again.** `429`, `503`, or no answer at all because the phone has no signal. The last is
 *   the most likely thing that will ever happen to this page.
 *
 * None of the copy below says whether a job exists, whose it is, or whether anything is on it. The
 * two that mention the link say something about the **credential** the driver is holding, which is
 * the one thing the platform does tell them apart on and the one thing they can act on.
 */

/**
 * What `GET /v1/driver/jobs/{id}` serves, which is the platform's answer to what a driver may see.
 *
 * Five fields, held to a closed set by a Go test and by a `make verify` check (SHIP-108). This
 * interface is a copy of that set and nothing more: the portal shows what the endpoint serves, and
 * a field this shape does not carry is not one the page invents.
 *
 * **Notably absent, and each for a reason recorded on the platform side.** The driver's mobile
 * number, because they know it and a forwarded link should teach whoever holds it as little as
 * possible. The job's status, because that vocabulary belongs to `jobs` and a second copy is a
 * second list to keep in step. Anything about money, ever — the customer's budget is never exposed
 * to a provider or to their driver, in any form.
 *
 * **Also absent, and this one is a gap rather than a decision**: the pickup and drop-off, the goods
 * and who to call. `Docs/03` §3 puts them in the driver's Prepare stage and the endpoint does not
 * serve them yet. See `Docs/11` §3's SHIP-120 entry.
 */
export interface Delivery {
  job_id: string;
  assignment_id: string;
  driver_name: string;
  assigned_at: string;
  link_expires_at: string;
}

/**
 * The ways this page fails to show a delivery, or fails to record one.
 *
 * The first five are the read's and are SHIP-120's. The last three arrive with SHIP-121 and can only
 * come from a write: they are the platform saying that a perfectly well-formed request contradicts
 * something about *this delivery*, which a read has no way to do.
 */
export type Refusal =
  | "expired"
  | "invalid"
  | "closed"
  | "unavailable"
  | "unexpected"
  | "too_early"
  | "needs_evidence"
  | "rejected";

/** What one attempt at opening a link produced. */
export type Outcome =
  | { kind: "delivery"; delivery: Delivery }
  | { kind: "refused"; refusal: Refusal };

/**
 * The copy for each refusal.
 *
 * A record rather than a switch, so the type checker refuses a `Refusal` that nothing renders —
 * adding a case to the union without adding its message is a build failure rather than a blank
 * screen on somebody's phone.
 */
export const REFUSALS: Record<Refusal, { title: string; body: string }> = {
  expired: {
    title: "This link has expired",
    body:
      "Delivery links stop working after a while. Ask the transport provider who sent it to send " +
      "you a new one. There is no account to sign in to and nothing to refresh.",
  },
  invalid: {
    title: "This link is not valid",
    body:
      "Open it again from the message you were sent, rather than from your history — some " +
      "messaging apps shorten or wrap a link and break it. If it still does not work, ask the " +
      "transport provider who sent it for a new one.",
  },
  closed: {
    title: "This link no longer opens a delivery",
    body:
      "If you are still carrying this job, ask the transport provider who sent it for a new link.",
  },
  unavailable: {
    title: "We could not reach the delivery",
    body: "Check your signal and try again. Nothing you have done has been lost.",
  },
  unexpected: {
    title: "Something went wrong at our end",
    body: "Try again in a moment. If it keeps happening, tell the transport provider who sent you the link.",
  },
  too_early: {
    title: "Not yet",
    body:
      "The delivery has not reached that point. Record the step before it first — and if you have " +
      "already done that step, record it now and this one will go through.",
  },
  needs_evidence: {
    title: "A delivery needs a photograph",
    body:
      "Take a photograph of the goods where you left them, or say why there is no photograph. " +
      "Either finishes the job.",
  },
  rejected: {
    title: "That could not be recorded",
    body:
      "Something in that update was not accepted. Try again, and if it keeps happening tell the " +
      "transport provider who sent you the link.",
  },
};

/**
 * Whether the platform has answered, as opposed to not having been reached.
 *
 * This is what `lib/keys.ts` means by settled and it is the whole of the decision about a driver's
 * idempotency key: an answer ends the action, so the next tap mints a new key; no answer leaves the
 * action in flight, so the next tap reuses the one already sent. Getting it backwards in either
 * direction is a real defect on a phone — reuse where a fresh key was needed silently records
 * nothing, and a fresh key where reuse was needed silently records twice.
 *
 * `unavailable` and `unexpected` are the two that are not answers. `unavailable` is the phone: no
 * signal, a `429`, or a `503` from a service that says to try again. `unexpected` covers a `5xx`
 * and anything this application could not parse, which is the platform not having said anything
 * usable — and a retry under the same key is exactly what the mechanism is for.
 */
export function isSettled(refusal: Refusal): boolean {
  return refusal !== "unavailable" && refusal !== "unexpected";
}

/** The path this portal fetches a delivery from. Relative, and one of three in the application. */
export function deliveryPath(jobId: string): string {
  return `/api/driver/jobs/${encodeURIComponent(jobId)}`;
}

/** The path this portal records a milestone at. Relative, under the delivery it names. */
export function milestonePath(jobId: string): string {
  return `${deliveryPath(jobId)}/milestones`;
}

/**
 * The refusal a platform answer maps to.
 *
 * Pure, and separate from the request, because this mapping is the part with a decision in it and
 * the part worth testing. It branches on `code` and never on `message`, which is what the error
 * contract asks of every client: messages get reworded, and a portal that switched on text would
 * break on a copy edit.
 */
export function refusalFor(status: number, code: string | null): Refusal {
  if (status === 401) {
    return code === "delivery_driver_link_expired" ? "expired" : "invalid";
  }
  if (status === 404) return "closed";
  if (status === 429 || status === 503) return "unavailable";
  return "unexpected";
}

/**
 * The refusal a platform answer to a *write* maps to (SHIP-121).
 *
 * Everything the read can be told, plus the three a write can. It delegates rather than repeating,
 * so a change to what a `401` means changes it in one place for both.
 *
 * # `delivery_milestone_not_permitted` means "too early" and never "too late"
 *
 * That is SHIP-112's narrowing and it is the difference between a message worth reading and one that
 * is wrong. A milestone the delivery has already passed is **absorbed** — recorded as history, the
 * job left where it stands, answered `201` — so it never reaches here. What does reach here is a
 * milestone the delivery has not got to yet, and the identical request succeeds once it has. So the
 * copy says to keep going rather than to give up, which is the true thing.
 *
 * # A `409` on the key is the driver's own retry racing itself
 *
 * `idempotency_key_reused` means this key has already recorded something different, and
 * `idempotency_in_progress` means the first attempt is still running. Both are `unavailable`: the
 * honest instruction in each case is to wait a moment and try again, and neither is something the
 * driver did wrong. Treating them as unsettled is also correct for `lib/keys.ts` — in progress
 * genuinely is in progress.
 */
export function recordRefusalFor(status: number, code: string | null): Refusal {
  if (status === 409) {
    switch (code) {
      case "delivery_milestone_not_permitted":
        return "too_early";
      case "delivery_proof_required":
        return "needs_evidence";
      case "idempotency_key_reused":
      case "idempotency_in_progress":
        return "unavailable";
      default:
        // Every other 409 this route can answer is about the evidence: a photograph that never
        // arrived, one the platform will not accept, one already standing behind another
        // milestone. All three are "capture it again", which is what needs_evidence says.
        return "needs_evidence";
    }
  }
  if (status === 422) return "rejected";
  return refusalFor(status, code);
}

/**
 * Whether a parsed body is the delivery this portal expects.
 *
 * The response is checked rather than asserted. A page that trusted the shape would render
 * `undefined` into the driver's screen the first time the contract moved, and this is a surface
 * with no error reporting behind it and a user who cannot be asked to try the other browser.
 */
function isDelivery(value: unknown): value is Delivery {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.job_id === "string" &&
    typeof candidate.assignment_id === "string" &&
    typeof candidate.driver_name === "string" &&
    typeof candidate.assigned_at === "string" &&
    typeof candidate.link_expires_at === "string"
  );
}

/** The `code` out of an error-contract body, or null if there is not one in there. */
export function codeFrom(body: unknown): string | null {
  if (typeof body !== "object" || body === null) return null;
  const error = (body as Record<string, unknown>).error;
  if (typeof error !== "object" || error === null) return null;
  const code = (error as Record<string, unknown>).code;
  return typeof code === "string" ? code : null;
}

/**
 * Open one delivery with one link.
 *
 * The token goes in the bearer credential header and nowhere else — never in the path, never in a
 * query parameter, never in a body — so it is not in this origin's access log any more than it was
 * in the platform's.
 */
export async function openDelivery(
  jobId: string,
  token: string,
  signal?: AbortSignal,
): Promise<Outcome> {
  let response: Response;
  try {
    response = await fetch(deliveryPath(jobId), {
      method: "GET",
      headers: { Authorization: `Bearer ${token}`, Accept: "application/json" }, // spelling:ok — RFC 9110
      cache: "no-store",
      signal,
    });
  } catch {
    // No answer at all. On this surface that is a driver in a shed rather than a broken platform,
    // and "check your signal" is the true thing to say.
    return { kind: "refused", refusal: "unavailable" };
  }

  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }

  if (response.ok) {
    return isDelivery(body)
      ? { kind: "delivery", delivery: body }
      : { kind: "refused", refusal: "unexpected" };
  }

  return { kind: "refused", refusal: refusalFor(response.status, codeFrom(body)) };
}

/**
 * What the platform answers when a milestone is recorded (SHIP-121).
 *
 * The fields `POST /v1/driver/jobs/{id}/milestones` serves, and the page shows two of them. **The
 * job's status is deliberately not among them** and never will be: `internal/delivery` keeps that
 * vocabulary in `jobs`, and a copy here would be a second list to keep in step. So this page never
 * learns what status the delivery is in — it learns what it recorded, which is the only thing the
 * driver did.
 *
 * `recorded_at` is the **actor's** clock and `accepted_at` is the platform's. They are ninety
 * minutes apart when a phone has been out of signal, and the first is the one a driver is shown.
 */
export interface Recorded {
  id: string;
  job_id: string;
  milestone: string;
  recorded_by: string;
  recorded_at: string;
  accepted_at: string;
}

/** What one attempt at recording a milestone produced. */
export type RecordOutcome =
  | { kind: "recorded"; recorded: Recorded }
  | { kind: "refused"; refusal: Refusal };

/**
 * The evidence attached to a recording: a photograph the platform issued a key for, or a reason
 * there is none (`Docs/01` §4.4).
 *
 * Exactly one, never both and never neither, which is the shape `internal/delivery`'s
 * `proofRequest` refuses any other version of. A `Delivered` needs one; the other three take none.
 */
export type Evidence = { object_key: string } | { exception_reason: string };

/** Whether a parsed body is a recorded milestone. Checked rather than asserted, as the read is. */
function isRecorded(value: unknown): value is Recorded {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.id === "string" &&
    typeof candidate.job_id === "string" &&
    typeof candidate.milestone === "string" &&
    typeof candidate.recorded_by === "string" &&
    typeof candidate.recorded_at === "string" &&
    typeof candidate.accepted_at === "string"
  );
}

/**
 * Record one milestone on one delivery (SHIP-121).
 *
 * # The job identifier is the caller's and is never derived from the token
 *
 * The same statement `openDelivery` makes, and it matters more here because this writes.
 * `lib/link.ts` has the full argument; the short form is that SHIP-108 put the one-job check in the
 * platform's auth class, comparing the job in the path against the job in the grant — so a client
 * that built the path out of the grant would leave that check comparing the token with itself, and
 * it would pass for ever on any link however widely forwarded. `lib/one-job.test.ts` records the
 * requests this function actually issues, for exactly that reason.
 *
 * # `recorded_at` is sent, and it is the driver's phone rather than the platform's clock
 *
 * `Docs/02` §3.1 and `Docs/01` §4.4 both make the recorded time the time the driver acted, not the
 * time the request arrived — "milestone updates must succeed without a network connection… the
 * recorded time is the time the driver acted". This page has no offline queue (that is the Flutter
 * client's, SHIP-124), but the same request can sit in a browser's retry for minutes in a yard, and
 * stamping the tap is free and correct. The platform stores it beside, never instead of, its own
 * arrival time and deliberately does not bound it against now.
 *
 * # The key comes in as an argument and is not minted here
 *
 * `lib/keys.ts` owns that decision — one value per action, reused for every retry of that action,
 * discarded when the action settles — and it needs to know whether the attempt settled, which is
 * something only the caller learns. Minting one here would make every retry a new action.
 */
export async function recordMilestone(
  jobId: string,
  token: string,
  milestone: string,
  key: string,
  options: { evidence?: Evidence; recordedAt?: string; signal?: AbortSignal } = {},
): Promise<RecordOutcome> {
  const body: Record<string, unknown> = {
    milestone,
    recorded_at: options.recordedAt ?? new Date().toISOString(),
  };
  if (options.evidence !== undefined) body.proof = options.evidence;

  let response: Response;
  try {
    response = await fetch(milestonePath(jobId), {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`, // spelling:ok — RFC 9110
        Accept: "application/json",
        "Content-Type": "application/json",
        "Idempotency-Key": key,
      },
      body: JSON.stringify(body),
      cache: "no-store",
      signal: options.signal,
    });
  } catch {
    // No answer at all, which on this surface is a driver in a shed. The action has **not**
    // settled, so the caller keeps the key and the next tap is the same action retrying.
    return { kind: "refused", refusal: "unavailable" };
  }

  let parsed: unknown = null;
  try {
    parsed = await response.json();
  } catch {
    parsed = null;
  }

  if (response.ok) {
    // `201` is a fresh recording and `200` is this key's own earlier one coming back, which is the
    // retry path working. Both are the same outcome to a driver: it is recorded, once.
    return isRecorded(parsed)
      ? { kind: "recorded", recorded: parsed }
      : { kind: "refused", refusal: "unexpected" };
  }

  return { kind: "refused", refusal: recordRefusalFor(response.status, codeFrom(parsed)) };
}
