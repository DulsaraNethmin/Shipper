/**
 * The two things every one of this portal's route handlers needs, and deliberately nothing else
 * (SHIP-121).
 *
 * # What is here, and the much more important question of what is not
 *
 * SHIP-120 had one route handler, so where the platform lives and what a locally-made refusal looks
 * like sat inside it. SHIP-121 adds a second and SHIP-122 a third, and three copies of the base-URL
 * fallback is three places for a deployment to be half-configured.
 *
 * **What did not move is the `fetch` and the upstream path.** That is the whole security property of
 * `app/api/driver/**` and it is written out per route on purpose: a shared `forward(path, …)` helper
 * would be a general, credential-forwarding proxy with the endpoint as an argument, which is exactly
 * the shape `route.ts` refused when it declined a `rewrites()` entry in `next.config.ts`. Each route
 * file names one path, as a template with one hole, and `lib/surface.test.ts` holds every file that
 * mentions `/v1/` to exactly one such template.
 *
 * So the rule for anything added here: it may know **where** the platform is and **what a refusal
 * looks like**. It may not know how to reach an endpoint, and it may not name one.
 */

/**
 * Where the platform is, read per request rather than inlined at build time.
 *
 * The default is the port `make run` serves on in a primary tree. A worktree serving elsewhere, or
 * any deployment at all, sets `SHIPPER_API_BASE_URL` — see this application's README.
 *
 * Reading it at request time rather than at build time is deliberate and is argued in `route.ts`:
 * "anything expected to change under operational pressure lives server-side" is a rule this
 * repository applies to Flutter because Dart cannot be updated over the air, and an inlined constant
 * in a JavaScript bundle is the same shape of problem with a shorter fuse.
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

/**
 * What a route handler answers when the platform is unreachable or answered with something that is
 * not the contract.
 *
 * One function rather than the same three arguments written at five call sites, because the copy is
 * what a driver in a yard actually reads and it should not drift between the read and the write.
 */
export function unreachable(): Response {
  return refuse(503, "service_unavailable",
    "The delivery service is not answering. Try again in a moment.");
}

/**
 * Whether an upstream answer is the JSON this portal will pass through.
 *
 * Anything else came from something between here and the platform — a load balancer's HTML error
 * page is the usual one — and forwarding it under a JSON content type would hand the browser a body
 * it cannot parse and a status it would act on.
 */
export function isJSON(response: Response): boolean {
  return (response.headers.get("content-type") ?? "").toLowerCase().startsWith("application/json");
}

/** The platform's answer, passed through unchanged apart from being marked uncacheable. */
export async function passThrough(upstream: Response): Promise<Response> {
  return new Response(await upstream.text(), {
    status: upstream.status,
    headers: { "content-type": "application/json", "cache-control": "no-store" },
  });
}
