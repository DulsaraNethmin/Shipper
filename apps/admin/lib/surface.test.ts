import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

/**
 * The guard: **an administrator's session token is never reachable from JavaScript, and putting it
 * within reach is a failing test rather than a review comment** (SHIP-188a).
 *
 * # Why this panel gets a different guard from the driver portal's
 *
 * `apps/driver-portal/lib/surface.test.ts` asserts that `localStorage`, `document.cookie` and
 * `cookies()` appear in no file, and holds the driver's token to `sessionStorage` in exactly one.
 * Copying that here would have been the obvious move and would have been wrong in both directions:
 * this panel *must* use a cookie, and it must *not* use any browser storage at all. The asset is
 * different, so the answer is:
 *
 * - a driver's token grants **one delivery**, is already in the URL they were sent, and the worst a
 *   stolen one does is show a stranger one job's milestones;
 * - an administrator's session reaches **every user's contact details, every job, the verification
 *   evidence and the append-only audit trail**, and it is the most valuable credential the product
 *   issues.
 *
 * A credential JavaScript can read is a credential any injected script can read. So the token lives
 * in an httpOnly cookie set by a route handler, and the tests below hold three properties that
 * together mean no line of client code can reach it:
 *
 * 1. **no browser storage anywhere**, so there is no second place it could be put;
 * 2. **the cookie is named only in server files**, so no bundle contains the name to look for; and
 * 3. **no `"use client"` file names it or imports `next/headers`**, which is the case a build would
 *    only catch for the second half.
 *
 * # It reads code and not comments, because wave 5 paid for the other kind
 *
 * `Docs/11` §7a records a guard that matched a spelling and was satisfied by a rename. This one
 * holds a set of *files* to a list, so a new file that fetches anything at all fails it whatever the
 * fetch is called. Block comments and whole-line comments are stripped first — every doc comment in
 * this application discusses the credential, the cookie and `/v1/` at length, and a guard that
 * counted those would have been failing from the moment it was written.
 */

const HERE = path.dirname(fileURLToPath(import.meta.url));
const PANEL = path.join(HERE, "..");

/**
 * Everything that may make a request, at either hop.
 *
 * **One file per upstream endpoint, and that is the mechanism rather than a filing convention.**
 * The property each route handler claims — "this can only ever reach one endpoint" — survives the
 * panel growing only because each names one path as a literal template. A shared `forward(path, …)`
 * in `lib/` would be the tidy version and would be a `rewrites()` entry with more steps: one
 * `fetch` whose destination is an argument is a general, credential-forwarding front door however
 * narrow its callers are today. Here that door opens onto twenty-four privileged endpoints.
 */
const MAY_REQUEST = [
  "components/sign-in-form.tsx",
  "components/sign-out-button.tsx",
  "lib/administrator.ts",
  "app/api/admin/sessions/route.ts",
  "app/api/admin/sessions/current/route.ts",
];

/**
 * The files that may name a platform endpoint, and the one path each names.
 *
 * The driver portal's rule is "only a route handler may name one". This panel's is one file per
 * endpoint, because `lib/administrator.ts` resolves the session during a server render and adding a
 * route handler for it would be creating a browser-reachable endpoint for nobody to call. It cannot
 * become a client module: it imports `next/headers`, which Next refuses to bundle into one.
 *
 * **The count below is the count of endpoints this origin can reach**, and it is the number to read
 * at review. It moves when somebody teaches the panel a new destination, which is exactly when
 * somebody should look.
 */
const ENDPOINTS: Record<string, string[]> = {
  "lib/administrator.ts": ["/v1/admin/me"],
  "app/api/admin/sessions/route.ts": ["/v1/admin/sessions"],
  "app/api/admin/sessions/current/route.ts": ["/v1/admin/sessions/current"],
};

/**
 * The files that may name the session cookie.
 *
 * All four run on the server. That is the assertion: the cookie's name appears in no module that
 * can be sent to a browser, so there is nothing in a bundle for an injected script to look for —
 * which is a smaller claim than "httpOnly stops it being read" and is true for a different reason,
 * so the two do not fail together.
 */
const MAY_NAME_THE_COOKIE = [
  "lib/session.ts",
  "lib/administrator.ts",
  "app/api/admin/sessions/route.ts",
  "app/api/admin/sessions/current/route.ts",
];

/** The files that may hold a bearer credential on its way to the platform. */
const MAY_SPEND_THE_CREDENTIAL = [
  "lib/administrator.ts",
  "app/api/admin/sessions/current/route.ts",
];

function sources(): Map<string, string> {
  const found = new Map<string, string>();

  const walk = (dir: string) => {
    for (const entry of readdirSync(path.join(PANEL, dir), { withFileTypes: true })) {
      const relative = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(relative);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name) || entry.name.endsWith(".test.ts")) continue;
      found.set(relative, strip(readFileSync(path.join(PANEL, relative), "utf8")));
    }
  };

  for (const dir of ["app", "components", "lib"]) walk(dir);
  return found;
}

/** Every file as written, comments included — for the one test that has to see a directive. */
function raw(): Map<string, string> {
  const found = new Map<string, string>();

  const walk = (dir: string) => {
    for (const entry of readdirSync(path.join(PANEL, dir), { withFileTypes: true })) {
      const relative = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(relative);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name) || entry.name.endsWith(".test.ts")) continue;
      found.set(relative, readFileSync(path.join(PANEL, relative), "utf8"));
    }
  };

  for (const dir of ["app", "components", "lib"]) walk(dir);
  return found;
}

/**
 * Code with its comments removed.
 *
 * Whole-line `//` comments only, deliberately: a naive line-comment strip would cut
 * `"http://localhost:8080"` in half, and a guard that mangles the code it inspects is one that
 * fails for reasons nobody can read.
 */
function strip(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((line) => !/^\s*\/\//.test(line))
    .join("\n");
}

function filesContaining(pattern: RegExp): string[] {
  return [...sources()]
    .filter(([, code]) => pattern.test(code))
    .map(([file]) => file)
    .sort();
}

/** The modules Next will send to a browser: the `"use client"` boundary and everything under it. */
function clientModules(): string[] {
  return [...raw()]
    .filter(([, source]) => /^\s*["']use client["']\s*;?\s*$/m.test(source))
    .map(([file]) => file)
    .sort();
}

/**
 * **The central assertion, and the one the ticket is named for.**
 *
 * No browser storage of any kind. Not `localStorage`, which would leave a credential on a shared
 * machine until something deleted it; not `sessionStorage`, which is the driver portal's answer to
 * a much smaller asset; and not `document.cookie`, which is what an httpOnly cookie exists to keep
 * empty and which nothing here has any reason to read.
 *
 * It is deliberately a ban rather than an allow-list of one file. There is no correct place for an
 * administrator credential in browser storage, so the honest guard has no exceptions to argue with.
 */
test("nothing in this panel touches browser storage", () => {
  assert.deepEqual(filesContaining(/localStorage|sessionStorage|document\.cookie/), []);
});

/** Only the five named files make a request, at either hop. */
test("only the named files make a request", () => {
  assert.deepEqual(filesContaining(/\bfetch\s*\(/), [...MAY_REQUEST].sort());
});

/**
 * A platform path appears only where it is allowed to, and each of those names exactly one.
 *
 * The second half is what has teeth. A file that named two templates would be a proxy with a branch
 * in it, and one that built its path from anything but a literal would be a proxy with a parameter.
 * Both are the `next.config.ts` `rewrites()` entry this panel refused, with more steps, and neither
 * would be caught by counting files.
 */
test("only the server files name a platform path, and each names exactly one", () => {
  assert.deepEqual(filesContaining(/\/v1\//), Object.keys(ENDPOINTS).sort());

  const named = new Map<string, string[]>();
  const code = sources();
  for (const file of Object.keys(ENDPOINTS)) {
    const handler = code.get(file) ?? "";
    named.set(file, [...handler.matchAll(/\/v1\/[A-Za-z0-9_\-/{}$]*/g)].map((m) => m[0]));
  }

  assert.deepEqual(
    Object.fromEntries(named),
    ENDPOINTS,
    "a file names a number of upstream paths other than the one it is allowed",
  );
});

/** The cookie is named on the server and nowhere else. */
test("the session cookie is named only in server files", () => {
  assert.deepEqual(
    filesContaining(/SESSION_COOKIE|shipper_admin_session|set-cookie/i),
    [...MAY_NAME_THE_COOKIE].sort(),
  );
});

/**
 * Nothing that reaches the browser can name the credential, read a cookie, or hold a bearer token.
 *
 * **This is the test that would fail on the mistake somebody will actually make**: rendering the
 * signed-in administrator from the sign-in response instead of from the next server render, and
 * reaching for the token to do it. The build catches half of it — `next/headers` in a client module
 * is a build error — and catches none of the other half, because a string constant compiles
 * perfectly.
 */
test("no client module names the cookie, reads one, or holds a credential", () => {
  const code = sources();
  const offending = clientModules().filter((file) => {
    const source = code.get(file) ?? "";
    return /SESSION_COOKIE|shipper_admin_session|next\/headers|\bcookies\s*\(|authorization|bearer/i
      .test(source);
  });

  assert.deepEqual(offending, [], "a client module can see the administrator's credential");
});

/** A bearer credential is put on a request in two places, and both are server files. */
test("only the server files spend the credential", () => {
  assert.deepEqual(
    filesContaining(/authorization|bearer/i),
    [...MAY_SPEND_THE_CREDENTIAL].sort(),
  );
});

/**
 * The idempotency key is minted where the browser can decide it is a new action, and nowhere else.
 *
 * `CLAUDE.md` makes an idempotency key mandatory on every state-changing endpoint, and the header is
 * only correct when the *same* value survives a retry of the *same* action — so a key generated at
 * a hop that is not the browser's changes on every attempt, which is the header doing the opposite
 * of its job. SHIP-188d's decision write inherits this: a double-click must record one decision.
 */
test("the idempotency key is minted in the browser and only forwarded after that", () => {
  assert.deepEqual(filesContaining(/randomUUID/), ["lib/keys.ts"]);
  assert.deepEqual(
    filesContaining(/idempotency[-_]?key/i),
    [
      "app/api/admin/sessions/current/route.ts",
      "app/api/admin/sessions/route.ts",
      "components/sign-in-form.tsx",
      "components/sign-out-button.tsx",
      "lib/keys.ts",
    ].sort(),
  );
});

/**
 * Every mutating route handler checks the origin.
 *
 * A cookie is attached by the browser to a request another site made, which is the cost
 * `lib/session.ts` accepts in exchange for a credential no script can read. `SameSite=Strict` is
 * the first lock and this is the second, and a route added later that forgot it would be the one
 * hole the whole arrangement has. So the check is asserted per file rather than trusted per author.
 */
test("every route handler checks that the request came from this panel", () => {
  const code = sources();
  const handlers = [...code.keys()].filter((file) => /^app\/api\/.*route\.tsx?$/.test(file)).sort();

  assert.deepEqual(handlers, [
    "app/api/admin/sessions/current/route.ts",
    "app/api/admin/sessions/route.ts",
  ]);

  for (const handler of handlers) {
    assert.match(code.get(handler) ?? "", /isSameOrigin\s*\(/, `${handler} does not check the origin`);
  }
});
