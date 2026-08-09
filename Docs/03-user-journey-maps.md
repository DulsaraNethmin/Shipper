# Shipper — User Journey Maps

**Status:** Draft  
**Purpose:** Describe the end-to-end experience that the MVP must support.

## 1. Customer journey

| Stage | Customer goal | Customer action | Shipper responsibility | Risk / opportunity |
|---|---|---|---|---|
| Discover | Understand whether Shipper is suitable | Reviews supported goods, locations, and service promise | Clearly explain service scope and prohibited goods | Prevent unsuitable jobs early |
| Register | Create a trusted account | Signs up and verifies contact details | Simple verification and privacy explanation | Drop-off during onboarding |
| Create job | Describe the delivery accurately | Adds locations, dates, goods, dimensions, budget, and handling needs | Validate fields and flag prohibited/unclear goods | Poor job data leads to poor bids |
| Receive bids | Find a suitable provider | Reviews bids and provider capability | Present comparable price, timing, vehicle, profile, and verification state | Avoid a price-only decision |
| Negotiate | Clarify terms | Messages or counter-offers | Preserve job-linked conversation and offer history | Reduce off-platform activity |
| Award | Commit to a provider | Accepts a bid | Lock winning offer and notify parties | Prevent double awards |
| Track | Know delivery progress | Views milestones and proof of delivery | Provide timely, clear updates | Build confidence without GPS MVP |
| Complete / resolve | Confirm outcome or report a problem | Confirms delivery or opens dispute | Collect evidence and route to support | Support must be transparent |

## 2. Provider journey

| Stage | Provider goal | Provider action | Shipper responsibility | Risk / opportunity |
|---|---|---|---|---|
| Join | Access relevant work | Creates account and profile | Explain requirements and verification | Balance safety and onboarding friction |
| Verify | Become eligible to bid | Adds business, vehicle, and declaration details | Review eligibility and communicate status | Establish marketplace trust |
| Find jobs | Identify worthwhile delivery work | Filters and views eligible jobs | Match on location, vehicle, availability, category | Early matching quality is critical |
| Bid | Submit a viable offer | Sets price, timing, and conditions | Validate bid and show job context | Avoid misleading offers |
| Negotiate | Reach workable agreement | Exchanges messages/counter-offers | Keep all negotiation tied to job | Set response/expiry expectations |
| Fulfil | Perform delivery efficiently | Assigns driver; updates milestones; records proof | Give mobile-friendly workflow and restricted driver access | Status updates must be easy |
| Close | Build future opportunity | Completes job and resolves exceptions | Retain reliable history; ratings later | Repeat work and reputation |

## 3. Assigned-driver journey

The assigned driver stays on the **web**. This is a deliberate exception to the mobile-first direction, not an oversight: assigned drivers are frequently subcontracted, casual, or working a single job, and requiring an app install would abandon the low-friction access this journey was designed around. The portal must therefore work well in a mobile browser on a wide range of devices.

| Stage | Driver action | Required experience |
|---|---|---|
| Receive assignment | Opens restricted portal link in a mobile browser | No general account and no app install required for MVP, if link security is sufficient |
| Prepare | Views pickup/delivery, goods notes, contact guidance | Only data needed for that job |
| Collect | Updates en-route and picked-up milestones | Fast, mobile-first status controls; large touch targets usable with one hand |
| Deliver | Updates delivered and captures proof | Proof requirements, timestamp, recipient/details; photo capture through the browser camera |
| Finish | Portal access ends or becomes read-only | No access to unrelated commercial data |

The provider, who does have the app, retains the richer fulfilment experience: offline milestone queueing, native camera capture, and push updates. The driver portal is intentionally the leaner of the two.

## 4. Administrator journey

| Stage | Admin goal | Required capability |
|---|---|---|
| Monitor | Detect risky or stuck activity | Queue/dashboard for verification, reports, disputes, and delayed jobs |
| Support | Answer an inquiry quickly | Search across users, jobs, bids, messages, and delivery history |
| Moderate | Remove unsafe or prohibited activity | Unpublish job, warn/restrict user, preserve reason and evidence |
| Resolve | Reach a fair documented outcome | Dispute workflow, internal notes, outcome, and notifications |
| Learn | Improve marketplace operations | Trend reporting for completion, cancellations, abuse, and support volume |

## 5. Experience principles

- Explain trust and policy requirements before users invest time.
- **Design for the phone first.** Customer and provider journeys happen in a native app used in the field — in a yard, at a roller door, in a cab. Admin work stays desk-oriented and should not be compromised to match.
- Make important state changes visible, timestamped, and understandable.
- **Never let poor connectivity block the job.** A driver or provider must be able to record what happened and move on; syncing is the platform's problem, not theirs.
- **Ask for a device permission only at the moment its value is obvious** — the camera when proof is being captured, notifications after the first bid arrives. Never on first launch.
- Never expose unnecessary personal, commercial, or location data, including data cached on the device.
- Give users a clear route to help when the delivery does not proceed as expected.
