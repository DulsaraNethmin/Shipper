import assert from "node:assert/strict";
import { test } from "node:test";

import { REFUSALS, codeFrom, deliveryPath, refusalFor, type Refusal } from "./delivery.ts";

/** The request, and what the platform's refusals are turned into (SHIP-120). */

test("the delivery path is relative and reaches one endpoint", () => {
  assert.equal(
    deliveryPath("6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d"),
    "/api/driver/jobs/6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d",
  );
});

/**
 * A relative path cannot address another origin, which is the property that matters: the token
 * goes to this portal's own route handler and there is nowhere else for it to be sent by editing
 * a string. `//evil.example` is the one shape that looks relative and is not.
 */
test("no job identifier can point the request at another origin", () => {
  for (const attempt of ["//evil.example", "https://evil.example", "..", "../../auth/sessions"]) {
    const path = deliveryPath(attempt);
    assert.equal(
      path.startsWith("/api/driver/jobs/"),
      true,
      `${JSON.stringify(attempt)} produced ${path}`,
    );
    assert.equal(path.startsWith("//"), false, `${JSON.stringify(attempt)} produced ${path}`);
    assert.equal(path.includes("://"), false, `${JSON.stringify(attempt)} produced ${path}`);
  }
});

/**
 * The distinction SHIP-108 minted an error code for.
 *
 * `delivery_driver_link_expired` rather than `token_expired`, because a driver has nothing to
 * refresh — a portal that read the second and retried would loop.
 */
test("an expired link is told apart from a link that is not one", () => {
  assert.equal(refusalFor(401, "delivery_driver_link_expired"), "expired");
  assert.equal(refusalFor(401, "unauthenticated"), "invalid");
  assert.equal(refusalFor(401, null), "invalid");
  assert.equal(refusalFor(401, "token_expired"), "invalid");
});

/**
 * **The disclosure test.** The platform answers `404` both when a valid link is presented on
 * another job and when the driver has been stood down, and it chose `404` over `403` so that a
 * link-holder cannot confirm a competitor's job exists. One status has to become one message, or
 * the page hands back what the status code was picked to withhold.
 */
test("both causes of a 404 produce one refusal and one message", () => {
  assert.equal(refusalFor(404, "not_found"), "closed");
  assert.equal(refusalFor(404, null), "closed");
  assert.equal(REFUSALS.closed.title, REFUSALS[refusalFor(404, "not_found")].title);
});

test("a refusal never says whether a job exists", () => {
  for (const refusal of Object.keys(REFUSALS) as Refusal[]) {
    const copy = `${REFUSALS[refusal].title} ${REFUSALS[refusal].body}`.toLowerCase();
    for (const disclosure of [
      "does not exist",
      "no such",
      "not yours",
      "another job",
      "someone else",
      "somebody else",
      "not found",
      "permission",
      "not allowed",
    ]) {
      assert.equal(copy.includes(disclosure), false, `${refusal} copy says "${disclosure}"`);
    }
  }
});

test("a busy or unreachable platform is worth retrying and a bad link is not", () => {
  assert.equal(refusalFor(429, "rate_limited"), "unavailable");
  assert.equal(refusalFor(503, "service_unavailable"), "unavailable");
  assert.equal(refusalFor(500, "internal_error"), "unexpected");
  assert.equal(refusalFor(400, "bad_request"), "unexpected");
});

test("the code is read out of the error contract and nothing else is trusted", () => {
  assert.equal(codeFrom({ error: { code: "not_found", message: "…" } }), "not_found");
  assert.equal(codeFrom({ error: {} }), null);
  assert.equal(codeFrom({ error: null }), null);
  assert.equal(codeFrom({ code: "not_found" }), null);
  assert.equal(codeFrom("not_found"), null);
  assert.equal(codeFrom(null), null);
});
