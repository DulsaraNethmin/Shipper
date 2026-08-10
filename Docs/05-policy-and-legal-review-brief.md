# Shipper — Policy and Legal-Review Brief

**Status:** Draft for professional review  
**Purpose:** Brief legal, privacy, and insurance advisers on the policy areas required before public launch.

## 1. Product summary

Shipper is an Australian online marketplace that connects customers needing lawful goods transported with independent transport providers. Customers publish jobs; providers bid; customers select a provider; the provider performs the delivery and records milestones/proof of delivery.

The MVP is intended to facilitate matching and communication. It does not initially collect customer funds, hold payment, or guarantee transport performance.

Customers and providers use a **mobile application distributed through the Apple App Store and Google Play**. Administration is a web application, and the assigned driver receives a job-scoped web link requiring no account. Advisers should note that app-store distribution introduces a further set of obligations owed to Apple and Google, and that Shipper's public conduct is subject to their review as well as to Australian law.

## 2. Documents to prepare or review

- Customer terms of use.
- Provider agreement.
- Privacy policy and collection notice.
- Acceptable-use and prohibited-goods policy.
- Cancellation and dispute policy.
- Provider verification and document-retention policy.
- Cookie/analytics notice, if applicable.
- Internal incident-response and law-enforcement request procedure.

### Additional materials required by app-store distribution

Shipper's customer and provider experience ships as a mobile application, which introduces disclosure obligations owed to Apple and Google in addition to those owed to users and regulators. These are submission blockers, not launch polish.

- **Apple privacy labels** and the **Google Play data-safety declaration**, each describing what personal information is collected, why, and whether it is linked to identity. Both must be consistent with the privacy policy; a mismatch is a common rejection cause.
- **Permission purpose strings** shown at the camera and notification prompts, in plain language.
- A **publicly reachable privacy-policy URL**, required at submission and before any pilot build is distributed.
- **In-app account deletion**, described below.
- Age rating and encryption/export-compliance declarations, required for a later public listing.

## 3. Questions for legal and insurance advisers

### Marketplace model

- How should Shipper’s role be described to avoid implying that it is the carrier, employer, insurer, or guarantor of a provider?
- What disclosures are required when users contract directly with one another?
- What terms are needed for platform fees, if introduced later?

### Transport and goods

- Which transport, dangerous-goods, consumer, state, and territory obligations affect the intended MVP?
- Which goods must be prohibited or require special verification?
- What provider credentials, registrations, licences, and insurance evidence should be required?

### Consumer protection and disputes

- Which cancellation, refund, misrepresentation, and service-failure obligations apply to Shipper?
- What dispute process and limitation wording is appropriate?
- What communications must be retained for a complaint or legal claim?

### Privacy and security

- What notices, consent, retention, access, correction, and deletion processes are needed for personal information?
- How should precise pickup/drop-off addresses, proof-of-delivery photos, identity evidence, and driver portal links be handled?
- Is cross-border cloud processing permitted under the proposed operating model and what disclosures are needed?
- **Account deletion versus audit retention.** A working model has been adopted — see §3.1 below. Advisers are asked to confirm the split and, specifically, to set the retention period.
- **Device-held data.** What may the app cache on the device — addresses, contact details, proof photos, queued offline milestones — and what must be cleared on sign-out, on account deletion, or after upload? Note that push tokens and device identifiers are themselves personal information.
- **Push notification content.** Notifications appear on a lock screen visible to anyone holding the phone. What may a notification say, given that pickup addresses and customer names are sensitive?
- Does Shipper's use of Firebase Cloud Messaging for push, and the resulting data flow, require specific disclosure under the cross-border processing position above?

### Worker classification and insurance

- How should provider and driver independence be reflected in product language and agreements?
- Which insurance products should Shipper hold, and what insurance evidence should providers provide?

### 3.1 Account deletion and audit retention — adopted model

Apple requires any app offering account creation to offer in-app account deletion. Shipper simultaneously needs an immutable audit history and must retain records relevant to a complaint, insurance claim, or legal proceeding. These pull in opposite directions, and the resolution determines the data model — so a working position has been adopted rather than left open.

**The principle: delete the person, retain the transaction.**

| Irreversibly deleted | Retained, with the user replaced by a stable pseudonym |
|---|---|
| Profile details and contact information | Job records |
| Verification documents and identity evidence | Bid and negotiation records |
| Push tokens and device identifiers | Status transition history |
| Message content | Audit log entries |
| Proof-of-delivery images attributable to the person | Proof-of-delivery metadata and timestamps |

A deleted user becomes a stable pseudonymous identifier, so the counterparty's own history — a customer's record of who carried their goods, a provider's record of completed work — stays coherent. Neither party's history is silently rewritten because the other left.

**Three operational rules follow:**

- **Deletion is initiated in-app but need not complete instantly.** Apple permits a reasonable delay and permits retaining what law requires. Shipper confirms the request in-app, executes within 30 days, and tells the user when it will complete.
- **Deletion during an active job is deferred, not refused.** A request made between Awarded and Delivered is queued until the job closes, and the user is told why. Erasing a party mid-delivery would strand the counterparty.
- **Ordinary administrators cannot execute deletion**, consistent with the audit controls in `04` §9.

**Decision required:** the retention period for pseudonymised transaction records. Owner: legal. This is the one genuinely open element — the split above is a design decision, but how long the retained side persists is a question of Australian limitation periods, tax and business record-keeping obligations, and insurance requirements. Needed before account deletion is implemented.

## 4. Policy positions

These are the draft positions to be confirmed with advisers. `07` §5 cites this section for the push-content rule.

| Policy area | Draft position | Approval needed |
|---|---|---|
| Service role | Marketplace facilitator, not transport carrier | Legal |
| Payments | Outside Shipper for MVP | Business / Legal |
| Prohibited goods | Illegal, living, dangerous/specialist goods unless expressly supported | Legal / Operations |
| Provider eligibility | Baseline declarations plus verified identity/business/vehicle process | Legal / Insurance |
| Disputes | Shipper investigates platform-policy issues; no compensation commitment in MVP | Legal / Business |
| Data retention | Retain operational/audit data for defined lawful periods; minimise verification evidence | Privacy / Legal |
| Tracking link | Restricted to one assigned job; revocable and time-limited | Security / Privacy |
| Account deletion | Delete the person, retain the transaction under a stable pseudonym; execute within 30 days; defer during an active job — see §3.1 | Legal / Privacy |
| Provider verification | Document evidence collected and reviewed manually in MVP; no third-party verification integration — see `04` §3 | Legal / Insurance |
| Device data | Cache the minimum needed to work offline; clear on sign-out and deletion; never write verification or proof images to the device photo library | Privacy / Security |
| Push content | Notifications carry no address, goods description, or full customer name; detail is revealed only after the user opens the app | Privacy / Product |

## 5. Launch checklist

Before public launch, obtain written approval for:

- Terms and privacy materials.
- Exact prohibited-goods policy.
- Provider verification and insurance requirements.
- Cancellation and dispute workflow.
- Customer/provider communication and data-sharing rules.
- Incident/escalation process.
- Marketing claims about price, reliability, verification, and delivery guarantees.
- App-store privacy disclosures, permission purpose strings, and the account-deletion position.

This brief does not provide legal advice. It identifies the issues that must be reviewed by appropriately qualified Australian advisers.
