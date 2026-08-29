import { isSameOrigin } from "@/lib/origin";
import { clearedSessionCookie, sessionTokenFrom } from "@/lib/session";
import { forwardRefusal, isJSON, platform, refuse, unreachable } from "@/lib/upstream";

/**
 * Sign-out, forwarded to `DELETE /v1/admin/sessions/current` and to nothing else (SHIP-188a).
 *
 * A second route file rather than a second method on the first, for the reason the driver portal
 * gives and this panel needs more: **one file, one literal upstream template**. The property being
 * kept is per file — a handler naming two paths is a proxy with a branch in it — and here the
 * branch would be sitting between the browser and twenty-four privileged endpoints.
 *
 * # Clearing the cookie is not signing out, and conflating them is the mistake this file avoids
 *
 * Deleting the browser's copy of a credential makes it unusable *by this browser*. The session
 * itself is a row in `admin_sessions`, and it stays valid until something ends it — which is
 * exactly why `internal/admin/adminauth.go` reads that row on every request rather than trusting a
 * signature. So a "sign out" that only dropped the cookie would leave a live administrator session
 * behind and tell somebody it was gone.
 *
 * The cookie is therefore cleared **only when the platform confirms the session is over**:
 *
 *   - `204`, the session ended by this request; and
 *   - `401`, the credential was already dead — expired, signed out on another tab, or the account
 *     disabled. There is nothing left to end and nothing to keep.
 *
 * **On anything else the cookie stays and the refusal is forwarded.** That reads backwards for a
 * moment — surely dropping the credential is the cautious act? — and it is not: dropping it makes
 * an administrator believe they have signed out of a session that is still live and now cannot be
 * ended from this browser at all. Keeping it means they see the failure, and can try again. During
 * a platform outage nobody can sign out, which is true rather than hidden.
 */
export async function DELETE(request: Request): Promise<Response> {
  // The reason this check matters most on this route: a cookie is attached by the browser to a
  // request another site made, so a bare form on another origin would sign an administrator out.
  // See lib/origin.ts.
  if (!isSameOrigin(request)) {
    return refuse(403, "forbidden", "That request did not come from the panel.");
  }

  const token = sessionTokenFrom(request.headers.get("cookie"));

  // Nothing to end. Answering 204 rather than 401 is not politeness: the caller asked for this
  // browser to stop being signed in, and it is not signed in, so the request has been satisfied.
  // The clearing header goes out anyway, in case what is held is a cookie this parser did not
  // recognise as a session.
  if (token === null) {
    return new Response(null, {
      status: 204,
      headers: {
        "cache-control": "no-store",
        "set-cookie": clearedSessionCookie(request.headers.get("host")),
      },
    });
  }

  const headers = new Headers({
    Accept: "application/json",
    Authorization: `Bearer ${token}`, // spelling:ok — RFC 9110 header name
  });

  // Minted in the browser, forwarded here. A retry of a dropped sign-out reuses its key and the
  // middleware replays the 204 it already sent — from outside the guard, so the dead credential is
  // never consulted. `scripts/verify/90-admin.sh` demonstrates that path against the platform.
  const key = request.headers.get("idempotency-key");
  if (key !== null) headers.set("Idempotency-Key", key);

  let upstream: Response;
  try {
    upstream = await fetch(`${platform()}/v1/admin/sessions/current`, {
      method: "DELETE",
      headers,
      cache: "no-store",
    });
  } catch {
    return unreachable();
  }

  if (upstream.status === 204 || upstream.status === 401) {
    return new Response(null, {
      status: 204,
      headers: {
        "cache-control": "no-store",
        "set-cookie": clearedSessionCookie(request.headers.get("host")),
      },
    });
  }

  // The session is still live. See the file note: the credential stays so the administrator can end
  // it, and the platform's own words say why this attempt did not.
  if (!isJSON(upstream)) return unreachable();
  return forwardRefusal(upstream);
}
