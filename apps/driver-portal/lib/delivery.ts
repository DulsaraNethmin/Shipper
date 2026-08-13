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

/** The ways this page fails to show a delivery. */
export type Refusal = "expired" | "invalid" | "closed" | "unavailable" | "unexpected";

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
};

/** The path this portal fetches a delivery from. Relative, and the only one in the application. */
export function deliveryPath(jobId: string): string {
  return `/api/driver/jobs/${encodeURIComponent(jobId)}`;
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
