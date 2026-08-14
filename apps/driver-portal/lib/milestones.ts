/**
 * The milestones a driver records, and the order they normally happen in (SHIP-121).
 *
 * # This list is written out rather than generated, and that is a decision with a cost
 *
 * `contracts/statuses.yaml` generates three enumerations into this application —
 * `JobStatus`, `BidStatus` and `ProofExceptionReason` — and `lib/statuses.gen.ts` is where they
 * land. **Milestones are not among them.** `internal/delivery/milestone.go` declares them by hand
 * and says why in its own words: they are "a different list" from `jobs.Status`, with five values,
 * a different membership and `ck_milestones_milestone` behind them, and SHIP-56a "has no opinion
 * about this one".
 *
 * So the choice was to add a fourth enumeration to `contracts/statuses.yaml` — a **shared file**
 * another lane may have open in the same wave, and a change that would also rewrite
 * `internal/delivery`'s hand-written type — or to write four strings here with the authority named
 * beside them. The second is what this file is. `Docs/11` §9 carries it as a recommendation with a
 * trigger: the second surface that needs the milestone vocabulary is the one that should pay for
 * the generator, and until then a fourth copy is a fourth thing to keep in step.
 *
 * **What keeps it honest meanwhile is the platform, not this file.** A wire form this application
 * invented is refused by `internal/delivery`'s decoder with a message naming the four it accepts,
 * so a drift is a `422` on the first tap rather than a silent mis-record. And `make verify`'s
 * SHIP-121 checks record every value below against the running service.
 *
 * # Four, not five
 *
 * `driver_assigned` is missing deliberately and is not an omission. `internal/delivery`'s
 * `Recording.problems` refuses it by name — "a driver is put on a job through its own endpoint, not
 * recorded as a milestone" — because the provider creates the assignment and the driver's link is
 * the *result* of it. A driver holding this page is, by construction, already assigned.
 *
 * # Not a sequence, despite `Docs/01` §4.4 numbering one
 *
 * The order below is the order a delivery normally runs in and the order the buttons read in. It is
 * **not** a chain the page may walk or enforce. `Docs/02` §2 permits `Awarded → En route to pickup`
 * directly, a repeated `en_route_to_pickup` is an ordinary failed pickup attempt (`Docs/02` §5), and
 * a late one that arrives after the delivery has moved on is absorbed rather than refused
 * (SHIP-112). **Which milestones are permitted from here is the platform's answer on every tap**,
 * which is `Docs/07` §3 exactly: the page may present, and the platform decides.
 */

import { ProofExceptionReason } from "./statuses.gen.ts";

/** One milestone a driver can record from this page. */
export interface Milestone {
  /** The wire form, which is what `POST /v1/driver/jobs/{id}/milestones` takes. */
  wire: string;

  /** What the button says. Imperative, because the driver is telling the platform what they did. */
  label: string;

  /** The line under it, for a driver deciding which of four buttons this moment is. */
  hint: string;

  /**
   * Whether recording this milestone requires evidence — a photograph, or a reason there is none
   * (`Docs/01` §4.4, and `CLAUDE.md` calls it an invariant).
   *
   * True of exactly one of the four, and the page uses it to decide whether a tap records
   * immediately or opens the completion step. It is **not** the page enforcing the rule: the
   * platform refuses a `Delivered` with neither and `000605`'s deferred constraint trigger refuses
   * the row whoever wrote it. What this flag buys is a driver who is asked for the photograph
   * *before* the refusal rather than after it.
   */
  needsEvidence: boolean;
}

/**
 * The four, in the order `Docs/01` §4.4 numbers them, less the one with an endpoint of its own.
 *
 * Exported as a readonly tuple so a page renders the list rather than four hard-coded buttons —
 * which is what makes adding a fifth a one-line change here and nothing in the component.
 */
export const MILESTONES: readonly Milestone[] = [
  {
    wire: "en_route_to_pickup",
    label: "On my way to pick up",
    hint: "You have set off for the pickup address.",
    needsEvidence: false,
  },
  {
    wire: "picked_up",
    label: "Picked up",
    hint: "The goods are loaded and you have left the pickup.",
    needsEvidence: false,
  },
  {
    wire: "in_transit",
    label: "In transit",
    hint: "You are on the road to the drop-off.",
    needsEvidence: false,
  },
  {
    wire: "delivered",
    label: "Delivered",
    hint: "Handed over. Needs a photograph, or a reason there is none.",
    needsEvidence: true,
  },
];

/** The milestone with this wire form, or undefined if this application does not know it. */
export function milestoneFor(wire: string): Milestone | undefined {
  return MILESTONES.find((m) => m.wire === wire);
}

/**
 * The three reasons a photograph can be impossible, as a driver reads them (`Docs/01` §4.4).
 *
 * # Why this is here and not in `lib/statuses.gen.ts`
 *
 * The generated file carries `proofExceptionReasonLabels`, and its own comment says what those are:
 * "the exact name `Docs/01` §4.4 uses… not free-form copy", which renders `recipient_objected` as
 * `recipient_objected`. That is right for an audit entry and for anywhere a driver's page and a
 * support queue must be reading about the same thing. It is not a button a driver taps in the dark.
 *
 * So this is the sentence rather than the name, and it is **verbatim from `Docs/01` §4.4** rather
 * than reworded, for the reason that comment gives one level up: the three clauses in the document
 * are what operations, moderation and the driver are all looking at, and a paraphrase here would be
 * a fourth wording of the same three facts.
 *
 * **Keyed by the generated union**, so a fourth reason added to `contracts/statuses.yaml` is a
 * TypeScript error in this file rather than a button that never appears. That is the same shape
 * `REFUSALS` uses in `lib/delivery.ts` and it is the reason both are records rather than switches.
 */
export const EXCEPTION_COPY: Readonly<Record<ProofExceptionReason, string>> = {
  [ProofExceptionReason.RecipientObjected]: "The recipient objects to being photographed",
  [ProofExceptionReason.CameraUnavailable]: "The camera permission is denied or unavailable",
  [ProofExceptionReason.LocationUnsafe]: "The delivery point is unlit or unsafe to photograph",
};
