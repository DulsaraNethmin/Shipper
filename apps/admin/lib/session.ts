/**
 * The administrator's credential, and every decision about how the browser holds it (SHIP-188a).
 *
 * # Why this is not the driver portal's answer, and why copying it would have been wrong
 *
 * The driver portal holds its token in `sessionStorage` and its `lib/surface.test.ts` asserts that
 * `localStorage`, `document.cookie` and `cookies()` appear in no file at all. That is the right
 * answer **there** and the wrong one here, and the difference is not a matter of taste:
 *
 * - A driver's token is **job-scoped**. It grants one delivery, it is already in the URL the driver
 *   was sent, and the worst a stolen one does is show a stranger one job's milestones.
 * - An administrator's session reaches **every user's contact details, every job, the verification
 *   evidence and the audit trail**. It is the most valuable credential this product issues.
 *
 * A credential JavaScript can read is a credential any injected script can read, and an admin panel
 * is precisely where that matters. So the token is set as an **httpOnly** cookie by a route handler,
 * is never in a response body, and is never seen by a line of client code. The guard in
 * `lib/surface.test.ts` holds that as a property of the tree rather than a habit.
 *
 * # The cost of a cookie, paid rather than ignored
 *
 * A cookie is sent automatically. That is the whole benefit — the browser attaches it to this
 * origin's own route handlers with no JavaScript involved — and it is also the whole cost: a request
 * this origin did not initiate carries it too, which is what a bearer header does not do. That is
 * cross-site request forgery, and it is answered twice over:
 *
 *   - `SameSite=Strict` below, so the browser does not attach the cookie to a request that started
 *     on another site at all; and
 *   - an `Origin` check in every mutating route handler (`lib/origin.ts`), because `SameSite` is one
 *     browser's promise and a defence that depends on exactly one mechanism has no second chance.
 *
 * # Everything here is a pure function of its arguments
 *
 * No `next/headers`, no clock, no environment. That is what lets `lib/session.test.ts` assert the
 * attributes of the actual string that reaches the browser, rather than asserting that some code
 * intended to set them. The route handlers put the result in a `Set-Cookie` header.
 */

/**
 * The cookie's name.
 *
 * **Not `__Host-shipper_admin_session`, and the reason is worth recording because the prefix is
 * otherwise strictly better.** `__Host-` makes the browser enforce `Secure`, `Path=/` and no
 * `Domain` — exactly the three attributes set below — and refuses the cookie outright if any is
 * missing. It would be the right name for every deployment. It is the wrong name for `make web-dev
 * app=admin`, which serves plain HTTP on localhost where `Secure` cannot be set, so the prefix
 * would have to vary with the environment. **A cookie whose *name* changes between development and
 * production is a class of bug nobody finds until the day it matters** — a stale cookie under the
 * other name, a sign-out that clears one and leaves the other. One name, and `Secure` decided per
 * host below.
 */
export const SESSION_COOKIE = "shipper_admin_session";

/**
 * Whether this host gets `Secure`.
 *
 * It **fails closed**: an unknown host gets `Secure`, so the only way to serve the credential over
 * plain HTTP is to be recognisably local. Any real deployment has a real hostname and is therefore
 * secure without anybody configuring it, which is the property an environment variable would not
 * have — a variable can be forgotten, and forgetting this one sends the credential in clear.
 */
export function secureFor(host: string | null): boolean {
  if (host === null || host === "") return true;

  const hostname = host.replace(/:\d+$/, "").replace(/^\[|\]$/g, "").toLowerCase();
  return !(
    hostname === "localhost" ||
    hostname.endsWith(".localhost") ||
    hostname === "127.0.0.1" ||
    hostname === "::1"
  );
}

/** What a route handler needs to know to write the cookie. */
export interface SessionCookie {
  /** The credential the platform issued. */
  token: string;

  /** `session.expires_at` from the platform's answer, RFC 3339. */
  expiresAt: string;

  /** The `Host` header of the request being answered, which decides `Secure`. */
  host: string | null;

  /** Now, passed in rather than read, so the lifetime below is testable. */
  now: Date;
}

/**
 * The `Set-Cookie` value that installs the session.
 *
 * `Max-Age` is the platform's own expiry rather than a number chosen here, so the browser stops
 * sending the credential at the moment it stops working. That is a convenience and not a control —
 * the platform re-reads `admin_sessions` on every request, which is what makes revocation immediate
 * — but a browser that goes on presenting a dead credential turns every screen into a 401 with no
 * explanation, and the panel would have to guess why.
 *
 * **An expiry in the past becomes a session cookie rather than a deletion.** Emitting `Max-Age=0`
 * for a credential the platform just issued would install and immediately destroy it, which reads
 * in a capture exactly like a sign-out. If the platform hands back something already expired that
 * is the platform's defect, and the honest thing is to hold the credential for this tab and let the
 * next request be refused by the platform, which is the component that knows.
 */
export function sessionCookie({ token, expiresAt, host, now }: SessionCookie): string {
  const attributes = ["Path=/", "HttpOnly", "SameSite=Strict"];
  if (secureFor(host)) attributes.push("Secure");

  const expiry = Date.parse(expiresAt);
  if (Number.isFinite(expiry)) {
    const seconds = Math.floor((expiry - now.getTime()) / 1000);
    if (seconds > 0) attributes.push(`Max-Age=${seconds}`);
  }

  return `${SESSION_COOKIE}=${encodeURIComponent(token)}; ${attributes.join("; ")}`;
}

/**
 * The `Set-Cookie` value that removes it.
 *
 * **Every attribute except the lifetime is repeated, and that is required rather than tidy.** A
 * browser matches a deletion to an existing cookie by name, domain and path; a `Set-Cookie` that
 * omitted `Path=/` would be scoped to the path of the request that sent it and would leave the real
 * cookie in place. `Secure` is repeated for the same reason, and `HttpOnly` because a deletion that
 * dropped it would briefly replace an unreadable cookie with a readable one.
 *
 * `Max-Age=0` and an expiry in the past, because the two are understood by different browsers and
 * sending both costs nothing.
 */
export function clearedSessionCookie(host: string | null): string {
  const attributes = ["Path=/", "HttpOnly", "SameSite=Strict"];
  if (secureFor(host)) attributes.push("Secure");
  attributes.push("Max-Age=0", "Expires=Thu, 01 Jan 1970 00:00:00 GMT");

  return `${SESSION_COOKIE}=; ${attributes.join("; ")}`;
}

/**
 * The session token in a request's `Cookie` header, if there is one.
 *
 * A route handler could ask `next/headers` for this instead. It does not, and the reason is that
 * reading the header keeps every route handler a plain function from a `Request` to a `Response` —
 * which is what lets `lib/sessions.test.ts` run the real handlers under `node --test` and assert on
 * the requests they actually issue. A handler that reached for `cookies()` would only run inside a
 * Next request scope, and the tests would have to assert about the code instead of running it. The
 * driver portal paid for that lesson once already: `Docs/11` §7a records a guard that read source
 * and was satisfied by a rename.
 *
 * Parsing is deliberately unclever. A cookie value is percent-encoded on the way out by
 * [sessionCookie], so it is decoded here, and a name is matched exactly rather than by prefix — a
 * cookie called `shipper_admin_session_backup` is not this one.
 */
export function sessionTokenFrom(cookieHeader: string | null): string | null {
  if (cookieHeader === null || cookieHeader === "") return null;

  for (const pair of cookieHeader.split(";")) {
    const at = pair.indexOf("=");
    if (at < 0) continue;
    if (pair.slice(0, at).trim() !== SESSION_COOKIE) continue;

    const value = decodeURIComponent(pair.slice(at + 1).trim());
    return value === "" ? null : value;
  }

  return null;
}
