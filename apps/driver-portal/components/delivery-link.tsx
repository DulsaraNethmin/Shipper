"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import {
  REFUSALS,
  type Completion as Finished,
  type Delivery,
  type Evidence,
  type Refusal,
} from "@/lib/delivery";
import { dayFirst } from "@/lib/format";
import { EXCEPTION_COPY, MILESTONES, type Milestone } from "@/lib/milestones";
import { capturePhotograph, openLink, recordStep, type Opened, type Told } from "@/lib/open";
import { proofExceptionReasonValues } from "@/lib/statuses.gen";

/**
 * The delivery a link opens, and the milestones a driver records on it (SHIP-120, SHIP-121).
 *
 * This is a client component and it has to be: the token arrives in the URL fragment, which no
 * server ever receives, and that is the whole point of putting it there (`lib/link.ts`). A server
 * component could not read it, so the only shapes available were a credential in the path — logged
 * at every hop, for as long as those logs are kept — or this.
 *
 * Nothing here decides anything. It reads the link, presents it to the platform, and renders the
 * answer. There is no filtering, no "is this job mine", and no branch that could hide a delivery
 * the platform was willing to serve: on this surface the platform decides and the page displays
 * (`Docs/07` §3).
 *
 * **That applies to the buttons as much as to the card.** Every milestone is offered on every
 * delivery, always, and the page never greys one out because it thinks the delivery has not reached
 * it. It cannot know — `GET /v1/driver/jobs/{id}` serves no status, deliberately — and a page that
 * guessed would be making the authorisation decision `Docs/07` §3 puts on the platform, in the one
 * direction that hurts: a driver at a roller door with the button they need disabled.
 *
 * **Reading the link and recording a milestone are both in `lib/open.ts` rather than here, and that
 * is not a tidy-up.** JSX cannot be imported by `node --test`, so nothing in this file can be
 * asserted on; `lib/one-job.test.ts` holds the portal to asking the platform for the job in the
 * *URL* rather than the job in the *token*, on the read and on the write, and it can only do that
 * against code a test can reach. What is left here is state and markup. `jobId` arrives as a prop
 * from the route and is handed on unchanged — it is not derived from anything, and nothing in this
 * file has the token to derive it from.
 */

/** What the page is showing: an answer, or the moment before there is one. */
type View = { kind: "opening" } | Opened;

/**
 * Where one milestone button has got to.
 *
 * Local to this page view and deliberately not durable. It is what the driver just did, not what the
 * platform holds — a reload starts every button at `idle` again, and tapping a milestone that is
 * already recorded is safe: `lib/keys.ts` gives the new tap a new key, `000601` has no uniqueness on
 * `(job_id, milestone)`, and `Docs/02` §5 calls a repeat an ordinary failed pickup attempt.
 *
 * Persisting it would need the platform to serve a driver their own milestone list, and no such
 * route exists — `GET /v1/jobs/{id}/delivery/milestones` is `RequireUser`. `Docs/11` §3 records the
 * gap rather than this file inventing a local source of truth for it.
 */
type StepState =
  | { kind: "idle" }
  | { kind: "recording" }
  | { kind: "recorded"; at: string }
  | { kind: "refused"; refusal: Refusal };

export function DeliveryLink({ jobId }: { jobId: string }) {
  const [view, setView] = useState<View>({ kind: "opening" });
  const inFlight = useRef<AbortController | null>(null);

  const open = useCallback(() => {
    inFlight.current?.abort();
    const controller = new AbortController();
    inFlight.current = controller;

    void openLink(jobId, controller.signal).then(
      (next) => {
        if (!controller.signal.aborted) setView(next);
      },
      () => {
        // openLink resolves rather than rejects: openDelivery turns every failure into an outcome,
        // and the link reading cannot throw. A browser that surprises us still gets a page.
        if (!controller.signal.aborted) setView({ kind: "refused", refusal: "unexpected" });
      },
    );
  }, [jobId]);

  useEffect(() => {
    open();
    return () => inFlight.current?.abort();
  }, [open]);

  const again = useCallback(() => {
    setView({ kind: "opening" });
    open();
  }, [open]);

  if (view.kind === "opening") return <Opening />;
  if (view.kind === "missing") return <NoLink />;
  if (view.kind === "refused") return <Refused refusal={view.refusal} onRetry={again} />;
  return <Detail jobId={jobId} delivery={view.delivery} />;
}

/** The shell every state renders inside, so the page does not jump as one replaces another. */
function Frame({ children }: { children: React.ReactNode }) {
  return <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-16">{children}</main>;
}

function Opening() {
  return (
    <Frame>
      <h1 className="text-xl font-semibold tracking-tight">Delivery</h1>
      <p className="text-muted-foreground text-sm" role="status">
        Opening your delivery…
      </p>
    </Frame>
  );
}

/**
 * The address bar has a job identifier in it and no credential.
 *
 * Deliberately not one of the [REFUSALS]: nothing was refused, because nothing was asked. The
 * instruction is the useful part — the message thread is where the working link lives.
 */
function NoLink() {
  return (
    <Frame>
      <h1 className="text-xl font-semibold tracking-tight">Open the link you were sent</h1>
      <p className="text-muted-foreground text-sm">
        This page needs the full link from the message the transport provider sent you. Open it
        again from that message rather than from your browser history.
      </p>
      <p className="text-muted-foreground text-sm">
        There is no account to sign in to, so keep that message — it is the way back in.
      </p>
    </Frame>
  );
}

function Refused({ refusal, onRetry }: { refusal: Refusal; onRetry: () => void }) {
  const { title, body } = REFUSALS[refusal];
  const retryable = refusal === "unavailable" || refusal === "unexpected";

  return (
    <Frame>
      <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
      <p className="text-muted-foreground text-sm">{body}</p>
      {retryable ? (
        <Button size="lg" className="h-12 justify-center text-base" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </Frame>
  );
}

/** One labelled fact. */
function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-sm">{value}</dd>
    </div>
  );
}

/**
 * The delivery itself — five fields, because five is what the platform serves.
 *
 * Every one of them is rendered. `Docs/11` §3 records the reasoning: the endpoint's response *is*
 * the answer to "what may a driver see", so a page that quietly dropped a field would be taking
 * that decision back off the platform, and a page that added one would be inventing it. The two
 * identifiers are the least interesting to look at and the most useful on a phone call, which is
 * why they are small and at the bottom rather than absent.
 */
function Detail({ jobId, delivery }: { jobId: string; delivery: Delivery }) {
  const assigned = dayFirst(delivery.assigned_at);
  const expires = dayFirst(delivery.link_expires_at);

  // Seeded from the platform and updated when this page view finishes the job (SHIP-123). The
  // platform's answer is what survives a reload; the state is what makes the controls disappear the
  // moment the delivery is recorded, without a second round trip to be told something this page
  // already knows.
  const [deliveredAt, setDeliveredAt] = useState<string>(delivery.delivered_at ?? "");

  return (
    <Frame>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">Delivery</h1>
        <p className="text-muted-foreground text-sm">
          This link is for {delivery.driver_name}.
        </p>
      </header>

      {deliveredAt === "" ? (
        <Progress jobId={jobId} onDelivered={setDeliveredAt} />
      ) : (
        <Finished at={deliveredAt} />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Your assignment</CardTitle>
          <CardDescription>
            The transport provider put you on this job. It is the only one this link opens.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <dl className="flex flex-col gap-3">
            <Fact label="Driver" value={delivery.driver_name} />
            <Fact label="Assigned" value={assigned ?? "Not recorded"} />
            <Fact
              label="This link works until"
              value={expires ?? "Not recorded"}
            />
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Pickup and drop-off</CardTitle>
          <CardDescription>
            Where to collect from, where to take it, what the goods are and who to call.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground text-sm">
          <p>
            Not carried by this link yet. Until it is, the transport provider who sent you here has
            the details.
          </p>
        </CardContent>
      </Card>

      <Separator />

      <section className="text-muted-foreground flex flex-col gap-3 text-xs">
        <p className="text-foreground font-medium">If you need to quote this delivery</p>
        <dl className="flex flex-col gap-3">
          <div className="flex flex-col gap-0.5">
            <dt>Job</dt>
            <dd className="text-foreground font-mono break-all">{delivery.job_id}</dd>
          </div>
          <div className="flex flex-col gap-0.5">
            <dt>Assignment</dt>
            <dd className="text-foreground font-mono break-all">{delivery.assignment_id}</dd>
          </div>
        </dl>
        <p>
          Keep the message this link came in. There is no account and no password, so that message
          is the way back into this page.
        </p>
      </section>
    </Frame>
  );
}

/**
 * The milestone controls (SHIP-121).
 *
 * # Large touch targets, and the number is not a guess
 *
 * `h-14` is 56 CSS pixels. WCAG 2.2's enhanced target size is 44×44, Apple asks for 44 points and
 * Material for 48dp, and the *Done when* asks for large ones rather than compliant ones — because
 * of who is tapping: a driver standing outdoors, often in gloves, often one-handed, on a phone they
 * are holding above a load. Every button below is full width as well as tall, so the target is the
 * row rather than the label, and they are stacked with a gap so a mis-tap lands on nothing rather
 * than on the next milestone.
 *
 * # Every milestone is always offered
 *
 * See this file's header. The page has no idea where the delivery has got to and must not pretend
 * to: `Docs/07` §3 puts the decision on the platform, and the platform's answers here are useful
 * ones — a milestone the delivery has already passed is *absorbed* rather than refused (SHIP-112),
 * and one it has not reached yet comes back as `too_early` with copy that says to record the step
 * before it.
 *
 * # A tap is an action, and a second tap on the same button is a second action
 *
 * `lib/open.ts`'s `recordStep` owns that, through `lib/keys.ts`. What this component contributes is
 * only that a button in flight cannot be tapped again — which is a courtesy to the driver rather
 * than the guarantee, because the guarantee has to survive a reload and a component cannot.
 */
function Progress({ jobId, onDelivered }: { jobId: string; onDelivered: (at: string) => void }) {
  const [steps, setSteps] = useState<Record<string, StepState>>({});
  const [completing, setCompleting] = useState(false);

  // The driver's own words for the **next** milestone they record (SHIP-131a), and cleared the
  // moment one commits. A note belongs to one milestone — `reason` is "what a person should know
  // about *this* milestone" — so carrying it forward would attach a sentence about a locked gate to
  // the pickup that followed it, on the customer's timeline, with nothing on this page saying so.
  //
  // It is held here rather than in `Note` so that it survives the completion form opening: a driver
  // who types "nobody at reception" and then taps `Delivered` finds the words still there, beside
  // the three reasons.
  const [note, setNote] = useState("");
  const live = useRef(true);

  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);

  const at = useCallback((wire: string): StepState => steps[wire] ?? { kind: "idle" }, [steps]);

  // One place both the plain recording and the photographed one report into, because the four
  // outcomes and the "close the completion step" rule are the same for both — a second copy would be
  // a second place for a photographed delivery to be drawn differently from an exception one.
  const settle = useCallback((wire: string, attempt: Promise<Told>) => {
    setSteps((held) => ({ ...held, [wire]: { kind: "recording" } }));

    void attempt.then(
      (told) => {
        if (!live.current) return;
        setSteps((held) => ({ ...held, [wire]: stepFrom(told) }));
        if (told.kind === "recorded") {
          setCompleting(false);
          // Cleared only when the platform took it. A refusal leaves the words in the box, because
          // the driver is about to tap again and retyping a sentence they already wrote is the worst
          // thing this page could ask of somebody standing at a roller door.
          setNote("");
          // The page goes read-only on the milestone the platform *recorded*, and on its recorded
          // time rather than the local clock — a retry answered from an earlier attempt comes back
          // with that attempt's time, which is the honest one to show.
          if (told.recorded.milestone === "delivered") onDelivered(told.recorded.recorded_at);
        }
      },
      () => {
        // Both chains resolve rather than reject, exactly as openLink does. A browser that
        // surprises us leaves the button offering another go rather than spinning for ever.
        if (live.current) {
          setSteps((held) => ({ ...held, [wire]: { kind: "refused", refusal: "unexpected" } }));
        }
      },
    );
  }, [onDelivered]);

  const record = useCallback(
    (wire: string, evidence?: Evidence, completion?: Finished) =>
      settle(wire, recordStep(jobId, wire, { evidence, completion, note })),
    [jobId, note, settle],
  );

  const capture = useCallback(
    (wire: string, photograph: Blob, completion: Finished) =>
      settle(wire, capturePhotograph(jobId, wire, photograph, { completion, note })),
    [jobId, note, settle],
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>Record your progress</CardTitle>
        <CardDescription>
          Tap each step as you do it. If you have no signal, tap it anyway and try again when you
          have — the time recorded is the time you tapped, not the time it goes through.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {/*
          Above the buttons, because the note is typed before the tap: a field under four large
          targets is one a driver fills in after they have already recorded the thing it was about.
          It moves into the completion form when that opens, so it sits beside the three reasons on
          the path `Docs/04` §5 needs it on most.
        */}
        {completing ? null : <Note value={note} onChange={setNote} disabled={false} />}

        {MILESTONES.map((milestone) => (
          <Step
            key={milestone.wire}
            milestone={milestone}
            step={at(milestone.wire)}
            onRecord={() =>
              milestone.needsEvidence ? setCompleting(true) : record(milestone.wire)
            }
          />
        ))}

        {completing ? (
          <Completion
            recording={at("delivered").kind === "recording"}
            note={note}
            onNote={setNote}
            onPhotograph={(photograph, finished) => capture("delivered", photograph, finished)}
            onException={(reason, finished) =>
              record("delivered", { exception_reason: reason }, finished)
            }
            onCancel={() => setCompleting(false)}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}

/**
 * The driver's own words about whatever they record next (SHIP-131a).
 *
 * # The field the platform has always accepted and no client ever sent
 *
 * `MilestoneRecording.reason` — optional, 500 characters, "what a person should know about this
 * milestone that the milestone itself does not say" — has been in the published contract since
 * SHIP-111 and `internal/delivery` has stored it since. This page's `recordMilestone` took
 * `evidence`, `completion` and `recordedAt` and no note, and the Flutter client sent `reason` on a
 * job cancellation and on no milestone; meanwhile the **customer's tracking view rendered it**. So a
 * customer-facing surface could display a note that nothing in the product could write.
 *
 * # It says who reads it, because that changes what a person writes
 *
 * "The customer can see it" is the difference between a note meant for the customer and a note meant
 * for the transport provider's own records, and a driver who does not know which is writing neither.
 *
 * # Optional in the strong sense
 *
 * Leaving it empty sends no `reason` key at all — `lib/delivery.ts` trims and then omits — so a
 * driver who ignores this box records exactly what they recorded before it existed.
 *
 * `maxLength` is the contract's 500 rather than a layout choice: the column is `varchar(500)` and
 * the platform answers `validation_failed` past it, which is a poor way to learn about a sentence
 * you have already written.
 */
function Note({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-sm font-medium">Anything to add? (optional)</span>
      <textarea
        className="border-border bg-background focus-visible:border-ring focus-visible:ring-ring/50 min-h-16 w-full rounded-lg border px-3 py-2 text-base outline-none focus-visible:ring-3"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        maxLength={500}
        placeholder="Nobody at the gate, returning at four"
        disabled={disabled}
      />
      <span className="text-muted-foreground text-xs">
        Kept with the delivery. The customer can see it.
      </span>
    </label>
  );
}

/** What one answer means for the button that produced it. */
function stepFrom(told: Awaited<ReturnType<typeof recordStep>>): StepState {
  if (told.kind === "recorded") return { kind: "recorded", at: told.recorded.recorded_at };
  if (told.kind === "missing") {
    // The token has gone from this tab — a reload in a browser that refuses storage, or a link
    // opened from history. Reported as the read reports it rather than as a recording failure,
    // because nothing was refused: nothing was asked.
    return { kind: "refused", refusal: "invalid" };
  }
  return { kind: "refused", refusal: told.refusal };
}

/** One milestone, its button, and whatever the platform last said about it. */
function Step({
  milestone,
  step,
  onRecord,
}: {
  milestone: Milestone;
  step: StepState;
  onRecord: () => void;
}) {
  const recorded = step.kind === "recorded";
  const when = recorded ? dayFirst(step.at) : null;

  return (
    <div className="flex flex-col gap-1.5">
      <Button
        size="lg"
        variant={recorded ? "outline" : "default"}
        className="h-14 w-full justify-center text-base"
        disabled={step.kind === "recording"}
        onClick={onRecord}
      >
        {step.kind === "recording" ? "Recording…" : milestone.label}
      </Button>

      {recorded ? (
        <p className="text-muted-foreground text-xs" role="status">
          Recorded{when === null ? "" : ` at ${when}`}. Tap again if you need to record it a second
          time.
        </p>
      ) : step.kind === "refused" ? (
        <p className="text-destructive text-xs" role="alert">
          {REFUSALS[step.refusal].title}. {REFUSALS[step.refusal].body}
        </p>
      ) : (
        <p className="text-muted-foreground text-xs">{milestone.hint}</p>
      )}
    </div>
  );
}

/**
 * Finishing the delivery: who took it, what was left where, and the evidence (SHIP-122, SHIP-123).
 *
 * # `Docs/01` §4.4's field set, in the order a driver fills it in
 *
 * That paragraph requires four things of a delivered job: a recipient name, a delivery timestamp, a
 * delivery note, and photo proof. The timestamp is the tap. The other three are this form, and the
 * two text fields come **before** the camera because that is the order the moment happens in — the
 * driver is standing in front of the person who took it, and the photograph is of where they left it.
 *
 * # Both halves of the evidence rule are on one screen, and that is not a layout preference
 *
 * `Docs/01` §4.4 makes photo proof mandatory **and**, in the same paragraph, makes the exception path
 * "part of the same feature… built with it, not after", because "what must never happen is a driver
 * standing at a delivery point unable to finish the job — that converts a UI constraint into an
 * operational failure and a support call". A driver whose camera permission has just been denied is
 * exactly the person who must not have to go looking, and `camera_unavailable` is one of the three
 * reasons for precisely that case. So the camera is first and largest, and the three reasons are
 * under it rather than behind another tap.
 *
 * # The name and the note gate both paths, and the gate is local
 *
 * Neither button does anything until both fields are filled in, which is presentation rather than
 * authorisation: the platform refuses a `delivered` without them, naming the field, and this only
 * saves the driver a round trip and a lost photograph. **`Docs/07` §3's rule is about who may act,
 * not about whether a form is complete** — and the failure this avoids is real, because a capture
 * that reached the platform and was refused would have uploaded the photograph first.
 *
 * There is deliberately **no exception path for the two text fields**. `Docs/01` §4.4 gives three
 * reasons a photograph can be impossible and none for a name or a note, because a driver can always
 * write what they see: "unattended" is a recipient and "left at the front door" is a note.
 *
 * # The third field is a third thing, and conflating it with the second is the mistake to avoid
 *
 * `delivery_note` — "What did you do with it?" — is `Docs/01` §4.4's required fact about a
 * **delivered** job, refused on any other milestone and required on that one. `reason` (SHIP-131a)
 * is the optional note **any** milestone may carry, and `internal/delivery`'s own comment says in as
 * many words that the two are not the same field. They read alike on a form and they are not: one is
 * where the goods went, the other is anything a person should know that the milestone itself does
 * not say. Both are here because a driver finishing a delivery may have something to say about both.
 *
 * # The camera is a file input and not a media stream
 *
 * `capture="environment"` on `<input type="file" accept="image/*">` asks the handset to open the rear
 * camera directly. It costs no permission prompt of its own on iOS or Android — the picker is the
 * permission — it needs no `getUserMedia`, no video element, no canvas and no secure-context
 * fallback, and **a browser that does not honour the attribute degrades to the photo library**, which
 * is a working path rather than a dead end. A `MediaDevices` implementation would be more control and
 * four more failure modes on devices this application cannot test on.
 *
 * **Nothing here compresses it**: SHIP-130's on-device compression budget is the Flutter client's,
 * and the platform's size limit is server-side and comes back in the refusal message, which is where
 * a client learns the current one. A photograph over the limit is a `rejected` with the limit in it,
 * and the exception path is on the same screen.
 */
function Completion({
  recording,
  note,
  onNote,
  onPhotograph,
  onException,
  onCancel,
}: {
  recording: boolean;
  note: string;
  onNote: (value: string) => void;
  onPhotograph: (photograph: Blob, finished: Finished) => void;
  onException: (reason: string, finished: Finished) => void;
  onCancel: () => void;
}) {
  const camera = useRef<HTMLInputElement | null>(null);
  const [recipientName, setRecipientName] = useState("");
  const [deliveryNote, setDeliveryNote] = useState("");

  const finished = useMemo<Finished>(
    () => ({ recipientName: recipientName.trim(), deliveryNote: deliveryNote.trim() }),
    [recipientName, deliveryNote],
  );
  // Trimmed before it is judged, because the platform trims before it judges: a name of spaces is
  // refused as required there, and a form that accepted one here would upload a photograph first.
  const ready = finished.recipientName !== "" && finished.deliveryNote !== "";

  return (
    <div className="border-border flex flex-col gap-3 rounded-lg border p-3">
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium">Finish the delivery</p>
        <p className="text-muted-foreground text-xs">
          Who took it and what you did with it, then a photograph of where you left it — or a reason
          there is no photograph. Either one finishes the job.
        </p>
      </div>

      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">Who took the delivery?</span>
        <input
          className="border-border bg-background focus-visible:border-ring focus-visible:ring-ring/50 h-12 w-full rounded-lg border px-3 text-base outline-none focus-visible:ring-3"
          value={recipientName}
          onChange={(event) => setRecipientName(event.target.value)}
          maxLength={120}
          autoComplete="off"
          enterKeyHint="next"
          placeholder="Their name, or “unattended”"
          disabled={recording}
        />
      </label>

      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">What did you do with it?</span>
        <textarea
          className="border-border bg-background focus-visible:border-ring focus-visible:ring-ring/50 min-h-20 w-full rounded-lg border px-3 py-2 text-base outline-none focus-visible:ring-3"
          value={deliveryNote}
          onChange={(event) => setDeliveryNote(event.target.value)}
          maxLength={500}
          placeholder="Left with reception, signed for"
          disabled={recording}
        />
      </label>

      {/*
        Third, and above both evidence paths, because tapping either of them records immediately —
        the photograph fires on the picker's `change` and a reason button is the commitment. A note
        under them would be one a driver typed after the delivery had already gone.

        On the exception path this is what `Docs/04` §5 needs: the three reasons are a closed list so
        that the delivery-exception queue can group them, and this is the sentence that says which of
        the three it actually was. It goes beside the selection and never instead of it — the reason
        buttons still record `proof.exception_reason` whether or not a word is typed here.
      */}
      <Note value={note} onChange={onNote} disabled={recording} />

      <input
        ref={camera}
        type="file"
        accept="image/*"
        capture="environment"
        className="sr-only"
        aria-hidden="true"
        tabIndex={-1}
        onChange={(event) => {
          const photograph = event.target.files?.[0];
          // The value is cleared so that taking the same photograph twice — which a driver does
          // after a failed upload — fires `change` again. Without it the second attempt is silent.
          event.target.value = "";
          if (photograph !== undefined) onPhotograph(photograph, finished);
        }}
      />

      <Button
        size="lg"
        className="h-14 w-full justify-center text-base"
        disabled={recording || !ready}
        onClick={() => camera.current?.click()}
      >
        {recording ? "Finishing…" : "Take a photograph"}
      </Button>

      {ready ? null : (
        <p className="text-muted-foreground text-xs" role="status">
          Fill both in first. A delivered job records who took it and what you did with it, as well
          as the photograph.
        </p>
      )}

      <p className="text-muted-foreground text-xs">Or say why there is no photograph:</p>

      {proofExceptionReasonValues.map((reason) => (
        <Button
          key={reason}
          size="lg"
          variant="outline"
          className="h-14 w-full justify-center px-3 text-center text-base whitespace-normal"
          disabled={recording || !ready}
          onClick={() => onException(reason, finished)}
        >
          {EXCEPTION_COPY[reason]}
        </Button>
      ))}

      <Button
        size="lg"
        variant="ghost"
        className="h-12 w-full justify-center text-base"
        disabled={recording}
        onClick={onCancel}
      >
        Not yet
      </Button>
    </div>
  );
}

/**
 * The delivery is finished, and the page says so and offers nothing (SHIP-123).
 *
 * # "Read-only after" is a property of this page and not of the platform
 *
 * The link keeps opening, and the endpoints keep answering. That is deliberate: a driver's last act
 * was recording a delivery, and reopening the page to check what they recorded is a reasonable thing
 * to do — a link that stopped working would send them back to the transport provider for a new one
 * over nothing. What is gone is the controls, because there is nothing left to record.
 *
 * **It is presentation, and the platform is still the one deciding.** A milestone recorded on a
 * delivered job is refused by the transition guard exactly as it was before this page existed;
 * `Docs/07` §3's rule is that the app may hide or disable and the platform decides, and this is the
 * hiding half. The fact it hides on — `delivered_at` — is the platform's answer rather than
 * something this page remembered.
 */
function Finished({ at }: { at: string }) {
  const when = dayFirst(at);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Delivered</CardTitle>
        <CardDescription>
          {when === null
            ? "This delivery is recorded as delivered."
            : `Recorded as delivered at ${when}.`}
        </CardDescription>
      </CardHeader>
      <CardContent className="text-muted-foreground flex flex-col gap-2 text-sm">
        <p>
          There is nothing more to record. The transport provider and the customer can both see what
          you recorded, including the photograph or the reason there was none.
        </p>
        <p>
          If something about it was wrong, tell the transport provider who sent you this link — a
          delivery record is kept as it was made rather than corrected in place.
        </p>
      </CardContent>
    </Card>
  );
}
