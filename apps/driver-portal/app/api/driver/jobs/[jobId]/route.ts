import { isJobId } from "@/lib/link";

/**
 * The portal's one outbound call, forwarded to `GET /v1/driver/jobs/{id}` and to nothing else
 * (SHIP-120).
 *
 * # Why this exists at all, given there is no BFF tier
 *
 * `CLAUDE.md` is explicit that the Go platform owns the versioned public API directly and that
 * there is no tier in front of it. This is not one. `Docs/10` §8.4 already allows a web surface its
 * own server-side data access as "an application detail and not a shared platform tier", and what
 * is below is that: one route, one method, one upstream path, no logic, no state and no vocabulary
 * of its own. It adds nothing to the API and hides nothing from it — the platform's status code and
 * its body, request id included, are what the browser receives.
 *
 * It exists because **the service serves no CORS headers**. A browser asked to send a bearer
 * credential header cross-origin sends a preflight `OPTIONS` first, and nothing in
 * `internal/httpx` answers one, so a fetch straight from this origin to the platform's is refused
 * by the browser before the platform ever sees it. Two ways out, and this is the smaller:
 *
 * - **CORS on the Go service**, which is a shared-surface change this lane may not make, and which
 *   is a decision about who may call the API from a browser rather than about a driver portal. If
 *   it is taken, this file is deleted and `openDelivery` fetches the platform directly.
 * - **A same-origin route on the portal**, which is this, and which turns out to have a property
 *   worth keeping either way: the API's location becomes a *server* environment variable read at
 *   request time rather than a `NEXT_PUBLIC_` value baked into a bundle at build time. "Anything
 *   expected to change under operational pressure lives server-side" is a rule this repository
 *   applies to Flutter because Dart cannot be updated over the air; an inlined constant in a
 *   JavaScript bundle is the same shape of problem with a shorter fuse.
 *
 * # The narrowness is the security property, not a stylistic preference
 *
 * A `rewrites()` entry in `next.config.ts` would have been three lines and would have proxied
 * *everything* under `/v1`, which makes this origin a general, credential-forwarding front door to
 * the whole platform. This route can only ever reach one endpoint: the method is `GET` because no
 * other export exists, the upstream path is a template, and the one hole in that template is
 * refused unless it is a job identifier. `..%2f..%2fjobs` does not construct a URL here; it
 * produces a `400` and no outbound request.
 *
 * That refusal is **not an authorisation decision**. It does not decide who may see what — it
 * declines to build a URL other than the one this route exists for. Whether the caller may open
 * that job is the platform's answer, and it is the only answer forwarded below.
 *
 * # The token passes through in a header and lands in no log
 *
 * A request line reaches an access log; a header does not. The token is read from the request's
 * credential header and written to the upstream one, and the job identifier — which is not a
 * credential itself, and which the platform refuses to serve to anyone not holding the token — is
 * the only thing in the path. Nothing else is forwarded: no cookies, no query string, no client
 * headers beyond the credential and the media type.
 */

/**
 * Where the platform is, read per request rather than inlined at build time.
 *
 * The default is the port `make run` serves on in a primary tree. A worktree serving elsewhere, or
 * any deployment at all, sets `SHIPPER_API_BASE_URL` — see this application's README.
 */
function platform(): string {
  return (process.env.SHIPPER_API_BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "");
}

/**
 * A refusal in the platform's own error contract, so the browser has one parser rather than two.
 *
 * There is no `request_id`, and its absence is the honest signal: this refusal was made here and
 * no request reached the platform, so there is no identifier for anybody to look up.
 */
function refuse(status: number, code: string, message: string): Response {
  return Response.json({ error: { code, message } }, {
    status,
    headers: { "cache-control": "no-store" },
  });
}

export async function GET(
  request: Request,
  context: { params: Promise<{ jobId: string }> },
): Promise<Response> {
  const { jobId } = await context.params;

  if (!isJobId(jobId)) {
    return refuse(400, "bad_request", "That is not a delivery this link can open.");
  }

  const headers = new Headers({ Accept: "application/json" });
  const credential = request.headers.get("authorization"); // spelling:ok — HTTP header name, RFC 9110
  if (credential !== null) headers.set("Authorization", credential); // spelling:ok — RFC 9110

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/driver/jobs/${jobId}`, {
      method: "GET",
      headers,
      cache: "no-store",
    });
  } catch {
    return refuse(503, "service_unavailable", "The delivery service is not answering. Try again in a moment.");
  }

  // Only a JSON answer is passed through. Anything else came from something between here and the
  // platform — a load balancer's HTML error page is the usual one — and forwarding it under a JSON
  // content type would hand the browser a body it cannot parse and a status it would act on.
  const contentType = upstream.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    return refuse(503, "service_unavailable", "The delivery service is not answering. Try again in a moment.");
  }

  return new Response(await upstream.text(), {
    status: upstream.status,
    headers: { "content-type": "application/json", "cache-control": "no-store" },
  });
}
