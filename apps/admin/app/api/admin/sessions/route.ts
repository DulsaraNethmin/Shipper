import { isSameOrigin } from "@/lib/origin";
import { sessionCookie } from "@/lib/session";
import { forwardRefusal, isJSON, platform, refuse, unreachable } from "@/lib/upstream";

/**
 * Sign-in, forwarded to `POST /v1/admin/sessions` and to nothing else (SHIP-188a).
 *
 * # Why a route handler at all, given there is no BFF tier
 *
 * Two reasons, and both are the driver portal's:
 *
 * - **The Go service serves no CORS headers.** A browser asked to send a credential header
 *   cross-origin sends a preflight `OPTIONS` first, and nothing in `internal/httpx` answers one, so
 *   a fetch straight from this origin to the platform's is refused by the browser before the
 *   platform ever sees it.
 * - **The API's location becomes a server variable read per request** rather than a `NEXT_PUBLIC_`
 *   constant baked into a bundle, which is what SHIP-189 requires of both web surfaces.
 *
 * `Docs/10` §8.4 already allows a web surface its own server-side data access as "an application
 * detail and not a shared platform tier", and this is that: one route, one method, one upstream
 * path, no state and no vocabulary of its own.
 *
 * # And one reason the driver portal does not have, which is the whole of SHIP-188a
 *
 * **This is the hop where the credential stops.** `POST /v1/admin/sessions` answers with a token;
 * the browser must never see it. So unlike every handler in the driver portal, this one does not
 * pass the platform's success through — it takes the token out of the body, puts it in an httpOnly
 * cookie, and hands back what is left. `lib/session.ts` argues the cookie; `lib/surface.test.ts`
 * asserts that no line of client code can read it.
 *
 * # The narrowness is the security property
 *
 * A `rewrites()` entry in `next.config.ts` would have been three lines and would have proxied
 * everything under `/v1` — making this origin a general, credential-forwarding front door to
 * twenty-four privileged endpoints. This route can only ever reach one: the method is `POST`
 * because no other export exists, the upstream path is a literal with no hole in it at all, and the
 * body it sends is built here from two named fields rather than forwarded.
 *
 * **There must never be a shared `forward(path, …)` helper in this application.** One route file,
 * one literal upstream template, one hole. `lib/surface.test.ts` fails if a file that mentions
 * `/v1/` carries more than one.
 */
export async function POST(request: Request): Promise<Response> {
  // See lib/origin.ts. The cookie this route installs is sent automatically by the browser, so
  // every route that spends or issues one is checked; SameSite=Strict is the other lock.
  if (!isSameOrigin(request)) {
    return refuse(403, "forbidden", "That request did not come from the panel.");
  }

  let submitted: { email?: unknown; password?: unknown };
  try {
    submitted = (await request.json()) as { email?: unknown; password?: unknown };
  } catch {
    return refuse(400, "bad_request", "That is not a sign-in this panel can send.");
  }

  const { email, password } = submitted;
  if (typeof email !== "string" || typeof password !== "string") {
    return refuse(400, "bad_request", "That is not a sign-in this panel can send.");
  }

  const headers = new Headers({
    Accept: "application/json",
    "Content-Type": "application/json",
  });

  // Minted in the browser and forwarded, never minted here: a key generated at a hop changes on
  // every attempt, which is the header doing the opposite of its job. See lib/keys.ts. An absent
  // key is not refused locally — the platform's own `idempotency_key_required` is the authoritative
  // answer, and duplicating the rule here would give the panel a second version of it to drift from.
  const key = request.headers.get("idempotency-key");
  if (key !== null) headers.set("Idempotency-Key", key);

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/admin/sessions`, {
      method: "POST",

      // Rebuilt from the two named fields rather than forwarded, so nothing else the browser sent
      // reaches the platform. `httpx.DecodeJSON` would refuse an unknown field anyway; this makes
      // the panel incapable of asking rather than reliant on being told no.
      body: JSON.stringify({ email, password }),
      headers,
      cache: "no-store",
    });
  } catch {
    return unreachable();
  }

  if (!isJSON(upstream)) return unreachable();

  if (!upstream.ok) {
    const forwarded = await forwardRefusal(upstream);

    // Sign-in is rate limited per account and per address, and the platform says when to come back
    // (internal/admin/http.go). A throttled administrator told to wait without being told how long
    // either gives up or hammers the endpoint, so the header travels with the refusal.
    const retryAfter = upstream.headers.get("retry-after");
    if (retryAfter !== null) forwarded.headers.set("Retry-After", retryAfter);
    return forwarded;
  }

  let issued: { token?: unknown; session?: { id?: unknown; expires_at?: unknown }; administrator?: unknown };
  try {
    issued = await upstream.json();
  } catch {
    return unreachable();
  }

  if (typeof issued.token !== "string" || typeof issued.session?.expires_at !== "string") {
    return unreachable();
  }

  // What goes back to the browser: everything the platform sent except the credential. The panel
  // renders the administrator from `GET /v1/admin/me` on the next render anyway — this body is what
  // lets the form say "signed in" before that render happens, and it is deliberately checked by
  // lib/sessions.test.ts for the absence of the token rather than for the presence of the rest.
  return new Response(
    JSON.stringify({ administrator: issued.administrator, session: issued.session }),
    {
      status: 200,
      headers: {
        "content-type": "application/json",
        "cache-control": "no-store",
        "set-cookie": sessionCookie({
          token: issued.token,
          expiresAt: issued.session.expires_at,
          host: request.headers.get("host"),
          now: new Date(),
        }),
      },
    },
  );
}
