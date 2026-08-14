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

/**
 * The one place a request is made from the browser, and the route handlers that make them from the
 * server.
 *
 * **One file per upstream endpoint, and that is the mechanism rather than a filing convention**
 * (SHIP-121). The portal grew from one outbound call to two, and the property SHIP-120 claimed —
 * "this route can only ever reach one endpoint" — survives that only because each handler names one
 * path as a literal template. A shared `forward(path, …)` in `lib/` would have been the tidy version
 * and would have been the `rewrites()` entry `route.ts` refused, with more steps: one `fetch` whose
 * destination is an argument is a general, credential-forwarding front door however narrow its
 * callers are today. So the count below is the count of endpoints this origin can reach, and the
 * `/v1/` test underneath turns that into an assertion.
 */
const MAY_REQUEST = [
  "lib/delivery.ts",
  "app/api/driver/jobs/[jobId]/route.ts",
  "app/api/driver/jobs/[jobId]/milestones/route.ts",
  "app/api/driver/jobs/[jobId]/proof-uploads/route.ts",
];

/** The route handlers, which are the only files that may name a platform endpoint. */
const MAY_NAME_AN_ENDPOINT = MAY_REQUEST.filter((file) => file.startsWith("app/"));

/**
 * The one place a credential is held between arriving and being spent, and the one place a
 * per-action idempotency key is.
 *
 * `lib/keys.ts` joined this list at SHIP-121 and it is deliberately a **second file** rather than
 * two more functions in `link.ts`. What it holds is not a credential: an idempotency key is a value
 * the client invents to name its own request, it authorises nothing, and its whole purpose is to be
 * sent again. Keeping the two in separate files is what lets the credential test below go on saying
 * that the token is named in exactly one place.
 */
const MAY_HOLD = ["lib/keys.ts", "lib/link.ts"];

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
 * A platform path appears only in a route handler, and each handler names exactly one.
 *
 * **The second half is the part that grew teeth at SHIP-121.** While there was one route file, "the
 * platform is addressed at exactly one path" was the whole statement. With more than one, the
 * statement that matters is per file: a handler that named two templates would be a proxy with a
 * branch in it, and a handler that built its path from anything but a literal would be a proxy with
 * a parameter. Both are the `rewrites()` entry `route.ts` refused, and neither would be caught by
 * counting files.
 *
 * The count of templates below is therefore the count of endpoints this origin can reach, and it is
 * the number that should be read at review: it went from one to two because SHIP-121 added a
 * milestone write, and any other movement in it is somebody teaching the portal a new destination.
 */
test("only the route handlers name a platform path, and each names exactly one", () => {
  assert.deepEqual(filesContaining(/\/v1\//), [...MAY_NAME_AN_ENDPOINT].sort());

  const named = new Map<string, string[]>();
  // The store's own URL is deliberately not in this map. `putPhotograph` fetches an absolute URL the
  // platform signed and this application never constructs — it holds no store hostname, no bucket
  // name and no path — so there is nothing here for a template to hold it to. What holds *that*
  // request is `lib/upload.test.ts`, which asserts it carries no credential.
  for (const file of MAY_NAME_AN_ENDPOINT) {
    const handler = sources().get(file) ?? "";
    named.set(file, [...handler.matchAll(/\/v1\/[A-Za-z0-9_\-/{}$]*/g)].map((m) => m[0]));
  }

  assert.deepEqual(
    Object.fromEntries(named),
    {
      "app/api/driver/jobs/[jobId]/route.ts": ["/v1/driver/jobs/${jobId}"],
      "app/api/driver/jobs/[jobId]/milestones/route.ts": ["/v1/driver/jobs/${jobId}/milestones"],
      "app/api/driver/jobs/[jobId]/proof-uploads/route.ts": ["/v1/driver/jobs/${jobId}/proof-uploads"],
    },
    "a route handler names a number of upstream paths other than one",
  );
});

/**
 * Nothing signs in from here. There is no account, and asking for one is the forbidden exchange.
 *
 * It looks for identity's *endpoints and credentials* rather than for the words "sign in", and the
 * first draft of this test proves why: it failed on the copy that reads "there is no account to
 * sign in to", which is the page saying the right thing. A guard that a page fails by explaining
 * the invariant is testing the wrong thing.
 *
 * **`Idempotency-Key` left this list at SHIP-121 and has a test of its own below.** It was here
 * because SHIP-120's portal made no state-changing request at all, so the header appearing anywhere
 * meant a write had been added without anybody deciding to add one. A write is now the ticket, and
 * leaving the header in a list of *identity* credentials would have made the next person delete the
 * whole assertion to get their build green.
 */
test("no identity endpoint or session credential is named anywhere in the portal", () => {
  assert.deepEqual(filesContaining(/\/auth\/|refresh_token|access_token/i), []);
});

/**
 * The idempotency key is sent where a request is made and minted where one is held, and nowhere else
 * (SHIP-121).
 *
 * `CLAUDE.md` makes an idempotency key mandatory on every state-changing endpoint, and the header is
 * only correct when the *same* value survives a retry — so a key generated at a hop that is not the
 * browser's is a key that changes on every attempt, which is the header doing the opposite of its
 * job. Holding the set of files that may name one to `lib/keys.ts` plus the request path is what
 * makes a second minting site a failing test rather than a milestone recorded twice in a yard.
 */
test("the idempotency key is named only where one is minted, held or sent", () => {
  // The read is deliberately absent. `GET /v1/driver/jobs/{id}` is not state-changing, the
  // middleware passes it straight through, and a portal that forwarded a key on it would be
  // claiming an action where there is only a look.
  assert.deepEqual(
    filesContaining(/idempotency[-_]?key/i),
    [
      "app/api/driver/jobs/[jobId]/milestones/route.ts",
      "app/api/driver/jobs/[jobId]/proof-uploads/route.ts",
      "lib/delivery.ts",
      "lib/keys.ts",
    ].sort(),
  );
  assert.deepEqual(filesContaining(/randomUUID/), ["lib/keys.ts"]);
});

/**
 * A seven-day credential that outlives the tab is a liability on a phone that gets handed around.
 * `sessionStorage` in one file; `localStorage` and cookies in none.
 */
test("the credential is held in one place, and not beyond the tab", () => {
  assert.deepEqual(filesContaining(/sessionStorage/), [...MAY_HOLD].sort());
  assert.deepEqual(filesContaining(/localStorage|document\.cookie|\bcookies\s*\(/), []);
});
