import { isJobId } from "@/lib/link";
import { isJSON, passThrough, platform, refuse, unreachable } from "@/lib/upstream";

/**
 * The milestone write, forwarded to `POST /v1/driver/jobs/{id}/milestones` and to nothing else
 * (SHIP-121).
 *
 * # A second file rather than a second branch in the first one
 *
 * The read's header argues at length why this origin is not a proxy: "this route can only ever
 * reach one endpoint: the method is `GET` because no other export exists, the upstream path is a
 * template, and the one hole in that template is refused unless it is a job identifier". Adding a
 * `POST` export beside that `GET` would have kept the property; adding a path parameter would have
 * destroyed it. **A file per upstream endpoint is what makes the property mechanical** — the number
 * of endpoints this origin can reach is the number of `/v1/` templates in `app/`, which
 * `lib/surface.test.ts` counts.
 *
 * Next resolves `app/api/driver/jobs/[jobId]/milestones/route.ts` from the URL path, so a request
 * for `/api/driver/jobs/<id>` cannot reach this handler and a request for
 * `/api/driver/jobs/<id>/milestones` cannot reach the read's. That is the router doing the
 * separation rather than a `switch` somebody could reorder.
 *
 * # What is forwarded, and the two headers that are not optional
 *
 * The credential, because the platform decides and this route decides nothing. And
 * `Idempotency-Key`, because `POST /v1/driver/jobs/{id}/milestones` is state-changing and the
 * service refuses one without it (SHIP-15) — a portal that dropped the header would turn every
 * recording into a `400` that named a header the browser had actually sent.
 *
 * **The key is passed through rather than generated here.** It identifies the driver's *action*, and
 * the action happened in the browser: a key minted on this hop would be a new one for every retry,
 * which is the precise opposite of what the header is for. `lib/keys.ts` holds the browser's, across
 * a reload, until the attempt settles.
 *
 * Nothing else crosses. No cookies, no query string, no client headers beyond the credential, the
 * key and the media type — and in particular **no `X-Forwarded-For`**: `Docs/11` §9 records that
 * the platform deliberately does not read one, and a portal that started sending one would be
 * offering a caller their own rate-limit bucket.
 *
 * # The body is passed through unread, and that is deliberate rather than lazy
 *
 * This handler does not know what a milestone is. `internal/delivery` validates the body, names the
 * field that is wrong, and lists the values it accepts — and a second copy of that vocabulary here
 * would be a second list to keep in step, in the one place that cannot be tested against the
 * service. What this file guarantees is the *destination*, which is the only thing a proxy can
 * guarantee that the platform cannot.
 */
export async function POST(
  request: Request,
  context: { params: Promise<{ jobId: string }> },
): Promise<Response> {
  const { jobId } = await context.params;

  if (!isJobId(jobId)) {
    return refuse(400, "bad_request", "That is not a delivery this link can record against.");
  }

  const headers = new Headers({ Accept: "application/json", "Content-Type": "application/json" });
  const credential = request.headers.get("authorization"); // spelling:ok — HTTP header name, RFC 9110
  if (credential !== null) headers.set("Authorization", credential); // spelling:ok — RFC 9110
  const key = request.headers.get("idempotency-key");
  if (key !== null) headers.set("Idempotency-Key", key);

  const body = await request.text();

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/driver/jobs/${jobId}/milestones`, {
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
