import assert from "node:assert/strict";
import { register } from "node:module";
import { test } from "node:test";

import { isSettled, recordRefusalFor, type Refusal } from "./delivery.ts";
import { MILESTONES, milestoneFor } from "./milestones.ts";
import { recordStep } from "./open.ts";

/**
 * **The idempotency key's lifecycle, which is the half of SHIP-121 that a driver meets and nobody
 * else does.**
 *
 * `httpx.Idempotent`'s own refusal states the contract in one sentence — "generate one value per
 * action and reuse it for every retry of that action" — and both halves fail silently in opposite
 * directions on a phone with no signal:
 *
 * - **Reuse where a fresh key was needed** answers the driver with a milestone they recorded
 *   earlier and writes nothing. `Docs/02` §5's second pickup attempt is the case: a driver who
 *   reaches a locked gate and records `en_route_to_pickup` again on the way back has made a second
 *   claim about the world, and `000601` deliberately has no uniqueness on `(job_id, milestone)` so
 *   that it can be kept.
 * - **A fresh key where reuse was needed** records the milestone twice. A driver taps, the request
 *   goes out, the answer never comes back because they are in a shed, and they tap again.
 *
 * Neither produces an error anywhere. The first looks like success and wrote nothing; the second
 * looks like success and wrote two rows. So the rule — settle on an answer, keep on silence — is
 * asserted here against the keys that actually leave the browser, rather than described in a
 * comment beside the code that implements it.
 *
 * # The route handler is in the chain, exactly as `one-job.test.ts` puts it there
 *
 * The key has to survive the portal's own hop. A handler that dropped the header would turn every
 * recording into a `400` naming a header the browser had sent, and a handler that *minted* one
 * would give every retry a new key — which is the second failure above, moved somewhere no client
 * test would look.
 */

register("./alias-hooks.mts", import.meta.url);

const { POST } = await import("../app/api/driver/jobs/[jobId]/milestones/route.ts");

const ORIGIN = "https://portal.example";
const JOB = "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d";

function segment(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}

const TOKEN = [
  segment({ alg: "EdDSA", typ: "JWT", kid: "driver-1" }),
  segment({ job_id: JOB, assignment_id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", aud: "driver" }),
  "c2lnbmF0dXJlLW5vdGhpbmctaGVyZS12ZXJpZmllcw",
].join(".");

/** What the stand-in platform did with one recording attempt. */
type Answer = "recorded" | "silence" | { status: number; code: string };

/** One browser session across several taps, which is what a key has to survive. */
interface Session {
  /** The `Idempotency-Key` each attempt carried, in order, as the **platform** saw it. */
  keys: string[];
  /** The same, as this origin's route handler saw it, so a dropped header is visible. */
  keysAtThisOrigin: string[];
  tap: (milestone: string) => Promise<void>;
}

/**
 * A browser holding one link, whose platform answers whatever the test says next.
 *
 * `sessionStorage` is a real map that survives every tap, because surviving is the whole property:
 * `lib/keys.ts` chose the store over a React ref precisely so a reload keeps the key, and a stub
 * that reset between calls would pass while the mechanism was broken.
 */
function session(answers: Answer[]): Session {
  const held = new Map<string, string>();
  const keys: string[] = [];
  const keysAtThisOrigin: string[] = [];
  let attempt = 0;

  const browser = {
    location: { pathname: `/j/${JOB}`, hash: `#${TOKEN}` },
    history: { replaceState: () => undefined },
    sessionStorage: {
      getItem: (key: string) => held.get(key) ?? null,
      setItem: (key: string, value: string) => void held.set(key, value),
      removeItem: (key: string) => void held.delete(key),
    },
  };

  const sending = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const headers = new Headers(init?.headers);

    if (!/^https?:/i.test(url)) {
      keysAtThisOrigin.push(headers.get("idempotency-key") ?? "");
      const address = new URL(url, ORIGIN);
      return POST(
        new Request(address, { method: "POST", headers: init?.headers, body: init?.body as BodyInit }),
        { params: Promise.resolve({ jobId: decodeURIComponent(address.pathname.split("/")[4] ?? "") }) },
      );
    }

    keys.push(headers.get("idempotency-key") ?? "");

    const answer = answers[attempt++] ?? "recorded";
    if (answer === "silence") throw new TypeError("fetch failed");
    if (answer === "recorded") {
      return Response.json(
        {
          id: "01999999-0000-7000-8000-000000000001",
          job_id: JOB,
          milestone: "picked_up",
          recorded_by: "driver",
          recorded_at: "2026-08-14T02:00:00.000Z",
          accepted_at: "2026-08-14T02:00:01.000Z",
        },
        { status: 201 },
      );
    }
    return Response.json(
      { error: { code: answer.code, message: "no" } },
      { status: answer.status },
    );
  };

  return {
    keys,
    keysAtThisOrigin,
    tap: async (milestone: string) => {
      const globals = globalThis as unknown as { window?: unknown; fetch?: typeof fetch };
      const realFetch = globals.fetch;
      globals.window = browser;
      globals.fetch = sending as typeof fetch;
      try {
        await recordStep(JOB, milestone);
      } finally {
        globals.fetch = realFetch;
        delete globals.window;
      }
    },
  };
}

/**
 * **The one that fails if `recordStep` settles on silence.** Two taps, no answer to either, and the
 * platform has to see one key — because they are one action retrying.
 */
test("a retry after no answer at all carries the key the first attempt used", async () => {
  const browser = session(["silence", "silence"]);
  await browser.tap("picked_up");
  await browser.tap("picked_up");

  assert.equal(browser.keys.length, 2, "the retry never reached the platform");
  assert.equal(
    browser.keys[0],
    browser.keys[1],
    "the retry carried a fresh key, so a driver in a shed records the same milestone twice",
  );
});

/**
 * **The one that fails if `recordStep` never settles.** The platform answered, so the next tap is a
 * new action — `Docs/02` §5's second pickup attempt, which has to be recordable.
 */
test("a second tap after an answer carries a new key", async () => {
  const browser = session(["recorded", "recorded"]);
  await browser.tap("en_route_to_pickup");
  await browser.tap("en_route_to_pickup");

  assert.equal(browser.keys.length, 2);
  assert.notEqual(
    browser.keys[0],
    browser.keys[1],
    "the second attempt reused the first one's key, so a failed pickup attempt is answered with " +
      "the first recording and nothing is written",
  );
});

/**
 * A `4xx` the driver caused is an answer as much as a `201` is: the platform decided, so the action
 * is over and the next tap is a new one. Getting this wrong would leave a driver whose `Delivered`
 * was refused for want of evidence unable to record it afterwards under a new key.
 */
test("a refusal the platform decided also ends the action", async () => {
  const browser = session([
    { status: 409, code: "delivery_proof_required" },
    "recorded",
  ]);
  await browser.tap("delivered");
  await browser.tap("delivered");

  assert.notEqual(browser.keys[0], browser.keys[1]);
});

/** Two milestones in flight at once do not share a key, and so cannot take each other's answer. */
test("each milestone holds a key of its own", async () => {
  const browser = session(["silence", "silence"]);
  await browser.tap("picked_up");
  await browser.tap("in_transit");

  assert.notEqual(browser.keys[0], browser.keys[1]);
});

/** The key survives this origin's hop, which is where a proxy would silently drop or replace it. */
test("the key reaches the platform as the browser minted it", async () => {
  const browser = session(["recorded"]);
  await browser.tap("picked_up");

  assert.equal(browser.keysAtThisOrigin.length, 1);
  assert.equal(browser.keys[0], browser.keysAtThisOrigin[0], "the route handler changed the key");
  assert.notEqual(browser.keys[0], "", "no key reached the platform");
});

/**
 * The keys are unguessable, which was load-bearing on this route and is defence in depth now.
 *
 * **Until SHIP-147b a driver-token request scoped its idempotency key to `anonymous`** — the scope
 * is computed group-wide, outside the middleware, and a driver token deliberately produces no
 * subject — so `idem:v1:anonymous:<key>` was a namespace shared with every other anonymous caller,
 * and what stopped a stored response being read by somebody else was that reproducing it needs the
 * exact request *and* the exact key. `httpx.SubjectScope` now answers `credential:<salted digest of
 * the bearer token>` for a credential that produces no subject, so the namespace is this link's
 * alone. The key is still 122 bits from a CSPRNG and still asserted here: `lib/keys.ts` argues why
 * a client should not lean on a platform-side scope decision staying as it is.
 */
test("a minted key is a random identifier and not a counter", async () => {
  const browser = session(["recorded", "recorded", "recorded"]);
  await browser.tap("picked_up");
  await browser.tap("picked_up");
  await browser.tap("picked_up");

  assert.equal(new Set(browser.keys).size, 3);
  for (const key of browser.keys) {
    assert.match(key, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
  }
});

/** The two refusals a write can be told that a read cannot, mapped from `code` and never from text. */
test("the write-only refusals are branched on the machine-readable code", () => {
  const cases: [number, string | null, Refusal][] = [
    [409, "delivery_milestone_not_permitted", "too_early"],
    [409, "delivery_proof_required", "needs_evidence"],
    [409, "delivery_proof_not_uploaded", "needs_evidence"],
    [409, "idempotency_key_reused", "unavailable"],
    [409, "idempotency_in_progress", "unavailable"],
    [422, "validation_failed", "rejected"],
    [401, "delivery_driver_link_expired", "expired"],
    [404, "not_found", "closed"],
    [503, "service_unavailable", "unavailable"],
  ];

  for (const [status, code, want] of cases) {
    assert.equal(recordRefusalFor(status, code), want, `${status} ${code}`);
  }
});

/** Exactly the two refusals that are not answers keep the action open. */
test("only the refusals that are not answers leave the action unsettled", () => {
  assert.equal(isSettled("unavailable"), false);
  assert.equal(isSettled("unexpected"), false);

  for (const settled of ["expired", "invalid", "closed", "too_early", "needs_evidence", "rejected"] as const) {
    assert.equal(isSettled(settled), true, settled);
  }
});

/**
 * The four the platform accepts, and the one it refuses by name.
 *
 * `internal/delivery`'s `Recording.problems` refuses `driver_assigned` — "a driver is put on a job
 * through its own endpoint, not recorded as a milestone" — so a button offering it would be a
 * `422` on every tap. The list here is hand-written (`lib/milestones.ts` says why), which is
 * exactly the situation that needs a test rather than a comment.
 */
test("the portal offers the four recordable milestones and not the fifth", () => {
  assert.deepEqual(
    MILESTONES.map((m) => m.wire),
    ["en_route_to_pickup", "picked_up", "in_transit", "delivered"],
  );
  assert.equal(milestoneFor("driver_assigned"), undefined);
  assert.deepEqual(
    MILESTONES.filter((m) => m.needsEvidence).map((m) => m.wire),
    ["delivered"],
    "Docs/01 §4.4 requires evidence of exactly one of the five",
  );
});
