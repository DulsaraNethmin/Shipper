import assert from "node:assert/strict";
import { register } from "node:module";
import { test } from "node:test";

import type { Delivery } from "./delivery.ts";
import { openLink, type Opened } from "./open.ts";

/**
 * **The regression test for the one mutation SHIP-120 could not catch.**
 *
 * When SHIP-120 landed, the portal was mutated to derive the job identifier from the *token*
 * instead of from the URL path. The URL asked for one job, the token granted another, the page
 * rendered the token's delivery with a `200`, and **every one of the application's twenty tests
 * still passed**. That is recorded in `Docs/11` §3, and this file is what closes it.
 *
 * # Why it is worth a test rather than a comment
 *
 * SHIP-108 put the one-job check in the auth class: the platform compares the job named in the
 * request against the job named in the grant, and refuses a mismatch with `404`. That comparison is
 * only worth something while the client states its intention **independently of the credential it
 * is presenting**. A client that builds the request path out of the token leaves the platform
 * comparing the token with itself — the check still runs, still passes, and has nothing left to
 * catch, on any grant, however widely it has been issued.
 *
 * Nothing on the platform side can detect that, because from the platform's seat the two agree.
 * It has to be caught here, and "the token already names the job, why pass it twice" is the single
 * most natural tidy-up the next person will make: SHIP-121, SHIP-122 and SHIP-123 all work in this
 * code.
 *
 * # It asserts on the wire, not on the source
 *
 * `surface.test.ts` reads source, and that kind of guard is one this repository has now been bitten
 * by twice — `Docs/11` §7a's budget guard was satisfied by a rename, and §9 records the client-side
 * one with the same blind spot. A source scan for "does anything decode the token" would be exactly
 * that shape again: a guard a rename walks past.
 *
 * So this file runs the portal's own code and records **the requests it actually issues**. Both
 * hops are real: `openLink` is what the page calls, its `fetch` is answered by this application's
 * own route handler — the same `GET` Next serves — and the route handler's outbound call is what
 * lands in `toThePlatform`. Only the platform itself is a stand-in, and the stand-in *implements
 * SHIP-108's check* rather than asserting about it, so the wrong-job case is refused here for the
 * same reason it is refused in production.
 *
 * A mutation anywhere in that chain — in `openLink`, in `openDelivery`, or in the route handler —
 * changes the path recorded below, and two of these five tests fail.
 */

/** This origin, which is the portal's own. Only used to make a relative path parseable. */
const ORIGIN = "https://portal.example";

/** The job the driver's link names in its path. */
const JOB_IN_THE_URL = "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d";

/** The job the token grants, which is a different one in every test that matters. */
const JOB_IN_THE_TOKEN = "11111111-2222-4333-8444-555555555555";

const ASSIGNMENT = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee";

/**
 * The `@/` alias, so the route handler can be imported at all.
 *
 * The handler says `@/lib/link`, which the bundler resolves from `tsconfig.json` and Node does not.
 * Registering the hook here rather than in the `test` script keeps every other test file on a
 * plain runtime. See `alias-hooks.mts`.
 */
register("./alias-hooks.mts", import.meta.url);

const { GET } = await import("../app/api/driver/jobs/[jobId]/route.ts");

/** One base64url segment of a token. */
function segment(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}

/**
 * A link token granting exactly one job, shaped like the one `delivery` signs.
 *
 * The claims are real enough to be decoded, and that is the point: a portal that decided to read
 * `job_id` out of the credential would find it here and would then be caught by the paths these
 * tests record. A token whose claims were unreadable would let the mutation fail for the wrong
 * reason and prove nothing.
 */
function linkTokenFor(jobId: string): string {
  return [
    segment({ alg: "EdDSA", typ: "JWT", kid: "driver-1" }),
    segment({ job_id: jobId, assignment_id: ASSIGNMENT, aud: "driver", exp: 2000000000 }),
    "c2lnbmF0dXJlLW5vdGhpbmctaGVyZS12ZXJpZmllcw",
  ].join(".");
}

/** The job a grant names, read the way the platform's verifier reads it. */
function jobGrantedBy(credential: string | null): string | null {
  const claims = credential?.split(" ").pop()?.split(".")[1];
  if (claims === undefined) return null;
  try {
    const decoded: unknown = JSON.parse(Buffer.from(claims, "base64url").toString("utf8"));
    const jobId = (decoded as { job_id?: unknown }).job_id;
    return typeof jobId === "string" ? jobId : null;
  } catch {
    return null;
  }
}

function deliveryOf(jobId: string): Delivery {
  return {
    job_id: jobId,
    assignment_id: ASSIGNMENT,
    driver_name: "Jo Driver",
    assigned_at: "2026-08-13T04:00:00Z",
    link_expires_at: "2026-08-20T04:00:00Z",
  };
}

/** A stand-in platform: what `GET /v1/driver/jobs/{id}` answers, given a path and a grant. */
type Platform = (path: string, credential: string | null) => Response;

/**
 * SHIP-108's one-job check, in the few lines it actually is.
 *
 * The job in the path against the job in the grant, `404` on a mismatch — not `403`, because the
 * same answer has to cover a stood-down driver and a link-holder guessing at a competitor's job.
 */
const checksTheGrant: Platform = (path, credential) => {
  const asked = path.split("/").pop() ?? "";
  const granted = jobGrantedBy(credential);

  if (granted === null) {
    return Response.json({ error: { code: "unauthenticated", message: "no" } }, { status: 401 });
  }
  if (granted !== asked) {
    return Response.json({ error: { code: "not_found", message: "no" } }, { status: 404 });
  }
  return Response.json(deliveryOf(asked));
};

/**
 * A platform that serves whatever it is asked for.
 *
 * Not a straw man — it is what the real one *becomes* when the client derives the path from the
 * token, because then the comparison above can never fail. Using it here lets a test say which job
 * was asked for without the refusal getting in the way of the answer.
 */
const servesWhateverIsAsked: Platform = (path) =>
  Response.json(deliveryOf(path.split("/").pop() ?? ""));

/** One request this portal made, at whichever hop it was made. */
interface Sent {
  url: string;
  credential: string | null;
}

/** Everything that went out during one attempt at opening a link. */
interface Wire {
  toThisOrigin: Sent[];
  toThePlatform: Sent[];
}

/**
 * Open a link in a browser that is not there, and keep what went out.
 *
 * `jobIdInTheUrl` is the path segment — in production it reaches the page as a route parameter and
 * is handed straight to `openLink`. The token is in the fragment, which is where a real link
 * carries it.
 *
 * The `fetch` stub is a router rather than a canned answer: a relative path is the browser asking
 * this origin, so it is handed to the route handler Next would have run, and the absolute URL that
 * handler then requests is the one the platform sees. Only the last hop is fabricated.
 */
async function openTheLink(
  jobIdInTheUrl: string,
  token: string,
  platform: Platform,
): Promise<{ outcome: Opened; wire: Wire; addressBar: string }> {
  const wire: Wire = { toThisOrigin: [], toThePlatform: [] };
  const held = new Map<string, string>();
  const location = { pathname: `/j/${jobIdInTheUrl}`, hash: `#${token}` };

  const browser = {
    location,
    history: {
      replaceState(_state: unknown, _title: string, url: string) {
        location.pathname = url;
        location.hash = "";
      },
    },
    sessionStorage: {
      getItem: (key: string) => held.get(key) ?? null,
      setItem: (key: string, value: string) => void held.set(key, value),
    },
  };

  const sending = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const credential = new Headers(init?.headers).get("authorization"); // spelling:ok — RFC 9110

    if (!/^https?:/i.test(url)) {
      wire.toThisOrigin.push({ url, credential });
      const address = new URL(url, ORIGIN);
      const jobId = decodeURIComponent(address.pathname.split("/").pop() ?? "");
      return GET(new Request(address, { headers: init?.headers }), {
        params: Promise.resolve({ jobId }),
      });
    }

    wire.toThePlatform.push({ url, credential });
    return platform(new URL(url).pathname, credential);
  };

  const globals = globalThis as unknown as { window?: unknown; fetch?: typeof fetch };
  const realFetch = globals.fetch;
  globals.window = browser;
  globals.fetch = sending as typeof fetch;

  try {
    const outcome = await openLink(jobIdInTheUrl, new AbortController().signal);
    return { outcome, wire, addressBar: `${location.pathname}${location.hash}` };
  } finally {
    globals.fetch = realFetch;
    delete globals.window;
  }
}

/** The paths of what went out, which is the whole subject of this file. */
function paths(sent: Sent[]): string[] {
  return sent.map(({ url }) => new URL(url, ORIGIN).pathname);
}

/**
 * **The one that fails under the mutation.** The URL names one job, the credential grants another,
 * and what leaves this portal has to name the URL's.
 */
test("the platform is asked for the job in the URL and not for the job in the token", async () => {
  const { wire } = await openTheLink(
    JOB_IN_THE_URL,
    linkTokenFor(JOB_IN_THE_TOKEN),
    servesWhateverIsAsked,
  );

  assert.deepEqual(
    paths(wire.toThisOrigin),
    [`/api/driver/jobs/${JOB_IN_THE_URL}`],
    "the page asked this origin for a job its own URL did not name",
  );
  assert.deepEqual(
    paths(wire.toThePlatform),
    [`/v1/driver/jobs/${JOB_IN_THE_URL}`],
    "the portal named the token's job upstream instead of the URL's, which leaves SHIP-108's " +
      "one-job check comparing the token with itself",
  );
});

/**
 * The same statement from the other end: the platform's check is allowed to work.
 *
 * The stand-in above does exactly what SHIP-108 does, so this is the wrong-job case reproduced
 * rather than described — and under the mutation it is a `200` and a delivery.
 */
test("a link for one job cannot open another, and it is the platform that refuses it", async () => {
  const { outcome } = await openTheLink(
    JOB_IN_THE_URL,
    linkTokenFor(JOB_IN_THE_TOKEN),
    checksTheGrant,
  );

  assert.deepEqual(
    outcome,
    { kind: "refused", refusal: "closed" },
    "a token for another job opened this one, so the request carried no intention of its own",
  );
});

/**
 * The control, and it is not optional: a test that refuses everything would pass the one above for
 * no reason at all.
 */
test("the same link opens its own job, so the refusal above is about the mismatch", async () => {
  const { outcome } = await openTheLink(
    JOB_IN_THE_TOKEN,
    linkTokenFor(JOB_IN_THE_TOKEN),
    checksTheGrant,
  );

  assert.equal(outcome.kind, "delivery");
  assert.equal(outcome.kind === "delivery" && outcome.delivery.job_id, JOB_IN_THE_TOKEN);
});

/**
 * A header reaches no access log and a request line reaches every one of them. This is the property
 * `lib/link.ts` argues for at length, asserted against what actually went out at both hops.
 */
test("the token travels in the credential header at both hops and in no request line", async () => {
  const token = linkTokenFor(JOB_IN_THE_TOKEN);
  const { wire } = await openTheLink(JOB_IN_THE_URL, token, servesWhateverIsAsked);

  const sent = [...wire.toThisOrigin, ...wire.toThePlatform];
  assert.equal(sent.length, 2, "one hop is missing, so this proves less than it looks");

  for (const { url, credential } of sent) {
    assert.equal(credential, `Bearer ${token}`, `${url} did not carry the link's credential`);
    assert.equal(url.includes(token), false, `${url} carries the token in its request line`);
  }
});

/** Once the token is somewhere a reload can find it, the address bar is just a job identifier. */
test("the token is out of the address bar once the link has been opened", async () => {
  const token = linkTokenFor(JOB_IN_THE_TOKEN);
  const { addressBar } = await openTheLink(JOB_IN_THE_TOKEN, token, checksTheGrant);

  assert.equal(addressBar, `/j/${JOB_IN_THE_TOKEN}`);
  assert.equal(addressBar.includes(token), false);
});
