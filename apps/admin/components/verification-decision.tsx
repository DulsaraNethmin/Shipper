"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { idempotencyKey } from "@/lib/keys";

/**
 * `Docs/04` §6's step 4: an outcome and a reason, recorded once (SHIP-188d).
 *
 * # The key is held for a decision, not minted per click, and that is the whole clause
 *
 * `Docs/09`'s SHIP-188d row: the decision is recorded "under an `Idempotency-Key` minted in the
 * browser, so a double-click records one decision". A fresh key per submission — which is what
 * `lib/keys.ts` argues for on sign-in and sign-out, and is right there — would **not** satisfy that.
 * Two clicks would be two keys, so two distinct actions: the first records the decision and the
 * second is refused as already in that state, which is one decision and an error message rather than
 * one decision.
 *
 * So the key is minted on the first submission and **held**, and a second click sends the same
 * value. `httpx.Idempotent` replays the first response rather than reaching the handler, so the
 * reviewer sees the outcome they asked for. That matters more here than anywhere else in the panel:
 * `provider_verification_decisions` is append-only and so is `audit_log`, so a duplicate cannot be
 * tidied up afterwards.
 *
 * **The key is discarded whenever the decision changes**, and that is the other half. The
 * middleware's fingerprint covers the body, so a corrected outcome or an edited reason sent under
 * the held key would be refused as `idempotency_key_reused` — the same trap `lib/keys.ts` records
 * for a mistyped password. Changing either field is a different action and gets a different key.
 *
 * The in-flight guard below is not a substitute for any of this. It stops the second of two clicks a
 * tenth of a second apart; the held key is what covers the reviewer who clicked, lost the answer to
 * a dropped connection, and clicked again.
 *
 * # Why a route handler rather than posting straight to the platform
 *
 * The two reasons `app/api/admin/sessions/route.ts` gives: the Go service serves no CORS headers, so
 * a browser would refuse the preflight before the platform saw it; and the platform's location stays
 * a server variable read per request rather than a `NEXT_PUBLIC_` constant baked into a bundle.
 */

/** The four outcomes this screen offers, and what each means to the provider. */
const OUTCOMES = [
  { state: "Verified", note: "May bid for supported job categories." },
  { state: "Restricted", note: "May sign in, but not bid." },
  { state: "Rejected", note: "Evidence refused. The provider may submit again." },
  { state: "Suspended", note: "Access withdrawn pending review." },
] as const;

/**
 * `Pending` is deliberately not offered.
 *
 * The platform would take it — any of `Docs/04` §4's five is a valid target — and `Docs/09`'s row
 * names four. It is the state a submission *arrives* in rather than an outcome a reviewer chooses,
 * and moving somebody back to it would put them in the queue indistinguishable from a provider who
 * has just submitted, at a `submitted_at` that is no longer when they submitted. Asking for new
 * documents is a Rejected with a reason that says so.
 */
export function VerificationDecision({
  providerId,
  mayDecide,
}: {
  providerId: string;

  /**
   * Whether this role holds `verifications.decide` — read to **disable**, never to decide
   * (`Docs/07` §3).
   *
   * A support administrator may open this screen and may not record an outcome, which is
   * `Docs/04` §9's least privilege expressed as two permissions on two endpoints. Without this
   * they compose a reason, click, and read a refusal — so the control that would be refused says
   * so before it is used, exactly as the navigation does for a section a role cannot reach.
   *
   * It changes nothing about what is permitted. Somebody who edited this value in a browser would
   * find the platform refusing the request exactly as before: `DecideVerification` checks
   * `PermissionVerificationsDecide` on every call, out of the session row read on that request.
   */
  mayDecide: boolean;
}) {
  const router = useRouter();
  const [state, setState] = useState("");
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [recorded, setRecorded] = useState<string | null>(null);

  // One value per action, minted on the first attempt and reused by every retry of it. See the
  // file note for why this is held where sign-in's is not.
  const key = useRef<string | null>(null);

  // A changed decision is a different action. Dropping the key here is what stops the next
  // submission being refused as `idempotency_key_reused` on a fingerprint that no longer matches.
  function change(next: { state?: string; reason?: string }) {
    key.current = null;
    setRecorded(null);
    setRefusal(null);
    if (next.state !== undefined) setState(next.state);
    if (next.reason !== undefined) setReason(next.reason);
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;

    setSubmitting(true);
    setRefusal(null);

    if (key.current === null) key.current = idempotencyKey();

    try {
      const answer = await fetch(`/api/admin/verifications/${providerId}/decision`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          "Idempotency-Key": key.current,
        },
        body: JSON.stringify({ state, reason }),
      });

      if (answer.ok) {
        const change = (await answer.json()) as { from?: string; to?: string };
        setRecorded(`Recorded: ${change.from ?? "?"} → ${change.to ?? "?"}`);

        // So the decision history below picks the new entry up, and so the queue this came from
        // no longer lists the provider under the state they have left.
        router.refresh();
        return;
      }

      setRefusal(await told(answer));
    } catch {
      setRefusal("The panel could not reach the platform. The decision was not recorded.");
    } finally {
      setSubmitting(false);
    }
  }

  if (!mayDecide) {
    return (
      <p className="border-border text-muted-foreground rounded-xl border px-4 py-3 text-sm">
        Your role does not hold{" "}
        <code className="font-mono text-xs">verifications.decide</code>, so you can review this
        evidence and cannot record an outcome. Reading the queue and deciding on it are deliberately
        separate permissions.
      </p>
    );
  }

  return (
    <form onSubmit={submit} className="border-border flex flex-col gap-4 rounded-xl border p-4">
      <fieldset className="flex flex-col gap-2" disabled={submitting}>
        <legend className="sr-only">Outcome</legend>
        {OUTCOMES.map((outcome) => (
          <label
            key={outcome.state}
            className="hover:bg-accent/40 flex cursor-pointer items-baseline gap-3 rounded-md px-2 py-1.5 text-sm"
          >
            <input
              type="radio"
              name="state"
              value={outcome.state}
              checked={state === outcome.state}
              onChange={() => change({ state: outcome.state })}
            />
            <span className="text-foreground font-medium">{outcome.state}</span>
            <span className="text-muted-foreground text-xs">{outcome.note}</span>
          </label>
        ))}
      </fieldset>

      <div className="flex flex-col gap-2">
        <Label htmlFor="reason">Reason</Label>
        <textarea
          id="reason"
          name="reason"
          required
          rows={3}
          value={reason}
          disabled={submitting}
          onChange={(event) => change({ reason: event.target.value })}
          placeholder="Licence, registration and insurance all current and matching the account."
          className="border-input bg-background text-foreground placeholder:text-muted-foreground/70 focus-visible:border-ring focus-visible:ring-ring/50 rounded-lg border px-3 py-2 text-sm shadow-xs outline-none focus-visible:ring-3 disabled:opacity-50"
        />
        <p className="text-muted-foreground text-xs">
          Recorded in the provider&rsquo;s own evidence trail and in the audit log. Both are
          append-only, so write it for somebody reading it a year from now.
        </p>
      </div>

      {refusal !== null && (
        <p
          role="alert"
          className="border-destructive/30 bg-destructive/10 text-destructive rounded-lg border px-3 py-2 text-sm"
        >
          {refusal}
        </p>
      )}

      {recorded !== null && (
        <p
          role="status"
          className="border-border bg-accent/40 text-foreground rounded-lg border px-3 py-2 text-sm"
        >
          {recorded}
        </p>
      )}

      <Button type="submit" disabled={submitting || state === "" || reason.trim() === ""}>
        {submitting ? "Recording…" : "Record decision"}
      </Button>
    </form>
  );
}

/**
 * What the reviewer is told when the platform says no.
 *
 * The platform's own words, including the field problems: a reason that is too long, an outcome the
 * provider already holds, and a role without `verifications.decide` are three different refusals and
 * only the platform knows which this is.
 */
async function told(answer: Response): Promise<string> {
  let body: {
    error?: { message?: unknown; details?: unknown };
  } | null = null;
  try {
    body = (await answer.json()) as { error?: { message?: unknown; details?: unknown } };
  } catch {
    body = null;
  }

  const message =
    typeof body?.error?.message === "string" && body.error.message !== ""
      ? body.error.message
      : "That decision could not be recorded.";

  const details = Array.isArray(body?.error?.details)
    ? body.error.details
        .map((entry) =>
          typeof entry === "object" && entry !== null && typeof (entry as { message?: unknown }).message === "string"
            ? ((entry as { message: string }).message)
            : "",
        )
        .filter((text) => text !== "")
    : [];

  return details.length === 0 ? message : `${message} ${details.join(" ")}`;
}
