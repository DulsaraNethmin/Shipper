import assert from "node:assert/strict";
import { test } from "node:test";

import { isSameOrigin } from "./origin.ts";
import {
  SESSION_COOKIE,
  clearedSessionCookie,
  secureFor,
  sessionCookie,
  sessionTokenFrom,
} from "./session.ts";

/**
 * The attributes of the string that actually reaches the browser (SHIP-188a).
 *
 * These functions are pure so that this file can assert on the `Set-Cookie` value itself rather
 * than on code that intends to set one. That distinction is the whole reason `lib/session.ts` takes
 * `now` and the `Host` header as arguments instead of reading a clock and `next/headers`: a test
 * that could only assert "the handler calls `cookies().set` with `httpOnly: true`" is a test of the
 * call, and the thing that protects the credential is the header.
 */

/** The attributes of one `Set-Cookie` value, lower-cased and split. */
function attributesOf(header: string): string[] {
  return header.split(";").slice(1).map((part) => part.trim().toLowerCase());
}

const NOW = new Date("2026-08-29T00:00:00.000Z");

test("the session cookie is unreadable, same-site and scoped to the whole panel", () => {
  const header = sessionCookie({
    token: "a-session-token",
    expiresAt: "2026-08-29T12:00:00.000Z",
    host: "admin.example.com",
    now: NOW,
  });

  assert.match(header, new RegExp(`^${SESSION_COOKIE}=a-session-token;`));

  const attributes = attributesOf(header);
  assert.ok(attributes.includes("httponly"), "the cookie is readable by script");
  assert.ok(attributes.includes("samesite=strict"), "the cookie travels on cross-site requests");
  assert.ok(attributes.includes("secure"), "the cookie may be sent in clear");
  assert.ok(attributes.includes("path=/"), "the cookie is not scoped to the whole panel");
});

/**
 * The lifetime is the platform's, not a number chosen here.
 *
 * Twelve hours ahead of `NOW` is 43200 seconds, and the arithmetic is asserted rather than the
 * presence of a `Max-Age`: a cookie that outlived the session would leave the panel presenting a
 * dead credential and rendering 401s it could not explain.
 */
test("the cookie expires when the platform says the session does", () => {
  const header = sessionCookie({
    token: "t",
    expiresAt: "2026-08-29T12:00:00.000Z",
    host: "admin.example.com",
    now: NOW,
  });

  assert.ok(attributesOf(header).includes("max-age=43200"), header);
});

/**
 * An expiry that is already past, or is not a date at all, becomes a cookie for this tab.
 *
 * `Max-Age=0` on a credential the platform has just issued installs and immediately destroys it,
 * which reads in a capture exactly like a sign-out and would send an administrator round the
 * sign-in loop for ever. The platform is the component that knows whether a credential works, so
 * the browser holds it and lets the platform refuse it.
 */
test("a spent or unreadable expiry leaves the cookie to the tab rather than deleting it", () => {
  for (const expiresAt of ["2026-08-28T00:00:00.000Z", "not-a-timestamp", ""]) {
    const attributes = attributesOf(
      sessionCookie({ token: "t", expiresAt, host: "admin.example.com", now: NOW }),
    );
    assert.ok(
      !attributes.some((attribute) => attribute.startsWith("max-age")),
      `${expiresAt} produced a lifetime`,
    );
    assert.ok(attributes.includes("httponly"), `${expiresAt} dropped httpOnly`);
  }
});

test("the token is encoded, so a value with a delimiter in it cannot forge an attribute", () => {
  const header = sessionCookie({
    token: "tok; Domain=evil.example",
    expiresAt: "2026-08-29T12:00:00.000Z",
    host: "admin.example.com",
    now: NOW,
  });

  assert.ok(!attributesOf(header).some((a) => a.startsWith("domain=")), header);
  assert.equal(sessionTokenFrom(header.split(";")[0]), "tok; Domain=evil.example");
});

/**
 * `Secure` fails closed.
 *
 * An unknown host gets it, so the only way to serve this credential over plain HTTP is to be
 * recognisably local. A deployment cannot lose the attribute by forgetting to set something.
 */
test("Secure is set everywhere except a recognisably local host", () => {
  for (const host of ["admin.example.com", "admin.example.com:8443", "192.0.2.10", null, ""]) {
    assert.equal(secureFor(host), true, `${String(host)} was served without Secure`);
  }
  for (const host of ["localhost", "localhost:3001", "127.0.0.1:3001", "[::1]:3001", "panel.localhost"]) {
    assert.equal(secureFor(host), false, `${host} demanded Secure and cannot serve one`);
  }
});

/**
 * The deletion repeats every attribute except the lifetime.
 *
 * A browser matches a deletion to an existing cookie by name, domain and path. A `Set-Cookie` that
 * omitted `Path=/` would be scoped to the path of the request that sent it and would leave the real
 * cookie in place — a sign-out that appears to work and does not.
 */
test("clearing the cookie matches the cookie it is clearing", () => {
  const attributes = attributesOf(clearedSessionCookie("admin.example.com"));

  assert.ok(attributes.includes("path=/"), "a deletion scoped to the wrong path deletes nothing");
  assert.ok(attributes.includes("httponly"));
  assert.ok(attributes.includes("samesite=strict"));
  assert.ok(attributes.includes("secure"));
  assert.ok(attributes.includes("max-age=0"));
  assert.ok(attributes.some((a) => a.startsWith("expires=")));
});

test("the cookie parser matches the name exactly and decodes the value", () => {
  assert.equal(sessionTokenFrom(null), null);
  assert.equal(sessionTokenFrom(""), null);
  assert.equal(sessionTokenFrom("other=1"), null);
  assert.equal(sessionTokenFrom(`${SESSION_COOKIE}=`), null);

  // A prefix match would read this as the session and send a value that is not one upstream.
  assert.equal(sessionTokenFrom(`${SESSION_COOKIE}_backup=stale`), null);

  assert.equal(sessionTokenFrom(`a=1; ${SESSION_COOKIE}=tok; b=2`), "tok");
  assert.equal(sessionTokenFrom(`${SESSION_COOKIE}=a%20b`), "a b");
});

/**
 * The origin check, which is the second lock on the door `SameSite=Strict` closes first.
 *
 * An absent `Origin` is refused. Every caller of these route handlers is a browser and every browser
 * sends the header; what does not send it is something that is not a browser, which is either a
 * mistake or somebody probing.
 */
test("a request is same-origin only when the browser says so and the hosts agree", () => {
  const from = (headers: Record<string, string>) =>
    isSameOrigin(new Request("http://localhost:3001/api/admin/sessions", { headers }));

  assert.equal(from({ origin: "http://localhost:3001", host: "localhost:3001" }), true);
  assert.equal(from({ origin: "https://ADMIN.example.com", host: "admin.example.com" }), true);

  assert.equal(from({ host: "localhost:3001" }), false, "an absent Origin was allowed");
  assert.equal(from({ origin: "http://localhost:3001" }), false, "an absent Host was allowed");
  assert.equal(from({ origin: "null", host: "localhost:3001" }), false, "an opaque origin was allowed");
  assert.equal(
    from({ origin: "http://evil.example", host: "localhost:3001" }),
    false,
    "another site was allowed",
  );

  // A different port is a different origin, which is the case a developer running two surfaces
  // locally will hit first — and it is right that it is refused.
  assert.equal(from({ origin: "http://localhost:3002", host: "localhost:3001" }), false);

  // X-Forwarded-Host is written by whatever is in front of this application, and by the caller when
  // nothing is. Trusting it would make the comparison forgeable by the party being checked.
  assert.equal(
    from({
      origin: "http://evil.example",
      host: "localhost:3001",
      "x-forwarded-host": "evil.example",
    }),
    false,
  );
});
