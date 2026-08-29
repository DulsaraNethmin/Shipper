/**
 * The second half of the cookie's cost, paid where the cookie is spent (SHIP-188a).
 *
 * `lib/session.ts` explains why this panel holds its credential in a cookie rather than in
 * JavaScript, and what that buys. This is what it costs: **a cookie is attached by the browser to
 * requests this origin did not initiate**, so a form on another site that posts to
 * `/api/admin/sessions/current` signs an administrator out, and a richer one against a route added
 * later does something worse. A bearer header has no equivalent exposure, because another site
 * cannot set one.
 *
 * `SameSite=Strict` already stops the browser attaching the cookie to such a request. This is the
 * second lock on the same door, and it exists because the first is **one browser's promise about a
 * category** — the category has been redefined before, `Lax` was the default nobody chose, and a
 * managed handset or an embedded view runs whatever engine somebody shipped. A control with one
 * mechanism has no second chance, and this one is nine lines.
 *
 * # It compares the `Origin` header with the request's own host, and absent means no
 *
 * A browser sends `Origin` on every request a script makes and on every non-`GET` form submission.
 * It is set by the browser and a page cannot forge it, which is the whole property being used here.
 *
 * **An absent `Origin` is refused rather than allowed**, and that is the decision worth stating
 * because the convenient reading goes the other way. Every client of these route handlers is a
 * browser — the panel's own screens — and every one of them sends the header. What does *not* send
 * it is a request made by something that is not a browser, which is either a mistake or somebody
 * probing, and neither is a caller to accommodate. The cost is that `curl` against these routes
 * needs `-H "Origin: …"`; the benefit is that the check has no hole in it.
 *
 * It deliberately does **not** consult `X-Forwarded-Host`. That header is written by whatever is in
 * front of this application and can be written by a caller when nothing is, so trusting it would
 * make the comparison forgeable by the party being checked. A reverse proxy in front of the panel
 * must therefore preserve `Host` — which is the ordinary configuration and is what SHIP-189's
 * deployment has to get right.
 */

/**
 * Whether this request came from this panel's own pages.
 *
 * Nothing here is an authorisation decision — `Docs/07` §3 forbids those on the client, and this is
 * a client by that document's reckoning. It does not decide who may do what; it declines to act on
 * a request the browser says started somewhere else. The platform decides everything else, on the
 * credential, as it does for every other surface.
 */
export function isSameOrigin(request: Request): boolean {
  const stated = request.headers.get("origin");
  const host = request.headers.get("host");
  if (stated === null || stated === "" || host === null || host === "") return false;

  let claimed: URL;
  try {
    claimed = new URL(stated);
  } catch {
    return false;
  }

  return claimed.host.toLowerCase() === host.toLowerCase();
}
