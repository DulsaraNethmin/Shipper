import { platformHeaders } from "@/lib/credential";
import { isJSON, platform } from "@/lib/upstream";

/**
 * Who is signed in, resolved on the server on every render (SHIP-188a).
 *
 * # It is a server module and cannot become anything else
 *
 * It imports `lib/credential.ts`, which imports `next/headers`, which Next refuses to bundle into a
 * client component — importing this file from a `"use client"` module is a build failure rather than
 * a review comment. That is the structural half of the guarantee `lib/session.ts` makes: the
 * credential cannot reach the browser's JavaScript because the only code that reads it cannot run
 * there. `lib/surface.test.ts` holds the other half, over the files a build would not notice.
 *
 * **The cookie is read by `lib/credential.ts` rather than here, since SHIP-188b.** This file was the
 * only server module that needed it when SHIP-188a wrote it, and inlining three lines was right for
 * one caller; five screens later the same three lines would be five files naming the cookie and five
 * holding a credential, which is an allow-list that grows with the panel and has stopped guarding
 * anything. What arrives here is a `Headers` — something to put on a request, and not a value this
 * file can log or render.
 *
 * # There is no route handler for this, deliberately
 *
 * Sign-in and sign-out need one because a form in a browser has to reach them. Nothing in the
 * browser needs to ask who is signed in — the server already knows before it renders, and a route
 * handler here would be a browser-reachable endpoint added for nobody. The panel's rule is
 * therefore one file per upstream endpoint rather than the driver portal's stricter "only a route
 * handler may name one", and `lib/surface.test.ts` asserts the count either way: this file names
 * exactly one platform path and it is the one below.
 *
 * # Every render costs one call, and that is the design rather than an oversight
 *
 * `internal/admin/adminauth.go` re-reads `admin_sessions` on every request precisely so that
 * revocation and a role change take effect at once. A panel that cached the answer would reintroduce
 * the staleness the platform pays an indexed read per request to avoid — an administrator whose
 * permissions were cut at 2am would go on being shown the screens until something evicted a cache.
 */

/** An administrator, exactly as `GET /v1/admin/me` serves them. */
export interface Administrator {
  id: string;
  email: string;
  name: string;
  role: string;

  /**
   * What the role holds, expanded by the platform (SHIP-148).
   *
   * **Read to hide and disable, never to decide.** `Docs/07` §3 is a rule about every client: the
   * client may hide, the platform rules. An administrator who edited this array in a response would
   * find every endpoint refusing them exactly as before, because the check reads the role out of
   * the session row on each request.
   */
  permissions: string[];

  created_at: string;
}

/** The session the browser is holding, as much of it as a client has any use for. */
export interface AdminSession {
  id: string;
  expires_at: string;
}

/**
 * The three answers, and the panel renders a different screen for each.
 *
 * **`unavailable` is separate from `signed-out` on purpose.** Collapsing them would send an
 * administrator to a sign-in form during an outage, where their correct password would fail for a
 * reason the form cannot express — and the panel would be telling them their credential is bad when
 * what is bad is the platform. That is the same distinction `Docs/09`'s SHIP-193 row insists on
 * between a vendor's "no results" and its "request denied", for the same reason: a refusal read as
 * an absence sends everybody looking in the wrong place.
 */
export type Viewer =
  | { state: "signed-in"; administrator: Administrator; session: AdminSession }
  | { state: "signed-out" }
  | { state: "unavailable" };

interface MeResponse {
  administrator: Administrator;
  session: AdminSession;
}

/**
 * Resolve the cookie this browser presented into an administrator, or say why not.
 *
 * The credential goes out in a header. A header reaches no access log; a request line does, which is
 * why nothing about the credential is ever in a path or a query string on any hop.
 */
export async function viewer(): Promise<Viewer> {
  const headers = await platformHeaders();
  if (headers === null) return { state: "signed-out" };

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/admin/me`, {
      method: "GET",
      headers,
      cache: "no-store",
    });
  } catch {
    return { state: "unavailable" };
  }

  // 401 is the platform saying this credential is not good: expired, signed out, or the account
  // disabled since it was issued. All three are "sign in again", which is the only thing an
  // administrator can do about any of them, so the panel does not distinguish what it cannot act on.
  if (upstream.status === 401) return { state: "signed-out" };
  if (!upstream.ok || !isJSON(upstream)) return { state: "unavailable" };

  let body: MeResponse;
  try {
    body = (await upstream.json()) as MeResponse;
  } catch {
    return { state: "unavailable" };
  }

  if (typeof body.administrator?.id !== "string") return { state: "unavailable" };

  return {
    state: "signed-in",
    administrator: body.administrator,
    session: body.session,
  };
}
