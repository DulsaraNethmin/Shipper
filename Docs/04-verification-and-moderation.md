# Shipper — Verification and Moderation Procedures

**Status:** Draft  
**Purpose:** Define a practical operating model for a safe MVP marketplace.

## 1. Verification principles

- Apply the lightest verification that safely supports the activity.
- Do not allow a provider to bid until baseline checks are complete.
- Use enhanced verification for higher-risk work if introduced.
- Treat verification as an eligibility decision, not a guarantee of delivery quality.
- Record every review, evidence item, decision, and expiry date.

## 2. Baseline customer verification

| Check | Requirement | Outcome |
|---|---|---|
| Email | Verify before publishing | Account may create drafts before verification |
| Phone | Verify before publishing | Reduces false contact details |
| Terms and goods declaration | Accept at job publication | Required for every job |

## 3. Baseline provider verification

| Check | Requirement | Review type |
|---|---|---|
| Email and phone | Required | Automated |
| Legal identity / business details | Required | Automated/manual as available |
| ABN where applicable | Required for businesses | Validation/manual |
| Address and service area | Required | Self-declared, review exceptions |
| Vehicle details | Required before bidding with a vehicle | Self-declared |
| Licence, registration, insurance | Required | Document image collected and reviewed by an administrator |
| Provider terms and goods policy | Required | Explicit acceptance |

**Decided — evidence is collected and reviewed manually.** Providers upload images of their licence, vehicle registration, and insurance certificate. An administrator reviews each by eye for obvious validity — correct document type, legible, not visibly expired, name matching the account — and records the decision.

This sits deliberately between the two alternatives. Declarations alone would leave Shipper holding no evidence at all, which is an uncomfortable position if something goes wrong during the pilot and an insurer or regulator asks what was checked. Integrating third-party identity and ABN verification would mean building and paying for automated checks before knowing whether provider supply materialises at all.

Manual review is affordable at pilot volume, produces a real evidence trail, and has a useful side effect: it shows exactly which checks are slow, ambiguous, or repetitive, which is the information needed to decide what is worth automating in Phase 2.

**Decision required:** which documents are *legally* required rather than merely prudent, and how often each must be renewed. Owner: legal and insurance advisers. This determines expiry tracking (`§5`) and retention obligations, and remains genuinely outside engineering's competence to settle. It is needed before pilot users are invited, not before build begins.

### 3.1 Evidence capture on mobile

The verification process itself is unchanged by the move to a mobile app; only the capture experience improves. Where document evidence is required:

- Providers photograph documents in-app rather than finding a scanner, which materially reduces onboarding drop-off at the point the funnel is weakest.
- Captured images upload directly to private object storage through short-lived pre-signed URLs, and are compressed on the device first.
- Verification images must **not** be written to the device photo library, and must be cleared from app storage once uploaded. Identity documents sitting in a camera roll are a privacy exposure the platform cannot control or revoke.
- The camera permission may be declined. A file-upload fallback must exist so that a refused permission never blocks verification outright.

## 4. Verification outcomes

- **Pending:** Provider cannot bid; information is incomplete or awaiting review.
- **Verified:** Provider may bid for supported job categories.
- **Restricted:** Provider may have limited access pending clarification or document renewal.
- **Rejected:** Provider may not bid; reason must be communicated where appropriate.
- **Suspended:** Provider access is disabled due to policy, safety, or repeated-performance concerns.

## 5. Moderation queues

Administrators should receive separate queues for:

1. New or changed provider verification submissions.
2. Reported jobs or messages.
3. Jobs flagged by goods/category rules.
4. Delivery exceptions: overdue pickup, delayed delivery, failed proof of delivery.
5. Cancellations after award.
6. Open disputes.
7. Expiring or expired provider verification records.

## 6. Moderation process

1. Receive a report, automatic flag, or support request.
2. Review the affected job, user profile, communications, and history.
3. Collect only evidence relevant to the decision.
4. Select outcome: no action, warning, content removal, job cancellation, restriction, suspension, or escalation.
5. Notify affected users with a clear, non-sensitive reason where appropriate.
6. Record the decision, actor, timestamp, reason, and evidence reference.

Urgent safety, suspected criminal activity, or credible threats must follow an escalation procedure approved by legal and operations leadership.

## 7. Dispute procedure

### Intake

Capture job, complainant, category, description, desired outcome, time of event, and evidence.

### Investigation

Review the job listing, awarded bid, messages, status history, proof of delivery, and relevant verification details. Give the other party a reasonable opportunity to respond.

### Outcome

Possible outcomes:

- Delivery completed as agreed.
- Delivery issue acknowledged; parties directed to resolve externally.
- Job cancelled / failed delivery recorded.
- User warning, restriction, or suspension.
- Referral to legal, insurer, or authorities where required.

For MVP, Shipper records outcomes and supports resolution; it does not promise compensation or adjudicate liability unless the final business model and terms expressly provide for it.

## 8. Service targets to approve

| Type | Proposed initial target |
|---|---|
| Urgent safety report acknowledgement | Same business day |
| Standard support first response | Within 2 business days |
| Verification review | Within 3 business days |
| Dispute acknowledgement | Within 2 business days |
| Dispute target resolution | Within 10 business days, subject to evidence |

## 9. Required internal controls

- Least-privilege administrative access.
- Two-person review for permanent account suspension where practical.
- No deletion of audit history by ordinary administrators.
- Private storage of verification evidence.
- Documented escalation contact for legal, security, and urgent safety incidents.
