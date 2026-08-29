import assert from "node:assert/strict";
import { register } from "node:module";
import { test } from "node:test";

/**
 * **What the panel actually puts on the wire when somebody signs in and out** (SHIP-188a).
 *
 * `surface.test.ts` reads source, and that kind of guard is one this repository has been bitten by
 * twice — `Docs/11` §7a records a budget guard satisfied by a rename, and §9 the client-side one
 * with the same blind spot. So this file runs the real route handlers and records the requests they
 * issue and the responses they return. Only the platform is a stand-in.
 *
 * The three properties it exists to hold, each of which a plausible refactor would break silently:
 *
 * - **the token is in the `Set-Cookie` header and nowhere else** — not in the body the browser
 *   parses, not in another header, not anywhere a script could reach;
 * - **the cookie is cleared only when the platform confirms the session is over**, so a failed
 *   sign-out leaves an administrator signed in rather than believing they are not; and
 * - **each handler reaches exactly one upstream path**, with the browser's idempotency key
 *   forwarded rather than replaced.
 */

/** Where the tests pretend the platform is. Read per call by `lib/upstream.ts`. */
const PLATFORM = "https://platform.example";
process.env.SHIPPER_API_BASE_URL = PLATFORM;

/** This origin, as a browser would state it. */
const PANEL = "https://admin.example.com";

/** The `@/` alias, so the route handlers can be imported at all. See `alias-hooks.mts`. */
register("./alias-hooks.mts", import.meta.url);

const { POST } = await import("../app/api/admin/sessions/route.ts");
const { DELETE } = await import("../app/api/admin/sessions/current/route.ts");

/** One request the panel made to the platform. */
interface Sent {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string | null;
}

/** Run one handler with the platform answering as given, and keep what went out. */
async function withPlatform(
  answer: (sent: Sent) => Response,
  act: () => Promise<Response>,
): Promise<{ response: Response; wire: Sent[] }> {
  const wire: Sent[] = [];
  const real = globalThis.fetch;

  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers: Record<string, string> = {};
    new Headers(init?.headers).forEach((value, name) => {
      headers[name.toLowerCase()] = value;
    });

    const sent: Sent = {
      url: String(input),
      method: init?.method ?? "GET",
      headers,
      body: typeof init?.body === "string" ? init.body : null,
    };
    wire.push(sent);
    return answer(sent);
  }) as typeof globalThis.fetch;

  try {
    return { response: await act(), wire };
  } finally {
    globalThis.fetch = real;
  }
}

/** A sign-in as the browser sends it. */
function signInRequest(
  body: unknown,
  { origin = PANEL, host = "admin.example.com", key = "11111111-2222-4333-8444-555555555555" } = {},
): Request {
  const headers = new Headers({ "content-type": "application/json", host });
  if (origin !== "") headers.set("origin", origin);
  if (key !== "") headers.set("idempotency-key", key);

  return new Request(`${PANEL}/api/admin/sessions`, {
    method: "POST",
    headers,
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
}

/** A sign-out as the browser sends it. */
function signOutRequest({
  cookie = "shipper_admin_session=a-live-token",
  origin = PANEL,
  host = "admin.example.com",
  key = "99999999-2222-4333-8444-555555555555",
} = {}): Request {
  const headers = new Headers({ host });
  if (cookie !== "") headers.set("cookie", cookie);
  if (origin !== "") headers.set("origin", origin);
  if (key !== "") headers.set("idempotency-key", key);

  return new Request(`${PANEL}/api/admin/sessions/current`, { method: "DELETE", headers });
}

const ISSUED = {
  token: "the-administrator-session-token",
  session: { id: "5f1f4e2a-0000-4000-8000-000000000001", expires_at: "2099-01-01T00:00:00Z" },
  administrator: {
    id: "5f1f4e2a-0000-4000-8000-000000000002",
    email: "moderator@shipper.example",
    name: "Jo Moderator",
    role: "support",
    permissions: ["moderation.read"],
    created_at: "2026-01-01T00:00:00Z",
  },
};

const issues = () => Response.json(ISSUED);

// --- sign-in ------------------------------------------------------------------------------------

test("sign-in reaches one upstream path, with the browser's key and only the two fields", async () => {
  const { response, wire } = await withPlatform(issues, () =>
    POST(signInRequest({ email: "moderator@shipper.example", password: "correct horse" })),
  );

  assert.equal(response.status, 200);
  assert.equal(wire.length, 1);
  assert.equal(wire[0].url, `${PLATFORM}/v1/admin/sessions`);
  assert.equal(wire[0].method, "POST");
  assert.equal(wire[0].headers["idempotency-key"], "11111111-2222-4333-8444-555555555555");
  assert.equal(
    wire[0].body,
    JSON.stringify({ email: "moderator@shipper.example", password: "correct horse" }),
  );
});

/**
 * The body is rebuilt rather than forwarded, so nothing else the browser sent reaches the platform.
 *
 * `httpx.DecodeJSON` would refuse an unknown field anyway. This makes the panel incapable of asking
 * rather than reliant on being told no — which is the difference that survives somebody relaxing
 * `additionalProperties` on the far side.
 */
test("a field the panel does not know about is not forwarded", async () => {
  const { wire } = await withPlatform(issues, () =>
    POST(signInRequest({ email: "a@b.example", password: "p", role: "owner", scope: "*" })),
  );

  assert.equal(wire[0].body, JSON.stringify({ email: "a@b.example", password: "p" }));
});

/**
 * **The assertion the whole ticket turns on.** The credential is in the `Set-Cookie` header and
 * nowhere else the browser can reach.
 */
test("the token reaches the browser only as an httpOnly cookie", async () => {
  const { response } = await withPlatform(issues, () =>
    POST(signInRequest({ email: "a@b.example", password: "p" })),
  );

  const body = await response.text();
  assert.ok(!body.includes(ISSUED.token), `the token was in the response body: ${body}`);
  assert.ok(body.includes("Jo Moderator"), "the administrator was not handed back");

  const cookie = response.headers.get("set-cookie") ?? "";
  assert.ok(cookie.includes(ISSUED.token), "the cookie does not carry the session");
  assert.match(cookie, /HttpOnly/i);
  assert.match(cookie, /SameSite=Strict/i);
  assert.match(cookie, /Secure/i);

  for (const [name, value] of response.headers) {
    if (name.toLowerCase() === "set-cookie") continue;
    assert.ok(!value.includes(ISSUED.token), `the token leaked into ${name}`);
  }
});

/** The `Host` of the request decides `Secure`, which is what lets `make web-dev` work at all. */
test("a local host gets the cookie without Secure", async () => {
  const { response } = await withPlatform(issues, () =>
    POST(
      signInRequest(
        { email: "a@b.example", password: "p" },
        { origin: "http://localhost:3001", host: "localhost:3001" },
      ),
    ),
  );

  const cookie = response.headers.get("set-cookie") ?? "";
  assert.match(cookie, /HttpOnly/i);
  assert.ok(!/Secure/i.test(cookie), cookie);
});

test("a refusal is forwarded whole, with no cookie installed", async () => {
  const refused = () =>
    Response.json(
      { error: { code: "invalid_credentials", message: "That email address and password do not match." } },
      { status: 400 },
    );

  const { response } = await withPlatform(refused, () =>
    POST(signInRequest({ email: "a@b.example", password: "wrong" })),
  );

  assert.equal(response.status, 400);
  assert.equal(response.headers.get("set-cookie"), null);
  assert.match(await response.text(), /invalid_credentials/);
});

/** A throttled administrator is told when to come back, or gives up and hammers the endpoint. */
test("Retry-After survives the hop", async () => {
  const throttled = () =>
    Response.json({ error: { code: "rate_limited", message: "Too many attempts." } }, {
      status: 429,
      headers: { "Retry-After": "60" },
    });

  const { response } = await withPlatform(throttled, () =>
    POST(signInRequest({ email: "a@b.example", password: "p" })),
  );

  assert.equal(response.status, 429);
  assert.equal(response.headers.get("Retry-After"), "60");
});

/**
 * Anything that is not the contract is an outage, not a sign-in.
 *
 * A load balancer's HTML error page is the usual one, and reading a credential out of it — or
 * forwarding it under a JSON content type — would both be wrong for the same reason.
 */
test("an answer outside the contract installs nothing", async () => {
  const gateway = () => new Response("<html>502</html>", { status: 502, headers: { "content-type": "text/html" } });

  const { response } = await withPlatform(gateway, () =>
    POST(signInRequest({ email: "a@b.example", password: "p" })),
  );

  assert.equal(response.status, 503);
  assert.equal(response.headers.get("set-cookie"), null);
});

test("a request from another site is refused before anything is sent", async () => {
  const { response, wire } = await withPlatform(issues, () =>
    POST(signInRequest({ email: "a@b.example", password: "p" }, { origin: "https://evil.example" })),
  );

  assert.equal(response.status, 403);
  assert.deepEqual(wire, []);
});

test("a request with no Origin at all is refused before anything is sent", async () => {
  const { response, wire } = await withPlatform(issues, () =>
    POST(signInRequest({ email: "a@b.example", password: "p" }, { origin: "" })),
  );

  assert.equal(response.status, 403);
  assert.deepEqual(wire, []);
});

test("a body the panel cannot read is refused before anything is sent", async () => {
  for (const body of ["not json", JSON.stringify({ email: 1, password: "p" }), JSON.stringify({})]) {
    const { response, wire } = await withPlatform(issues, () => POST(signInRequest(body)));
    assert.equal(response.status, 400, body);
    assert.deepEqual(wire, [], body);
  }
});

// --- sign-out -----------------------------------------------------------------------------------

const ended = () => new Response(null, { status: 204 });

test("sign-out spends the cookie's token against one upstream path", async () => {
  const { response, wire } = await withPlatform(ended, () => DELETE(signOutRequest()));

  assert.equal(response.status, 204);
  assert.equal(wire.length, 1);
  assert.equal(wire[0].url, `${PLATFORM}/v1/admin/sessions/current`);
  assert.equal(wire[0].method, "DELETE");
  assert.equal(wire[0].headers.authorization, "Bearer a-live-token"); // spelling:ok — RFC 9110
  assert.equal(wire[0].headers["idempotency-key"], "99999999-2222-4333-8444-555555555555");

  assert.match(response.headers.get("set-cookie") ?? "", /Max-Age=0/i);
});

/**
 * A credential the platform has already refused is gone, so the browser's copy goes too.
 *
 * This is the retry path: the first sign-out succeeded, the answer was lost, and the second attempt
 * carries a fresh key — so it reaches the guard rather than the middleware's replay, and is refused.
 * An administrator who clicked twice must not be told the second click failed.
 */
test("a credential the platform refuses is cleared and reported as signed out", async () => {
  const refused = () =>
    Response.json({ error: { code: "unauthenticated", message: "no" } }, { status: 401 });

  const { response } = await withPlatform(refused, () => DELETE(signOutRequest()));

  assert.equal(response.status, 204);
  assert.match(response.headers.get("set-cookie") ?? "", /Max-Age=0/i);
});

/**
 * **The one that reads backwards until you think about it.** When the platform did not confirm the
 * session ended, the cookie stays.
 *
 * Dropping it would make an administrator believe they had signed out of a session that is still
 * live — and which they can now no longer end from this browser at all. See the handler's file note.
 */
test("a session the platform did not end keeps its cookie, and the refusal is forwarded", async () => {
  const broke = () =>
    Response.json({ error: { code: "internal", message: "Something went wrong at our end." } }, { status: 500 });

  const { response } = await withPlatform(broke, () => DELETE(signOutRequest()));

  assert.equal(response.status, 500);
  assert.equal(response.headers.get("set-cookie"), null, "a live session lost its only credential");
});

test("an unreachable platform keeps the cookie", async () => {
  const { response } = await withPlatform(
    () => {
      throw new Error("no route to host");
    },
    () => DELETE(signOutRequest()),
  );

  assert.equal(response.status, 503);
  assert.equal(response.headers.get("set-cookie"), null);
});

/** Nothing to end. The request has been satisfied, and no credential is sent to say so. */
test("signing out with no session asks the platform nothing", async () => {
  const { response, wire } = await withPlatform(ended, () => DELETE(signOutRequest({ cookie: "" })));

  assert.equal(response.status, 204);
  assert.deepEqual(wire, []);
  assert.match(response.headers.get("set-cookie") ?? "", /Max-Age=0/i);
});

/**
 * The route a bare cross-site form would aim at, refused twice over.
 *
 * `SameSite=Strict` means the browser does not attach the cookie to such a request in the first
 * place. This is the second lock, and it is the one that still holds when the first is not honoured.
 */
test("another site cannot sign an administrator out", async () => {
  const { response, wire } = await withPlatform(ended, () =>
    DELETE(signOutRequest({ origin: "https://evil.example" })),
  );

  assert.equal(response.status, 403);
  assert.deepEqual(wire, []);
  assert.equal(response.headers.get("set-cookie"), null);
});
