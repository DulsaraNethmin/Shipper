/**
 * The two things every one of this panel's server-side callers needs, and deliberately nothing else
 * (SHIP-188a).
 *
 * This is the driver portal's `lib/upstream.ts` with one deliberate difference, and the difference
 * is the reason it is a second file rather than a shared package. The portal passes the platform's
 * answer through untouched; **this panel cannot**, because the one thing sign-in answers with is a
 * credential, and the whole of SHIP-188a is that the credential never reaches the browser's
 * JavaScript. So `passThrough` is absent here on purpose and each route handler decides what it
 * hands back.
 *
 * # What may live here, and what may not
 *
 * It may know **where** the platform is and **what a refusal looks like**. It may not know how to
 * reach an endpoint, and it may not name one. That rule is the driver portal's and it is what makes
 * `lib/surface.test.ts`'s central assertion possible: a platform path appears in exactly the files
 * that are allowed to name one, and each of those names exactly one. A shared `forward(path, …)`
 * helper would be a general, credential-forwarding proxy with the endpoint as an argument — and
 * here the door it opens is onto the twenty-four `/v1/admin/**` endpoints, every one of which reads
 * or writes a real person's account.
 */

/**
 * Where the platform is, read per request rather than inlined at build time.
 *
 * The default is the port `make run` serves on in a primary tree. A worktree serving elsewhere, or
 * any deployment at all, sets `SHIPPER_API_BASE_URL` — the same variable the driver portal reads,
 * which is what SHIP-189 requires of both surfaces: neither bundle carries a compiled-in API host.
 */
export function platform(): string {
  return (process.env.SHIPPER_API_BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "");
}

/**
 * A refusal in the platform's own error contract, so the browser has one parser rather than two.
 *
 * There is no `request_id`, and its absence is the honest signal: this refusal was made here and no
 * request reached the platform, so there is no identifier for anybody to look up.
 */
export function refuse(status: number, code: string, message: string): Response {
  return Response.json({ error: { code, message } }, {
    status,
    headers: { "cache-control": "no-store" },
  });
}

/** What a route handler answers when the platform is unreachable or answered outside the contract. */
export function unreachable(): Response {
  return refuse(503, "service_unavailable",
    "The platform is not answering. Try again in a moment.");
}

/**
 * Whether an upstream answer is the JSON this panel will act on.
 *
 * Anything else came from something between here and the platform — a load balancer's HTML error
 * page is the usual one — and reading a credential out of it, or forwarding it under a JSON content
 * type, would both be wrong for the same reason: it is not the contract.
 */
export function isJSON(response: Response): boolean {
  return (response.headers.get("content-type") ?? "").toLowerCase().startsWith("application/json");
}

/**
 * The platform's refusal, forwarded unchanged apart from being marked uncacheable.
 *
 * Used only on the paths where the platform said no. A **successful** answer is never forwarded
 * verbatim by this panel — see the file note — so this function is deliberately narrower than the
 * driver portal's `passThrough`, and its name says which case it is for.
 */
export async function forwardRefusal(upstream: Response): Promise<Response> {
  return new Response(await upstream.text(), {
    status: upstream.status,
    headers: { "content-type": "application/json", "cache-control": "no-store" },
  });
}
