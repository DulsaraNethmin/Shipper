# Shipper — Job Lifecycle and Status Rules

**Status:** Draft  
**Purpose:** Establish one shared interpretation of the job state for product, operations, and architecture.

## 1. Status model

| Status | Meaning | Primary actor |
|---|---|---|
| Draft | Customer is preparing the job; not visible to providers | Customer |
| Open | Published and eligible for bids | Customer / Platform |
| Negotiating | One or more active bids or counter-offers exist; job remains open to eligible bids | Customer / Provider |
| Awarded | Customer has accepted one provider bid; provider commitment exists | Customer |
| Driver assigned | Provider has nominated a driver or self-assigned | Provider |
| En route to pickup | Driver is travelling to pickup | Provider / Driver |
| Picked up | Provider confirms goods collection | Provider / Driver |
| In transit | Goods are being transported | Provider / Driver |
| Delivered | Provider records successful delivery and proof | Provider / Driver |
| Completed | Customer confirms, or the confirmation window expires without dispute | Platform / Customer |
| Cancelled | The job ends before completion | Customer / Provider / Admin |
| Disputed | A customer, provider, or admin has raised an unresolved issue | Customer / Provider / Admin |

“Negotiating” is a useful presentation status. Technically, the job remains available for eligible bids unless the customer closes it or awards a bid.

## 2. Permitted transitions

| From | To | Conditions |
|---|---|---|
| Draft | Open | Required job details valid; customer verified |
| Draft | Cancelled | Customer abandons draft |
| Open | Negotiating | First bid or counter-offer submitted |
| Negotiating | Open | All active bids expire, are withdrawn, or are rejected |
| Open / Negotiating | Awarded | Customer accepts one currently valid bid |
| Open / Negotiating | Cancelled | Customer cancels before award; admin may intervene |
| Awarded | Driver assigned | Provider assigns a driver |
| Awarded / Driver assigned | Open | Provider cancels before pickup; all bids closed and the cancellation recorded against the provider — see §6.2 |
| Awarded / Driver assigned | En route to pickup | Provider/driver records progress |
| En route to pickup | Picked up | Pickup confirmed |
| Picked up | In transit | Transport begins; can be automatic presentation change |
| In transit | Delivered | Proof-of-delivery data recorded, or a reasoned exception recorded |
| Delivered | Completed | Customer confirms, or 72 hours pass with no dispute — see §6.1 |
| Open / Negotiating | Cancelled | Job expires unclaimed — see §6.3. The deadline is the job's own and does not wait for its last offer to lapse |
| Awarded through Delivered | Disputed | Eligible user/admin opens a supported dispute |
| Disputed | Completed | Admin resolves dispute with delivery accepted |
| Disputed | Cancelled | Admin resolves as cancelled/failed delivery |

**This table is authoritative for the transition guard.** Two consequences are easy to miss and both are deliberate. `Awarded → En route to pickup` is permitted directly, so `Driver assigned` is skippable — a provider who is driving the job themselves need not nominate anyone. And `Picked up → In transit` may be applied by the platform as a presentation change rather than by an actor. Where `01` §4.4 numbers the five recordable milestones as a sequence, it is describing what a driver records, not constraining what the guard accepts.

## 3. Transition controls

- Only the customer can award a job, and the selected bid must be active.
- Awarding a job atomically marks one bid accepted and all others closed.
- Delivery-status updates must be made only by the awarded provider, their assigned driver, or an administrator acting with an audit reason.
- “Delivered” requires a delivery timestamp, a recipient name, a delivery note, and photo proof — or, in place of the photo, a reasoned exception recorded under §6.4. See `01` §4.4, which is authoritative for the field set.
- Core job details cannot change after award without a documented change process.
- Cancellation after pickup requires support intervention unless the parties agree and the workflow is explicitly supported.
- A dispute freezes automatic completion until an administrator resolves it.

### 3.1 Offline and out-of-order updates

The status model above is platform truth and does not change because the client is a mobile app. What changes is that transitions now arrive **late, repeated, or out of sequence**, because drivers record milestones in places with no signal and the device retries when it reconnects.

- Every state-changing request carries a **client-generated idempotency key**. Replaying a request with a key already seen returns the original outcome rather than creating a second record.
- A transition carries **two timestamps**: when the actor recorded it and when the platform accepted it. The first is what the customer sees; the second is what audit and support rely on.
- A queued update that arrives after a later transition has already been recorded must be **absorbed, not rejected as an error**. If a driver's offline "Picked up" arrives after "In transit" is already recorded, the platform accepts the historical fact without moving the job backwards.
- A queued update that contradicts an administrative action loses. If a driver records "Delivered" offline while an administrator cancels the job, the cancellation stands, the attempt is retained in history, and the app must show the driver what happened rather than silently discarding their work.
- Transitions are never applied by the client. The app displays optimistic local state, clearly marked as pending, and reconciles to whatever the platform returns.

**Decided — unsynced milestone handling.** Escalation is tiered, because a 24-hour operations alert is a backstop, not a mechanism. By the time operations hears about an unsynced update, it should already be rare.

| Elapsed unsynced | Response |
|---|---|
| Immediately | The app shows a persistent indicator of how many updates are pending. The user is never left guessing whether their work was recorded |
| 4 hours | The provider is nudged, so someone who can find signal knows to |
| 24 hours | Operations alert. The job is treated as at risk and enters the delivery-exception queue (`04` §5) |

The customer sees the last confirmed milestone throughout and is never shown a pending client-side state as though it were fact.

## 4. Bidding state

Each bid has one of these statuses: Draft, Submitted, Countered, Accepted, Rejected, Withdrawn, Expired, or Superseded.

- A customer counter-offer supersedes the prior provider offer.
- A provider counter-offer supersedes the prior customer offer.
- Only the latest valid offer can be accepted.
- Bid history remains visible to the customer, bidding provider, and administrators.

**`Countered` is a retained synonym for `Superseded`, and the platform writes only `Superseded`.** Read closely, the first two rules above describe one transition from its two ends — an offer "answered with a different price or timing" is the same event as an offer "displaced by a counter from either party", and there is no third thing either could mean. SHIP-87 built it that way, and SHIP-96 confirmed it from the other side: rendering the chain to the customer, the bidding provider and an administrator, nothing acts on the distinction, so a second status would be a value every client had to branch on to no effect.

`Countered` stays in the list rather than being deleted, for two reasons that are both mechanical. `Docs/10` §3.4 pairs the Go constant list against `ck_bids_status` in both directions, so removing the value means a migration and an edit to another ticket's fixtures; and a value absent from a client's enumeration decodes as unknown on the day somebody starts writing it, which is a worse failure than an unused constant. **The trigger for writing it would be a ticket needing to distinguish "displaced because the other party answered" from some other way of being displaced — and there is no other way today.**

`contracts/statuses.yaml` is the source this vocabulary is generated from for Go, Dart and TypeScript. It carries the same decision against the `Countered` value, and the two are edited together.

## 5. Exception scenarios

| Scenario | Required handling |
|---|---|
| Provider fails to arrive | Customer can report no-show; admin can cancel or re-open job |
| Goods differ from listing | Provider records issue; job may be paused, cancelled, or disputed |
| Customer unavailable at pickup/delivery | Provider records failed attempt; support workflow begins |
| Delivery is late | Status remains active; notify customer; support can open a case |
| Goods damaged or missing | Job becomes disputed; evidence is collected |
| Driver loses portal link | Regenerate or revoke job-scoped access without exposing other jobs |
| Driver records milestones with no signal | Updates queue on the device and sync later under §3.1; the job is not treated as stalled until the unsynced threshold passes |
| Provider's app build is below the supported floor | Launch-time version gate blocks use until updated; the provider must still be reachable by support and email meanwhile |

## 6. Decisions taken

### 6.1 Completion confirmation window — 72 hours

A job recorded as Delivered auto-completes 72 hours later if no dispute is raised.

The earlier proposal was 48 hours. It was extended because a Friday-evening delivery would otherwise auto-complete on Sunday, when few customers are looking — the window would expire precisely when it was least able to be used. Since Shipper holds no money in the MVP, Completed carries no financial consequence, so a longer window costs the provider nothing while giving the customer a real opportunity to object. Expect to shorten this once payment flows exist and providers have a stake in being marked done.

**A job delivered through the proof-exception path is not an exclusion from this window.** It auto-completes on the same 72-hour rule as a job delivered with a photograph, and needs no additional customer confirmation. That is X-6, decided 14 August 2026 and previously carried in §7.

The question stayed open for eight waves because it looked like a trade between closing the job and having a person look at it. **It is not a trade, and the fact that settled it arrived with SHIP-117: an exception-completed job now enters the moderation queue.** Human review happens either way, so the two are not exclusive — blocking auto-completion would add no review at all. It would only strand the job in `Delivered` when the customer never acts, which is the single outcome this window exists to prevent.

The rest follows from what is already written. `Completed` carries no financial consequence while Shipper holds no money, which is this section's own argument for a window rather than a confirmation; and a reasoned exception is evidence rather than the absence of it, which is the reading `Docs/01` §4.4 and `contracts/statuses.yaml` both take. A dispute still stops the clock exactly as it does for a photographed delivery — that is §6.1's rule and the exception path does not touch it.

**SHIP-119 implements the task; this settles what it implements**, and it needs no column that does not already exist.

### 6.2 Provider cancellation after award — the job returns to Open

**Between Awarded and Picked up**, a provider cancellation returns the job to Open rather than ending it.

- All prior bids are **closed, not restored**. They were priced against a date and an availability that have since moved, so treating them as still valid would be wrong.
- Providers who previously bid are **notified that the job is open again**. They have already shown interest and assessed the work, so this is the cheapest supply the job will ever get.
- The cancellation is **recorded against the provider**. It carries no penalty in the MVP, but it is the reliability signal that Phase 2 reputation will be built from, and it cannot be reconstructed later if it is not captured now.

**After Picked up this does not apply.** The goods are in someone's vehicle, and that is a support case under §3, not a state transition.

### 6.3 Job expiry — the earlier of 14 days or the pickup date passing

A job leaves Open at whichever comes first:

- **14 days** after publication, or
- the moment its own **pickup date passes**.

The second is the operative rule; a job whose pickup window has gone is dead regardless of how recently it was posted. The 14-day limit is only a backstop for jobs with distant dates. Stale listings make a young marketplace look abandoned and waste the attention of the providers it most needs to keep.

The customer is warned 48 hours before expiry and can extend in one action.

### 6.4 Photo proof — mandatory, with a reasoned exception path

Delivered requires photo proof. See `01-mvp-product-requirements.md` §4.4 for the exception path, which must be built alongside it.

### 6.5 Unsynced milestones — tiered escalation

See §3.1.

## 7. Decisions still required

- Cancellation fee policy and responsibility for no-shows. Deferred: no payment flows exist in the MVP, so there is nothing to charge against.

**The proof-exception auto-completion question has left this list.** It was the second bullet here from the first draft until 14 August 2026, when X-6 was decided: an exception-completed job auto-completes on §6.1's ordinary 72-hour rule. The decision and its reasoning are recorded in §6.1, where the rule they qualify is, rather than here — a decision taken is not a decision required, and leaving it here as a struck bullet would leave the reader who reaches §6.1 first with no answer.
