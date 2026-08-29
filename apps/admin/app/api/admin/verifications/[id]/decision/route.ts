import { isSameOrigin } from "@/lib/origin";
import { sessionTokenFrom } from "@/lib/session";
import { forwardRefusal, isJSON, platform, refuse, unreachable } from "@/lib/upstream";

/**
 * The verification decision, forwarded to `POST /v1/admin/verifications/{id}/decision` and to
 * nothing else (SHIP-188d, over SHIP-154).
 *
 * # The panel's third route handler, and the first that is not about the session
 *
 * Sign-in and sign-out need one because a form in a browser has to reach them. Every *read* the
 * panel does is a server render instead, because nothing in the browser needs to ask — the server
 * knows before it renders, and a route handler for a read would be a browser-reachable endpoint
 * added for nobody.
 *
 * A decision is different in the one way that matters: **the browser is the only participant that
 * can decide whether a submission is a new action or a retry of the last one**, and `Docs/09`'s row
 * requires the `Idempotency-Key` to be minted there. A server action or a server-rendered form post
 * would mint at a hop, and a key that changes on every attempt is the header doing the exact
 * opposite of its job. `components/verification-decision.tsx` holds the key across retries; this
 * forwards it and mints nothing.
 *
 * # One file, one literal template, one hole in the path
 *
 * The second and last place in this application where anything but `platform()` reaches a `/v1/`
 * template. The identifier is checked before it is interpolated — the canonical hyphenated form and
 * nothing else — so no crafted segment is ever sent, and `lib/surface.test.ts` spells the hole in
 * its allow-list so the interpolation is visible at review rather than buried.
 *
 * **There must never be a shared `forward(path, …)` helper in this application.** The door that
 * would open is onto twenty-four privileged endpoints, and this one can only ever reach one: the
 * method is `POST` because no other export exists, and the body is rebuilt from two named fields.
 */

/** The canonical hyphenated form, checked before the identifier reaches a path. */
const IDENTIFIER = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export async function POST(
  request: Request,
  context: RouteContext<"/api/admin/verifications/[id]/decision">,
): Promise<Response> {
  // See lib/origin.ts. A cookie is attached by the browser to a request another site made, so a
  // form on another origin would otherwise be able to record a verification decision — which is a
  // write to two append-only tables and cannot be undone.
  if (!isSameOrigin(request)) {
    return refuse(403, "forbidden", "That request did not come from the panel.");
  }

  const { id } = await context.params;
  if (!IDENTIFIER.test(id)) {
    return refuse(400, "bad_request", "That is not a provider identifier.");
  }

  const token = sessionTokenFrom(request.headers.get("cookie"));
  if (token === null) {
    return refuse(401, "unauthenticated", "You are not signed in to the panel.");
  }

  let submitted: { state?: unknown; reason?: unknown };
  try {
    submitted = (await request.json()) as { state?: unknown; reason?: unknown };
  } catch {
    return refuse(400, "bad_request", "That is not a decision this panel can send.");
  }

  const { state, reason } = submitted;
  if (typeof state !== "string" || typeof reason !== "string") {
    return refuse(400, "bad_request", "That is not a decision this panel can send.");
  }

  const headers = new Headers({
    Accept: "application/json",
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`, // spelling:ok — RFC 9110 header name
  });

  // Minted in the browser and forwarded, never minted here. An absent key is not refused locally —
  // the platform's own `idempotency_key_required` is the authoritative answer, and duplicating the
  // rule here would give the panel a second version of it to drift from.
  const key = request.headers.get("idempotency-key");
  if (key !== null) headers.set("Idempotency-Key", key);

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/admin/verifications/${id}/decision`, {
      method: "POST",

      // Rebuilt from the two named fields rather than forwarded, so nothing else the browser sent
      // reaches the platform. httpx.DecodeJSON would refuse an unknown field anyway; this makes
      // the panel incapable of asking rather than reliant on being told no — and the fields it
      // deliberately cannot send are the ones the platform takes from the credential instead: no
      // reviewer, and no `from`.
      body: JSON.stringify({ state, reason }),
      headers,
      cache: "no-store",
    });
  } catch {
    return unreachable();
  }

  if (!isJSON(upstream)) return unreachable();
  if (!upstream.ok) return forwardRefusal(upstream);

  let change: { provider_id?: unknown; from?: unknown; to?: unknown; reason?: unknown };
  try {
    change = await upstream.json();
  } catch {
    return unreachable();
  }

  // Built from named fields rather than forwarded verbatim, which is this application's rule for a
  // success (see lib/upstream.ts). Both ends of the move, because one endpoint serves all four
  // outcomes and "Restricted" alone does not say whether that was a tightening or a loosening.
  return Response.json(
    {
      provider_id: change.provider_id,
      from: change.from,
      to: change.to,
      reason: change.reason,
    },
    { status: 200, headers: { "cache-control": "no-store" } },
  );
}
