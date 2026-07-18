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
| Licence, registration, insurance declarations | Required | Declaration in MVP; evidence process to be approved |
| Provider terms and goods policy | Required | Explicit acceptance |

**Decision required:** Obtain legal/insurance advice on which evidence must be collected and how often it must be renewed before launch.

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
