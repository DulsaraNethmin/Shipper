import { isJobId } from "@/lib/link";
import { isJSON, passThrough, platform, refuse, unreachable } from "@/lib/upstream";

/**
 * The presign, forwarded to `POST /v1/driver/jobs/{id}/proof-uploads` and to nothing else
 * (SHIP-122).
 *
 * # The third file, and the count is the point
 *
 * `lib/surface.test.ts` holds every file that mentions `/v1/` to being a route handler naming
 * exactly one upstream template, so **the number of those templates is the number of endpoints this
 * origin can reach**. It went from one to two at SHIP-121 and from two to three here, and each step
 * was somebody deciding to add a destination rather than a helper quietly gaining a parameter. The
 * read route's header argues why a shared `forward(path, …)` is the `rewrites()` entry
 * `next.config.ts` refused, with more steps.
 *
 * # What comes back through here is a credential, which no other route on this origin answers with
 *
 * `upload_url` is the whole of the authorisation to write that object — anybody holding it can,
 * until it expires, and nothing revokes a pre-signed URL. Two consequences for this file:
 *
 * - **It is never logged and never put in a URL.** Nothing here logs anything, which is the same
 *   position the read takes about the token.
 * - **`cache-control: no-store`**, which `passThrough` sets on every answer. A browser or an
 *   intermediary holding this response would be holding a live write credential for somebody else's
 *   evidence bucket.
 *
 * # The bytes do not come through this origin either
 *
 * The browser PUTs straight to the store (`Docs/06` §5.2 — "never proxied through the API", and this
 * origin is not the API but has no more business carrying megabytes of image than it does). So there
 * is no upload route here and there must not be one: what this file forwards is a request for
 * permission, and permission is all that comes back.
 *
 * **That direct PUT is cross-origin and needs CORS on the bucket**, which is the one thing about
 * this ticket that is a deployment fact rather than a code fact. MinIO allows it in development;
 * an S3 bucket needs a CORS configuration permitting `PUT` from the portal's origin with
 * `Content-Type` in the allowed headers. `Docs/11` §9 records it, because a bucket policy is not
 * something this repository can hold a test against.
 */
export async function POST(
  request: Request,
  context: { params: Promise<{ jobId: string }> },
): Promise<Response> {
  const { jobId } = await context.params;

  if (!isJobId(jobId)) {
    return refuse(400, "bad_request", "That is not a delivery this link can upload to.");
  }

  const headers = new Headers({ Accept: "application/json", "Content-Type": "application/json" });
  const credential = request.headers.get("authorization"); // spelling:ok — HTTP header name, RFC 9110
  if (credential !== null) headers.set("Authorization", credential); // spelling:ok — RFC 9110
  const key = request.headers.get("idempotency-key");
  if (key !== null) headers.set("Idempotency-Key", key);

  const body = await request.text();

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/driver/jobs/${jobId}/proof-uploads`, {
      method: "POST",
      headers,
      body,
      cache: "no-store",
    });
  } catch {
    return unreachable();
  }

  if (!isJSON(upstream)) return unreachable();

  return passThrough(upstream);
}
