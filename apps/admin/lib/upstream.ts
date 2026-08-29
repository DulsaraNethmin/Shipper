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

/**
 * What a screen got back from the platform (SHIP-188b).
 *
 * Four answers, and every screen renders a different thing for each. **`refused` is separate from
 * `unavailable` for the reason `lib/administrator.ts` separates `signed-out` from it**, and this is
 * the case `Docs/09`'s SHIP-188b row names outright: "a search made by a session without the
 * permission renders the platform's refusal rather than an empty table". A 403 collapsed into an
 * empty result set tells a support engineer their search found nothing, when what happened is that
 * their role does not hold `users.read` — so they search again, differently, for something that was
 * never going to be shown to them.
 *
 * `signed-out` is the session dying between the layout's `GET /v1/admin/me` and the screen's own
 * call. Rare, and it has a correct answer — the sign-in form — rather than an error card.
 */
export type Answer<T> =
  | { state: "ok"; body: T }
  | { state: "signed-out" }
  | { state: "refused"; refusal: Refusal }
  | { state: "unavailable" };

/**
 * One field the platform would not accept, from the error contract's `details` (SHIP-12).
 *
 * **Rendering these is the difference between a refusal a person can act on and one they cannot.**
 * A `validation_failed` carries the top-level message "Some of the details you entered need
 * attention" — true, and useless on its own — while the sentence that says *which* detail and what
 * the accepted values are lives here. `Docs/10` §4.6 has the platform report every problem rather
 * than the first for exactly this reason, and a panel that read only `message` would throw all of it
 * away.
 */
export interface FieldProblem {
  field: string;
  code: string;
  message: string;
}

/** A refusal, as much of the contract as a screen has any use for. */
export interface Refusal {
  code: string;
  message: string;

  /**
   * The request identifier, which is in every error body (SHIP-12, SHIP-14).
   *
   * Shown to the reader rather than logged, because on a support surface the person looking at the
   * screen is often not the person who can read the logs. An operator who can quote it has turned
   * "the panel would not let me" into something somebody can look up.
   */
  requestId: string;

  /** Empty unless the refusal was about the request's own fields. */
  details: FieldProblem[];
}

/** The platform's error contract, as much of it as a screen renders. */
interface ErrorBody {
  error?: {
    code?: unknown;
    message?: unknown;
    request_id?: unknown;
    details?: unknown;
  };
}

/**
 * Classify one upstream answer.
 *
 * It takes a `Response` rather than making one, which is what keeps every endpoint named in the
 * screen that reads it: a helper that fetched would need the path as an argument, and that is the
 * shared forwarder this application refuses (see `lib/credential.ts`).
 *
 * **A refusal's `message` is the platform's own words, never a local copy.** The platform is the
 * component that knows which no this is, and `Docs/10` §4.6 writes those messages for a person to
 * read. A panel that substituted its own would drift from them silently, and would lose the field
 * errors the validation contract puts in the message for a mistyped status or an expired cursor.
 */
export async function answered<T>(upstream: Response): Promise<Answer<T>> {
  if (upstream.status === 401) return { state: "signed-out" };

  // Not JSON means something between here and the platform answered — a load balancer's HTML error
  // page is the usual one. Reading a result out of it would be reading something that is not the
  // contract, so it is an outage rather than a refusal.
  if (!isJSON(upstream)) return { state: "unavailable" };

  if (!upstream.ok) {
    let body: ErrorBody | null = null;
    try {
      body = (await upstream.json()) as ErrorBody;
    } catch {
      return { state: "unavailable" };
    }

    const code = typeof body?.error?.code === "string" ? body.error.code : "";
    const message = typeof body?.error?.message === "string" ? body.error.message : "";

    // A refusal outside the contract is not a refusal this panel can render. Saying "the platform
    // said no" with nothing to show for it is worse than saying it is not answering, because the
    // first invites somebody to fix their search and the second invites them to look at the
    // platform — which is where the fault actually is.
    if (code === "" || message === "") return { state: "unavailable" };

    return {
      state: "refused",
      refusal: {
        code,
        message,
        requestId: typeof body.error?.request_id === "string" ? body.error.request_id : "",
        details: fieldProblems(body.error?.details),
      },
    };
  }

  try {
    return { state: "ok", body: (await upstream.json()) as T };
  } catch {
    return { state: "unavailable" };
  }
}

/**
 * The `details` array, keeping only entries that are the shape the contract describes.
 *
 * Checked per entry rather than trusted, because this is rendered: a `details` that is not an array,
 * or an entry missing its message, would throw during a server render and turn a refusal somebody
 * could have acted on into an error page that says nothing at all. Dropping a malformed entry leaves
 * the top-level message, which is still the platform's own words.
 */
function fieldProblems(raw: unknown): FieldProblem[] {
  if (!Array.isArray(raw)) return [];

  const out: FieldProblem[] = [];
  for (const entry of raw) {
    if (typeof entry !== "object" || entry === null) continue;
    const { field, code, message } = entry as Record<string, unknown>;
    if (typeof message !== "string" || message === "") continue;

    out.push({
      field: typeof field === "string" ? field : "",
      code: typeof code === "string" ? code : "",
      message,
    });
  }
  return out;
}

/**
 * One page of a collection, in `internal/pagination`'s envelope (`Docs/10` §4.5).
 *
 * `next_cursor` is omitted rather than emptied when there is no further page — `omitempty` on the
 * Go struct — so it is optional here for the same reason.
 */
export interface Page<T> {
  data: T[];
  next_cursor?: string;
  has_more: boolean;
}
