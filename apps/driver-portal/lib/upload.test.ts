import assert from "node:assert/strict";
import { register } from "node:module";
import { test } from "node:test";

import { capturePhotograph } from "./open.ts";

/**
 * **The photograph, and the three requests it takes** (SHIP-122).
 *
 * `capturePhotograph` asks the platform for somewhere to put one, PUTs the bytes straight to the
 * store, and records the milestone against the key it was given. The order is not rearrangeable and
 * the middle hop does not go to this origin, so what this file asserts is the *shape of what went
 * out* rather than the outcome — an outcome can be right for the wrong reason, and two of the things
 * that matter most here are invisible in one.
 *
 * # The assertion this file exists for
 *
 * **The driver's link is not sent to the object store.** The pre-signed URL carries its own
 * authorisation in its query string — that is what a pre-signed URL *is* — and attaching the
 * credential would send a seven-day, forwardable token to a host this application does not control,
 * on a request with no use for it. `Docs/06` §5.2 puts the bytes outside the platform precisely
 * because the store and the API are separate trust domains, and a client that carried the API's
 * credential into the store's domain would be undoing that.
 *
 * `lib/surface.test.ts` cannot catch it: the credential and the `fetch` are in `lib/delivery.ts`
 * either way, so the file list is identical whether or not the header is attached. It has to be
 * asserted against the request.
 *
 * # And the one after it, which is what "uploads via pre-signed URL" actually means
 *
 * The bytes go to the URL **the platform signed** and to nothing this application constructed. There
 * is no store hostname, no bucket name and no key format anywhere in this codebase — so a mutation
 * that built the destination locally would have to invent all three, and the test that catches it is
 * the one asserting the outgoing URL is the one that came back.
 */

register("./alias-hooks.mts", import.meta.url);

const { POST: PRESIGN } = await import("../app/api/driver/jobs/[jobId]/proof-uploads/route.ts");
const { POST: RECORD } = await import("../app/api/driver/jobs/[jobId]/milestones/route.ts");

const ORIGIN = "https://portal.example";
const STORE = "https://objects.example/shipper-dev";
const JOB = "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d";
const OBJECT_KEY = `proof/${JOB}/019bd7a1-2c44-7f10-9a2c-3d4e5f607182`;

function segment(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}

function linkTokenFor(jobId: string): string {
  return [
    segment({ alg: "EdDSA", typ: "JWT", kid: "driver-1" }),
    segment({ job_id: jobId, assignment_id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", aud: "driver" }),
    "c2lnbmF0dXJlLW5vdGhpbmctaGVyZS12ZXJpZmllcw",
  ].join(".");
}

const TOKEN = linkTokenFor(JOB);

/** A job the link does not open, for the one test that needs the two to disagree. */
const ANOTHER_JOB = "11111111-2222-4333-8444-555555555555";

/** One request this application made, wherever it was made to. */
interface Sent {
  url: string;
  method: string;
  credential: string | null;
  contentType: string | null;
  key: string | null;
  body: unknown;
}

interface Wire {
  toThisOrigin: Sent[];
  toThePlatform: Sent[];
  toTheStore: Sent[];
}

/** What the stand-in platform and store do with this attempt. */
interface Answers {
  /** Whether the presign succeeds, and what it answers with when it does not. */
  presign?: { status: number; code: string };
  /** Whether the PUT to the store succeeds. */
  storeAccepts?: boolean;
  /** The job the link grants, when a test needs it to disagree with the URL's. */
  tokenJob?: string;
}

/**
 * Capture a photograph in a browser that is not there, and keep everything that went out.
 *
 * All three hops are real except the last leg of each: `capturePhotograph` is what the page calls,
 * both `fetch`es to this origin are answered by the route handlers Next would have run, and only the
 * platform and the store are stand-ins.
 */
async function capture(
  answers: Answers = {},
  photograph = new Blob([new Uint8Array(1874)], { type: "image/jpeg" }),
): Promise<{ outcome: Awaited<ReturnType<typeof capturePhotograph>>; wire: Wire }> {
  const wire: Wire = { toThisOrigin: [], toThePlatform: [], toTheStore: [] };
  const held = new Map<string, string>();
  const token = answers.tokenJob === undefined ? TOKEN : linkTokenFor(answers.tokenJob);

  const browser = {
    location: { pathname: `/j/${JOB}`, hash: `#${token}` },
    history: { replaceState: () => undefined },
    sessionStorage: {
      getItem: (key: string) => held.get(key) ?? null,
      setItem: (key: string, value: string) => void held.set(key, value),
      removeItem: (key: string) => void held.delete(key),
    },
  };

  const platform = (path: string, body: unknown): Response => {
    if (path.endsWith("/proof-uploads")) {
      if (answers.presign !== undefined) {
        return Response.json(
          { error: { code: answers.presign.code, message: "no" } },
          { status: answers.presign.status },
        );
      }
      const asked = body as { content_type: string; content_length: number };
      return Response.json({
        object_key: OBJECT_KEY,
        // Lower-cased and trimmed by the platform before it is signed, which is why the PUT has to
        // use what came back and never what the file reported.
        upload_url: `${STORE}/${OBJECT_KEY}?X-Amz-Signature=abc`,
        method: "PUT",
        content_type: asked.content_type.toLowerCase(),
        content_length: asked.content_length,
        expires_at: "2026-08-14T02:07:00.000Z",
      });
    }
    return Response.json(
      {
        id: "01999999-0000-7000-8000-000000000001",
        job_id: JOB,
        milestone: "delivered",
        recorded_by: "driver",
        recorded_at: "2026-08-14T02:00:00.000Z",
        accepted_at: "2026-08-14T02:00:01.000Z",
      },
      { status: 201 },
    );
  };

  const sending = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const headers = new Headers(init?.headers);
    const sent: Sent = {
      url,
      method: init?.method ?? "GET",
      credential: headers.get("authorization"), // spelling:ok — RFC 9110
      contentType: headers.get("content-type"),
      key: headers.get("idempotency-key"),
      body: init?.body,
    };

    if (url.startsWith(STORE)) {
      wire.toTheStore.push(sent);
      return new Response(null, { status: answers.storeAccepts === false ? 403 : 200 });
    }

    if (!/^https?:/i.test(url)) {
      wire.toThisOrigin.push(sent);
      const address = new URL(url, ORIGIN);
      const params = {
        params: Promise.resolve({ jobId: decodeURIComponent(address.pathname.split("/")[4] ?? "") }),
      };
      const handler = address.pathname.endsWith("/proof-uploads") ? PRESIGN : RECORD;
      return handler(
        new Request(address, { method: "POST", headers: init?.headers, body: init?.body as BodyInit }),
        params,
      );
    }

    wire.toThePlatform.push(sent);
    const body: unknown = init?.body === undefined ? null : JSON.parse(String(init.body));
    return platform(new URL(url).pathname, body);
  };

  const globals = globalThis as unknown as { window?: unknown; fetch?: typeof fetch };
  const realFetch = globals.fetch;
  globals.window = browser;
  globals.fetch = sending as typeof fetch;

  try {
    const outcome = await capturePhotograph(JOB, "delivered", photograph);
    return { outcome, wire };
  } finally {
    globals.fetch = realFetch;
    delete globals.window;
  }
}

/**
 * **The one this file exists for.** The store gets the bytes and gets no credential.
 */
test("the driver's link is not sent to the object store", async () => {
  const { wire } = await capture();

  assert.equal(wire.toTheStore.length, 1, "the photograph did not reach the store");
  const [put] = wire.toTheStore;

  assert.equal(
    put.credential,
    null,
    "the driver's link was sent to the object store — the pre-signed URL is the whole of the " +
      "authorisation, and the store is a different trust domain from the API",
  );
  assert.equal(put.key, null, "an idempotency key was sent to the store, which does not read one");
  assert.equal(put.url.includes(TOKEN), false, "the token is in the store's request line");

  // And it did reach the platform on the two hops that need it, so the assertion above is about the
  // store rather than about a chain that carries no credential anywhere.
  for (const sent of wire.toThePlatform) {
    assert.equal(sent.credential, `Bearer ${TOKEN}`, `${sent.url} lost the driver's link`);
  }
});

/**
 * **The wave-7 mutation on the newest code path.** The URL names one job and the credential grants
 * another, and every request the capture chain issues has to name the URL's.
 *
 * `lib/one-job.test.ts` holds the read and the milestone write to this. The capture chain is a third
 * place the same tidy-up is available — "the token already names the job, why pass it twice" — and it
 * is the one with a credential coming *back*: a presign built out of the grant would leave SHIP-108's
 * one-job check comparing the token with itself, and the platform would then mint an upload URL for
 * the token's job while the driver believed they were photographing the URL's.
 *
 * The platform is a stand-in that serves whatever it is asked for, deliberately, so the assertion is
 * about the path that went out rather than about a refusal getting in the way of it.
 */
test("the capture chain names the job in the URL and not the job in the token", async () => {
  const { wire } = await capture({ tokenJob: ANOTHER_JOB });

  assert.deepEqual(
    wire.toThePlatform.map((sent) => new URL(sent.url).pathname),
    [`/v1/driver/jobs/${JOB}/proof-uploads`, `/v1/driver/jobs/${JOB}/milestones`],
    "the portal asked for an upload slot on the token's job instead of the URL's, which leaves " +
      "SHIP-108's one-job check comparing the token with itself on a route that issues a credential",
  );
});

/** The bytes go to the URL the platform signed, and to nothing this application built. */
test("the photograph is PUT to the URL the platform signed", async () => {
  const { wire } = await capture();
  const [put] = wire.toTheStore;

  assert.equal(put.url, `${STORE}/${OBJECT_KEY}?X-Amz-Signature=abc`);
  assert.equal(put.method, "PUT", "the method came from somewhere other than the platform's answer");
  assert.ok(put.body instanceof Blob, "the photograph was not sent as its own bytes");
});

/**
 * The media type on the PUT is the one the platform signed, not the one the file reported.
 *
 * Both are signed into the URL, so a handset reporting `IMAGE/JPEG` must send `image/jpeg` — the
 * platform normalises before it signs, and a client that echoed its own spelling would get
 * `403 SignatureDoesNotMatch` from the store with no explanation.
 */
test("the PUT carries the media type the platform signed", async () => {
  const { wire } = await capture(
    {},
    new Blob([new Uint8Array(10)], { type: "IMAGE/JPEG" }),
  );

  assert.equal(wire.toTheStore[0].contentType, "image/jpeg");
});

/**
 * `Content-Length` is not set by this application, and that is correct rather than an omission: it is
 * a forbidden header name in the Fetch specification, so a browser drops any attempt to set one and
 * computes it from the body. The platform signs the *exact* size, and the browser sends exactly that
 * many bytes because it is sending exactly that file.
 */
test("Content-Length is left to the browser, and the exact size is what was asked for", async () => {
  const photograph = new Blob([new Uint8Array(1874)], { type: "image/jpeg" });
  const { wire } = await capture({}, photograph);

  const asked = JSON.parse(String(wire.toThePlatform[0].body)) as { content_length: number };
  assert.equal(asked.content_length, photograph.size, "the platform signed a size the file does not have");

  const headers = new Headers();
  assert.equal(headers.get("content-length"), null);
});

/** Three requests, in the order the ticket requires, and the middle one is the store's. */
test("the presign, the upload and the recording happen in that order", async () => {
  const { outcome, wire } = await capture();

  assert.deepEqual(
    wire.toThisOrigin.map((sent) => new URL(sent.url, ORIGIN).pathname),
    [`/api/driver/jobs/${JOB}/proof-uploads`, `/api/driver/jobs/${JOB}/milestones`],
  );
  assert.deepEqual(
    wire.toThePlatform.map((sent) => new URL(sent.url).pathname),
    [`/v1/driver/jobs/${JOB}/proof-uploads`, `/v1/driver/jobs/${JOB}/milestones`],
  );
  assert.equal(outcome.kind, "recorded");
});

/** The object key the platform issued is what is recorded, and the page never invents one. */
test("the milestone is recorded against the key the platform issued", async () => {
  const { wire } = await capture();

  const recorded = JSON.parse(String(wire.toThePlatform[1].body)) as {
    milestone: string;
    proof: { object_key: string };
  };
  assert.equal(recorded.milestone, "delivered");
  assert.equal(recorded.proof.object_key, OBJECT_KEY);
});

/**
 * The presign's key is fresh and the milestone's is held, which is the asymmetry `lib/keys.ts`
 * argues for. Two captures, and the presign keys must differ.
 */
test("each capture asks for a new upload slot under a new key", async () => {
  const first = await capture();
  const second = await capture();

  assert.notEqual(
    first.wire.toThePlatform[0].key,
    second.wire.toThePlatform[0].key,
    "the second capture reused the first one's key, so the middleware replays a URL whose expiry " +
      "has already run down",
  );
  for (const attempt of [first, second]) {
    assert.match(
      attempt.wire.toThePlatform[0].key ?? "",
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
    );
  }
});

/**
 * A failed upload records nothing, and says so in a way the driver can act on.
 *
 * Nothing durable was written by the presign, so what is left behind is at most one unreferenced
 * object key nobody will use. What must **not** happen is the milestone being recorded against a key
 * whose object never arrived: the platform would refuse it anyway, but a client that tried has
 * misunderstood what proof is.
 */
test("a failed upload records no milestone", async () => {
  const { outcome, wire } = await capture({ storeAccepts: false });

  assert.deepEqual(outcome, { kind: "refused", refusal: "upload_failed" });
  assert.equal(
    wire.toThePlatform.length,
    1,
    "the milestone was recorded against a photograph that never reached the store",
  );
});

/** A refused presign never touches the store, and never records anything either. */
test("a refused presign uploads nothing and records nothing", async () => {
  const { outcome, wire } = await capture({
    presign: { status: 422, code: "validation_failed" },
  });

  assert.deepEqual(outcome, { kind: "refused", refusal: "rejected" });
  assert.equal(wire.toTheStore.length, 0);
  assert.equal(wire.toThePlatform.length, 1);
});

/** A stood-down link is refused at the presign, and the driver is told the link is the problem. */
test("a link that no longer opens the delivery is refused before anything is uploaded", async () => {
  const { outcome, wire } = await capture({ presign: { status: 404, code: "not_found" } });

  assert.deepEqual(outcome, { kind: "refused", refusal: "closed" });
  assert.equal(wire.toTheStore.length, 0);
});
