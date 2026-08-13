import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

/**
 * The guard: **the driver's token goes to one endpoint, and adding a second place it could go is a
 * failing test rather than a review comment** (SHIP-120).
 *
 * The invariant is `CLAUDE.md`'s — the driver's job-scoped token and the mobile auth token are
 * separate systems and neither can be exchanged for the other. The platform holds up its half by
 * construction: two keysets, two audiences, and a driver token that carries no `sub` for a session
 * to be built from (SHIP-107). This portal's half is smaller and easier to lose: it holds a live
 * credential in a browser, and every place that credential is *sent* is a place it could be sent
 * somewhere it does not belong.
 *
 * So the set of files that may name a credential or make a request is closed, and the test names
 * the offending file when it grows. Two shapes it is written to catch:
 *
 * - **A second call site.** A page that fetched `/v1/jobs/{id}` with the driver's token to fill in
 *   the pickup address would be refused by the platform — that route is `RequireUser` — but the
 *   token would have been sent to it, and the next endpoint someone tried might not be.
 * - **A sign-in.** Anything reaching an identity endpoint from here is the exchange the invariant
 *   forbids, in the one direction a client could attempt it.
 *
 * # It reads code and not comments, because wave 5 paid for the other kind
 *
 * `Docs/11` §7a records a guard that matched a spelling and was satisfied by a rename. This one is
 * narrower than that in one useful way: it does not search for a forbidden word, it holds a set of
 * *files* to a list, so a new file that fetches anything at all fails it whatever the fetch is
 * called. Block comments and whole-line comments are stripped first — every doc comment in this
 * application discusses the credential header, `localStorage` and `/v1/` at length, and a guard that
 * counted those would have been failing from the moment it was written.
 */

const HERE = path.dirname(fileURLToPath(import.meta.url));
const PORTAL = path.join(HERE, "..");

/** The one place a request is made from the browser, and the one place it is made from the server. */
const MAY_REQUEST = ["lib/delivery.ts", "app/api/driver/jobs/[jobId]/route.ts"];

/** The one place a credential is held between arriving and being spent. */
const MAY_HOLD = ["lib/link.ts"];

function sources(): Map<string, string> {
  const found = new Map<string, string>();

  const walk = (dir: string) => {
    for (const entry of readdirSync(path.join(PORTAL, dir), { withFileTypes: true })) {
      const relative = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(relative);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name) || entry.name.endsWith(".test.ts")) continue;
      found.set(relative, strip(readFileSync(path.join(PORTAL, relative), "utf8")));
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

test("only the delivery read and its route handler make a request", () => {
  assert.deepEqual(filesContaining(/\bfetch\s*\(/), [...MAY_REQUEST].sort());
});

test("only those two name a credential", () => {
  assert.deepEqual(filesContaining(/authorization|bearer/i), [...MAY_REQUEST].sort()); // spelling:ok — RFC 9110
});

/**
 * The platform's own path appears once, in the route handler, spelled out in full. A second `/v1/`
 * anywhere in this application is a second endpoint somebody taught the portal to reach.
 */
test("the platform is addressed at exactly one path", () => {
  assert.deepEqual(filesContaining(/\/v1\//), ["app/api/driver/jobs/[jobId]/route.ts"]);

  const handler = sources().get("app/api/driver/jobs/[jobId]/route.ts") ?? "";
  const paths = [...handler.matchAll(/\/v1\/[A-Za-z0-9_\-/{}$]*/g)].map((m) => m[0]);
  assert.deepEqual(paths, ["/v1/driver/jobs/${jobId}"]);
});

/**
 * Nothing signs in from here. There is no account, and asking for one is the forbidden exchange.
 *
 * It looks for identity's *endpoints and credentials* rather than for the words "sign in", and the
 * first draft of this test proves why: it failed on the copy that reads "there is no account to
 * sign in to", which is the page saying the right thing. A guard that a page fails by explaining
 * the invariant is testing the wrong thing.
 */
test("no identity endpoint or session credential is named anywhere in the portal", () => {
  assert.deepEqual(filesContaining(/\/auth\/|refresh_token|access_token|Idempotency-Key/i), []);
});

/**
 * A seven-day credential that outlives the tab is a liability on a phone that gets handed around.
 * `sessionStorage` in one file; `localStorage` and cookies in none.
 */
test("the credential is held in one place, and not beyond the tab", () => {
  assert.deepEqual(filesContaining(/sessionStorage/), [...MAY_HOLD].sort());
  assert.deepEqual(filesContaining(/localStorage|document\.cookie|\bcookies\s*\(/), []);
});
