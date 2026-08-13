import assert from "node:assert/strict";
import { test } from "node:test";

import { isJobId, storageKey, tokenFromFragment } from "./link.ts";

/**
 * The link, taken apart (SHIP-120).
 *
 * Run with `pnpm --filter ./apps/driver-portal test`, which is `node --test lib` and needs nothing
 * installed: Node 22 strips the types itself. That is why there is no test framework in this
 * application's dependencies — see the README for what CI does and does not yet run.
 */

test("a job identifier is a canonical UUID and nothing else", () => {
  assert.equal(isJobId("6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d"), true);
  assert.equal(isJobId("6F2A1F3C-9D4E-4B1A-8C7D-2E5F0A9B1C3D"), true);
});

/**
 * The interesting half. A path segment reaches the route handler already URL-decoded, so anything
 * refused here is something that would otherwise have been interpolated into an upstream URL —
 * which is what would turn one endpoint into a general proxy.
 */
test("nothing that could steer a URL is a job identifier", () => {
  for (const attempt of [
    "",
    "..",
    "../../jobs",
    "../../v1/jobs/6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d",
    "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d/../../auth/sessions",
    "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d?fields=budget",
    "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d#x",
    "6f2a1f3c9d4e4b1a8c7d2e5f0a9b1c3d",
    "not-a-uuid",
    " 6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d",
  ]) {
    assert.equal(isJobId(attempt), false, `accepted ${JSON.stringify(attempt)} as a job identifier`);
  }
});

test("the token is read out of the fragment", () => {
  assert.equal(tokenFromFragment("#aGVhZGVy.Y2xhaW1z.c2ln"), "aGVhZGVy.Y2xhaW1z.c2ln");
  assert.equal(tokenFromFragment("aGVhZGVy.Y2xhaW1z.c2ln"), "aGVhZGVy.Y2xhaW1z.c2ln");
  assert.equal(tokenFromFragment("#  aGVhZGVy.Y2xhaW1z.c2ln  "), "aGVhZGVy.Y2xhaW1z.c2ln");
});

/**
 * A fragment that is not a credential produces no credential, so the page says "open the link you
 * were sent" rather than making a request that can only be refused. A messaging app that wrapped
 * the URL is the common cause and it is not the same thing as an expired link.
 */
test("a fragment carrying no credential yields none", () => {
  for (const fragment of ["", "#", "#section", "#a.b", "#a.b.c.d", "#a.b.c d", "#a.b.c!"]) {
    assert.equal(tokenFromFragment(fragment), null, `read a token out of ${JSON.stringify(fragment)}`);
  }
});

/**
 * Two links in two tabs cannot overwrite one another, and a token from a previous delivery is
 * never the one presented on this job.
 */
test("a held token is keyed by the job it opens", () => {
  const first = storageKey("6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d");
  const second = storageKey("11111111-2222-4333-8444-555555555555");

  assert.notEqual(first, second);
  assert.match(first, /6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d$/);
});
