/**
 * The link is the credential, and this file is everything the portal knows about holding one
 * (SHIP-120).
 *
 * A driver has no account, no password and no session. What the transport provider forwards is a
 * URL, and inside it is a signed job-scoped token (SHIP-107). Every decision about where that
 * token lives between arriving and being spent is made here, in one file, because SHIP-121,
 * SHIP-122 and SHIP-123 all inherit it.
 *
 * # The shape of a link, and why the job identifier is in it twice over
 *
 *     https://<portal>/j/<job-id>#<token>
 *
 * The token already names the job it grants — that claim is inside the signature, which is what
 * makes it single-job (SHIP-107). So carrying the identifier in the path as well looks redundant,
 * and it is the mechanism rather than the redundancy.
 *
 * `GET /v1/driver/jobs/{id}` compares the job in the path against the job in the token and refuses
 * a mismatch, and SHIP-108 put that comparison in the auth class precisely so it cannot be skipped.
 * **A client that decoded `job_id` out of the token and used it to build the path would make that
 * comparison compare the token with itself.** It would pass for ever, on any grant, however widely
 * it had been issued — the platform would still be checking, and the check would have nothing left
 * to catch. So the identifier reaches this portal independently, the token is never decoded here,
 * and the two are presented to the platform as what they are: what the client means to open, and
 * what the caller is allowed to open.
 *
 * That is also why nothing in this portal parses a JWT. The one claim a page might want —
 * when the link stops working — is served as `link_expires_at` by the endpoint, which is what the
 * contract already tells clients to do instead of reading a credential.
 *
 * # Where the token lives once the link is opened, and what each alternative costs
 *
 * It arrives in the **URL fragment** and is moved into **`sessionStorage`** on arrival, after which
 * the fragment is stripped from the address bar.
 *
 * A fragment is the one part of a URL that **is never sent to any server**. It is not in the
 * request line, so it reaches no access log, no proxy, no CDN and no `Referer` header on anything
 * the page later loads. A token in the path or the query string is in all of them, at every hop,
 * for as long as those logs are kept — and this is a credential that lasts seven days and is
 * forwarded through whatever messaging channel the provider already uses.
 *
 * Stripping it afterwards is the second half. The driver is holding a phone, outdoors, often
 * beside the customer; the address bar is the most screenshotted, most shoulder-surfed surface
 * they have. Once the token is in `sessionStorage` the URL is just a job identifier, which the
 * platform refuses to serve to anybody who is not holding the token.
 *
 * `sessionStorage` rather than the alternatives:
 *
 * - **`localStorage`** would leave a working credential on the device until something deleted it.
 *   Drivers share phones and hand them over; a seven-day token that outlives the tab is a
 *   liability with no matching benefit.
 * - **A cookie** is sent automatically, which sounds like a convenience and is the problem: it
 *   would be attached to every request to this origin, including ones that have nothing to do with
 *   the delivery, and it would need a scope and a lifetime decided here rather than by the token's
 *   own `exp`. The platform wants a bearer token in a credential header, and a cookie would
 *   have to be unpacked into one anyway.
 * - **The URL, for the life of the session**, is what stripping the fragment gives up, and the
 *   cost is real: closing the tab loses the token. That is the right trade, because **the message
 *   thread the link arrived in is the durable store**. The driver already has it, it is where they
 *   will look, and it is the only copy that should survive.
 *
 * What this file does not do is decide anything. Recognising that a string is *shaped* like an
 * identifier or a credential is not deciding whether it grants access to anything — the platform
 * decides that, on every request, and a portal that concluded otherwise would be making an
 * authorisation decision on the device (`Docs/07` §3).
 */

/** The canonical 36-character UUID, which is the only shape a job identifier takes on the wire. */
const JOB_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * A three-segment base64url JWS, which is the shape `delivery` signs (`Docs/10` §5).
 *
 * This is a test for "is there a credential in this link at all", not for "is this credential
 * good". A driver whose messaging app wrapped the URL across two lines has a broken link rather
 * than an expired one, and telling them so without a pointless round trip is the whole benefit.
 */
const TOKEN = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

/** Whether `value` is shaped like a job identifier. */
export function isJobId(value: string): boolean {
  return JOB_ID.test(value);
}

/**
 * The token carried in a URL fragment, or null when the fragment carries no credential.
 *
 * Takes the fragment rather than reading `window.location` so that it can be tested without a
 * browser, and so that the one place the address bar is read is visible in the component.
 */
export function tokenFromFragment(fragment: string): string | null {
  const candidate = fragment.replace(/^#/, "").trim();
  return TOKEN.test(candidate) ? candidate : null;
}

/**
 * Where one link is held.
 *
 * Keyed by job, so a driver carrying two deliveries in two tabs cannot have one link overwrite the
 * other — and so a token left over from a previous delivery is never presented on a different job.
 * That second property is a tidiness measure and not a control: presenting the wrong link is
 * refused by the platform, which is where SHIP-108 put the check.
 */
export function storageKey(jobId: string): string {
  return `shipper.delivery-link.${jobId}`;
}

/**
 * Hold the token for the rest of this tab session, answering whether it was held.
 *
 * The answer is load-bearing rather than informational: the caller strips the fragment from the
 * address bar only when the token is somewhere a reload can find it. A browser with storage
 * disabled — private mode on some devices, a locked-down managed handset — then keeps a working
 * link in its URL, which is worse in principle and much better than a page that cannot be
 * reloaded in a yard with one bar of signal.
 */
export function rememberToken(jobId: string, token: string): boolean {
  try {
    window.sessionStorage.setItem(storageKey(jobId), token);
    return true;
  } catch {
    return false;
  }
}

/** The token held for this job in this tab, if there is one. */
export function recallToken(jobId: string): string | null {
  try {
    const held = window.sessionStorage.getItem(storageKey(jobId));
    return held !== null && TOKEN.test(held) ? held : null;
  } catch {
    return null;
  }
}
