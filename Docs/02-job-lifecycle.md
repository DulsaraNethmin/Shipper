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
| Awarded / Driver assigned | En route to pickup | Provider/driver records progress |
| En route to pickup | Picked up | Pickup confirmed |
| Picked up | In transit | Transport begins; can be automatic presentation change |
| In transit | Delivered | Proof-of-delivery data recorded |
| Delivered | Completed | Customer confirms or no dispute within agreed window |
| Awarded through Delivered | Disputed | Eligible user/admin opens a supported dispute |
| Disputed | Completed | Admin resolves dispute with delivery accepted |
| Disputed | Cancelled | Admin resolves as cancelled/failed delivery |

## 3. Transition controls

- Only the customer can award a job, and the selected bid must be active.
- Awarding a job atomically marks one bid accepted and all others closed.
- Delivery-status updates must be made only by the awarded provider, their assigned driver, or an administrator acting with an audit reason.
- “Delivered” requires a timestamp, recipient name or delivery note, and at least one form of proof if the policy requires it.
- Core job details cannot change after award without a documented change process.
- Cancellation after pickup requires support intervention unless the parties agree and the workflow is explicitly supported.
- A dispute freezes automatic completion until an administrator resolves it.

## 4. Bidding state

Each bid has one of these statuses: Draft, Submitted, Countered, Accepted, Rejected, Withdrawn, Expired, or Superseded.

- A customer counter-offer supersedes the prior provider offer.
- A provider counter-offer supersedes the prior customer offer.
- Only the latest valid offer can be accepted.
- Bid history remains visible to the customer, bidding provider, and administrators.

## 5. Exception scenarios

| Scenario | Required handling |
|---|---|
| Provider fails to arrive | Customer can report no-show; admin can cancel or re-open job |
| Goods differ from listing | Provider records issue; job may be paused, cancelled, or disputed |
| Customer unavailable at pickup/delivery | Provider records failed attempt; support workflow begins |
| Delivery is late | Status remains active; notify customer; support can open a case |
| Goods damaged or missing | Job becomes disputed; evidence is collected |
| Driver loses portal link | Regenerate or revoke job-scoped access without exposing other jobs |

## 6. Decisions required

- Completion confirmation window: recommended 48 hours after delivered.
- Whether a provider can update “Delivered” without photo/signature proof.
- Cancellation fee policy and responsibility for no-shows.
- Whether an awarded job may return to Open if its provider cancels.
- Maximum time a job can remain Open before expiry.
