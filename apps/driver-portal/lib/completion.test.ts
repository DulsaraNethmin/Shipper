import assert from "node:assert/strict";
import { register } from "node:module";
import { test } from "node:test";

import { openDelivery, type Delivery } from "./delivery.ts";
import { capturePhotograph, recordStep } from "./open.ts";

/**
 * **The delivery completion form, and the field `Docs/01` §4.4 required for eight waves before
 * anything could hold it** (SHIP-123).
 *
 * That paragraph requires four things of a delivered job — recipient name, delivery timestamp,
 * delivery note, and photo proof. Three were reachable before this ticket; `000605`'s own comment
 * named SHIP-123 as the one that would close the fourth and the fifth, and `Docs/11` §4 carried the
 * gap as documentation ahead of code.
 *
 * What is asserted here is the client's half: that both fields reach the platform on a `delivered`
 * and on nothing else, through both paths to it — the photograph and the reasoned exception — and
 * that `delivered_at` is read back so the page can stop offering the controls.
 */

register("./alias-hooks.mts", import.meta.url);

const { POST: PRESIGN } = await import("../app/api/driver/jobs/[jobId]/proof-uploads/route.ts");
const { POST: RECORD } = await import("../app/api/driver/jobs/[jobId]/milestones/route.ts");
const { GET: READ } = await import("../app/api/driver/jobs/[jobId]/route.ts");

const ORIGIN = "https://portal.example";
const STORE = "https://objects.example/shipper-dev";
const JOB = "6f2a1f3c-9d4e-4b1a-8c7d-2e5f0a9b1c3d";
const OBJECT_KEY = `proof/${JOB}/019bd7a1-2c44-7f10-9a2c-3d4e5f607182`;

function segment(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}

const TOKEN = [
  segment({ alg: "EdDSA", typ: "JWT", kid: "driver-1" }),
  segment({ job_id: JOB, assignment_id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", aud: "driver" }),
  "c2lnbmF0dXJlLW5vdGhpbmctaGVyZS12ZXJpZmllcw",
].join(".");

const FINISHED = { recipientName: "R. Chen", deliveryNote: "Left with reception, signed for" };

/** The bodies this application sent to the platform, in order, parsed. */
type Bodies = Record<string, unknown>[];

/**
 * Run one page action in a browser that is not there, and keep the bodies that reached the platform.
 *
 * Both hops are real: the portal's own route handlers are the ones Next would run, and only the
 * platform and the store are stand-ins.
 */
async function inTheBrowser<T>(
  act: () => Promise<T>,
  delivered?: string,
): Promise<{ outcome: T; bodies: Bodies }> {
  const bodies: Bodies = [];
  const held = new Map<string, string>();

  const browser = {
    location: { pathname: `/j/${JOB}`, hash: `#${TOKEN}` },
    history: { replaceState: () => undefined },
    sessionStorage: {
      getItem: (key: string) => held.get(key) ?? null,
      setItem: (key: string, value: string) => void held.set(key, value),
      removeItem: (key: string) => void held.delete(key),
    },
  };

  const platform = (path: string, body: unknown): Response => {
    if (path.endsWith("/proof-uploads")) {
      const asked = body as { content_type: string; content_length: number };
      return Response.json({
        object_key: OBJECT_KEY,
        upload_url: `${STORE}/${OBJECT_KEY}?X-Amz-Signature=abc`,
        method: "PUT",
        content_type: asked.content_type,
        content_length: asked.content_length,
        expires_at: "2026-08-14T02:07:00.000Z",
      });
    }
    if (path.endsWith("/milestones")) {
      const asked = body as { milestone: string };
      return Response.json(
        {
          id: "01999999-0000-7000-8000-000000000001",
          job_id: JOB,
          milestone: asked.milestone,
          recorded_by: "driver",
          recorded_at: "2026-08-14T02:00:00.000Z",
          accepted_at: "2026-08-14T02:00:01.000Z",
        },
        { status: 201 },
      );
    }

    const view: Delivery = {
      job_id: JOB,
      assignment_id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
      driver_name: "Jo Driver",
      assigned_at: "2026-08-13T04:00:00Z",
      link_expires_at: "2026-08-20T04:00:00Z",
    };
    // `delivered_at` is omitted entirely rather than sent empty, which is what `omitempty` does on
    // the platform side and is the case `isDelivery` has to keep accepting.
    if (delivered !== undefined) view.delivered_at = delivered;
    return Response.json(view);
  };

  const sending = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;

    if (url.startsWith(STORE)) return new Response(null, { status: 200 });

    if (!/^https?:/i.test(url)) {
      const address = new URL(url, ORIGIN);
      const params = {
        params: Promise.resolve({ jobId: decodeURIComponent(address.pathname.split("/")[4] ?? "") }),
      };
      if (address.pathname.endsWith("/proof-uploads")) {
        return PRESIGN(
          new Request(address, { method: "POST", headers: init?.headers, body: init?.body as BodyInit }),
          params,
        );
      }
      if (address.pathname.endsWith("/milestones")) {
        return RECORD(
          new Request(address, { method: "POST", headers: init?.headers, body: init?.body as BodyInit }),
          params,
        );
      }
      return READ(new Request(address, { headers: init?.headers }), params);
    }

    const body: unknown = init?.body === undefined ? null : JSON.parse(String(init.body));
    if (body !== null) bodies.push(body as Record<string, unknown>);
    return platform(new URL(url).pathname, body);
  };

  const globals = globalThis as unknown as { window?: unknown; fetch?: typeof fetch };
  const realFetch = globals.fetch;
  globals.window = browser;
  globals.fetch = sending as typeof fetch;

  try {
    return { outcome: await act(), bodies };
  } finally {
    globals.fetch = realFetch;
    delete globals.window;
  }
}

/** The recipient and the note reach the platform on the exception path. */
test("a delivery recorded with a reasoned exception carries the recipient and the note", async () => {
  const { outcome, bodies } = await inTheBrowser(() =>
    recordStep(JOB, "delivered", {
      evidence: { exception_reason: "camera_unavailable" },
      completion: FINISHED,
    }));

  assert.equal(outcome.kind, "recorded");
  assert.equal(bodies.length, 1);
  assert.deepEqual(bodies[0].proof, { exception_reason: "camera_unavailable" });
  assert.equal(bodies[0].recipient_name, "R. Chen");
  assert.equal(bodies[0].delivery_note, "Left with reception, signed for");
});

/** And on the photographed path, which is three requests rather than one. */
test("a photographed delivery carries them on the recording, not on the presign", async () => {
  const photograph = new Blob([new Uint8Array(1874)], { type: "image/jpeg" });
  const { outcome, bodies } = await inTheBrowser(() =>
    capturePhotograph(JOB, "delivered", photograph, { completion: FINISHED }));

  assert.equal(outcome.kind, "recorded");
  assert.equal(bodies.length, 2, "the presign and the recording");

  // The presign describes bytes and nothing else. A recipient name in it would be personal data on
  // a request whose whole subject is a media type and a length.
  assert.deepEqual(Object.keys(bodies[0]).sort(), ["content_length", "content_type"]);

  assert.equal(bodies[1].milestone, "delivered");
  assert.equal(bodies[1].recipient_name, "R. Chen");
  assert.equal(bodies[1].delivery_note, "Left with reception, signed for");
  assert.deepEqual(bodies[1].proof, { object_key: OBJECT_KEY });
});

/**
 * Every other milestone sends neither, because the platform refuses both on any milestone but
 * `delivered` — a recipient name on a pickup is a handover that did not happen.
 */
test("an ordinary milestone carries neither field", async () => {
  const { bodies } = await inTheBrowser(() => recordStep(JOB, "picked_up"));

  assert.equal(bodies.length, 1);
  assert.equal("recipient_name" in bodies[0], false);
  assert.equal("delivery_note" in bodies[0], false);
  assert.deepEqual(Object.keys(bodies[0]).sort(), ["milestone", "recorded_at"]);
});

/**
 * A delivery that has not been delivered carries no `delivered_at`, and the page still reads it.
 *
 * This is the case the field's `omitempty` produces, and it is the reason `isDelivery` deliberately
 * does not require it: an absent `delivered_at` is an ordinary delivery in progress, not a broken
 * contract.
 */
test("a delivery in progress is read without a delivered_at", async () => {
  const { outcome } = await inTheBrowser(() => openDelivery(JOB, TOKEN));

  assert.equal(outcome.kind, "delivery");
  assert.equal(outcome.kind === "delivery" && outcome.delivery.delivered_at, undefined);
});

/**
 * A finished delivery reports when, which is the whole of how the page becomes read-only after a
 * reload — component state does not survive one, and the alternative would be this page inventing a
 * fact about the delivery rather than reading the platform's.
 */
test("a finished delivery is read back with the time it was delivered", async () => {
  const { outcome } = await inTheBrowser(
    () => openDelivery(JOB, TOKEN),
    "2026-08-14T06:22:11.000Z",
  );

  assert.equal(outcome.kind, "delivery");
  assert.equal(
    outcome.kind === "delivery" && outcome.delivery.delivered_at,
    "2026-08-14T06:22:11.000Z",
  );
});
