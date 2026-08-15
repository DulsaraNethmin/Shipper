# Shipper — API error codes

**Generated. Do not edit.** Regenerate with:

```
go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update
```

Source: the registry in `internal/httpx/codes.go`, plus every code each domain declares
with `httpx.RegisterCode`. A merge conflict in this file is resolved by regenerating it,
never by choosing a side — choosing a side drops a domain's codes silently.

## How to use this list

Every failure the platform returns has the same shape, and `error.code` is the only part of
it a client may branch on (`Docs/10` §4.4). Messages are reworded, translated and made
friendlier; a store build that switched on message text would break on a copy edit, with no
over-the-air path to fix it.

**Protocol codes** can be returned by any endpoint, because they come from the transport, the
middleware, or `net/http` itself rather than from a business rule. Handle them once, centrally.

**Domain codes** are named `<domain>_<condition>` and are raised by one domain's rules. Handle
them where the call is made, beside the thing the user was trying to do.

## Protocol codes

| Code | Meaning |
|---|---|
| `bad_request` | The request could not be understood — unparseable JSON, a missing header, or a path parameter of the wrong shape. |
| `conflict` | Valid, but it contradicts the current state — a bid on a job that has just been awarded, a second award on one job. |
| `forbidden` | The caller is known and is not permitted. Used where the caller may already know the resource exists. |
| `idempotency_key_required` | A state-changing request arrived with no Idempotency-Key header. Generate one value per action and reuse it for every retry of that action. |
| `idempotency_key_reused` | The key has already been used for a different request. Replaying the first response would report success for something the client never sent. |
| `idempotency_request_in_progress` | The original request with this key is still being handled. Retrying is correct, and is already what the client was doing. |
| `internal_error` | A failure the caller cannot act on. Carries nothing beyond the request ID, which is what makes it diagnosable. |
| `method_not_allowed` | The path exists but does not serve this method. |
| `not_found` | No such resource — or none the caller has any business knowing about. The two are deliberately indistinguishable. |
| `payload_too_large` | The request body exceeds the limit. Images go directly to object storage by pre-signed URL and never through the API. |
| `rate_limited` | Too many requests. Carries a Retry-After header wherever one can be given honestly. |
| `service_unavailable` | A dependency this request needs is not answering. Unlike `internal_error` it means try again. |
| `token_expired` | The access token was genuine and has expired. Refresh it and retry; do not sign the user out. |
| `unauthenticated` | No usable credential was presented. Distinct from `forbidden` so a client knows to refresh a token rather than give up. |
| `unsupported_media_type` | This endpoint accepts application/json. |
| `validation_failed` | The request was well formed and the platform rejected it. Carries `details`, one entry per offending field. |

## Domain codes

| Code | Meaning |
|---|---|
| `admin_account_disabled` | This administrator account has been disabled. Ask whoever administers the platform to restore it; signing in again will not help. |
| `admin_credentials_invalid` | That email address and password do not match an administrator account. Deliberately one code for both halves, so this endpoint cannot be used to find out which addresses are administrators. |
| `admin_dispute_already_open` | This job already has a dispute waiting on an outcome. Open it rather than raising another — one job is disputed once at a time. |
| `admin_email_taken` | An administrator account already exists with that email address. |
| `admin_job_already_unpublished` | This job has already been unpublished. Reload it to see who removed it and why. |
| `admin_job_not_disputable` | A dispute can only be raised on a job that has been awarded and has not yet been completed or cancelled. Reload the job to see its current status. |
| `admin_job_not_unpublishable` | A job can only be unpublished before it is awarded. Once a provider has committed, ending it is a dispute an administrator resolves — reload the job to see its current status. |
| `admin_permission_denied` | This administrator account does not have permission to do that. Ask whoever administers the platform if you need it. |
| `admin_same_administrator` | A suspension must be approved by a different administrator from the one who requested it. Docs/04 §9 requires two people. |
| `admin_session_expired` | The administrator session has ended, through inactivity or by reaching its maximum length. Sign in again. |
| `admin_suspension_needs_review` | A permanent suspension needs a second administrator's approval. Request one instead, and another administrator can approve it. |
| `admin_suspension_review_outstanding` | This account already has a suspension waiting for a second administrator. Approve the existing request rather than making another. |
| `admin_suspension_review_settled` | That suspension review has already been settled. Reload the queue — another administrator may have approved it. |
| `admin_user_standing_unchanged` | This account already has that standing. Reload it — another administrator may have changed it already, and the audit trail will say who. |
| `bidding_already_bid` | You already have a live offer on this job. Revise or withdraw it rather than placing a second. |
| `bidding_bid_accepted` | That offer has been accepted. An accepted bid can be neither revised nor withdrawn. |
| `bidding_bid_closed` | That offer is no longer live, so it can be neither changed nor answered. |
| `bidding_wrong_party` | That offer belongs to the other party. Counter an offer they made; revise one you made yourself. |
| `delivery_driver_already_assigned` | This job already has a driver. Reload it to see who is carrying it. |
| `delivery_driver_link_expired` | This delivery link has expired. There is nothing to refresh — ask the transport provider to send a new one. |
| `delivery_job_not_assignable` | A driver can only be assigned to a job that has been awarded and has not yet set off. Reload the job to see its current status. |
| `delivery_milestone_not_permitted` | This milestone cannot be recorded from the job's current status. Reload the delivery to see where it is, and keep the update — the delivery has not reached this point yet, and one it has already passed is recorded rather than refused. |
| `delivery_proof_already_recorded` | That photograph is already the proof for another milestone. Ask for a new upload URL and send it again. |
| `delivery_proof_not_uploaded` | That photograph is not in the store yet. Finish uploading it to the URL you were given, then record the milestone again. |
| `delivery_proof_rejected` | That file is not a photograph this platform accepts. Capture it again, and compress it if it is large. |
| `delivery_proof_required` | A delivery is recorded with photo proof, or with a reason why there is none. Send the object_key of a photograph you have uploaded, or one of the exception reasons, in this request's proof field. |
| `fleet_duplicate_registration` | A vehicle with that registration is already in service in this fleet. Edit the existing one, or deactivate it first. |
| `fleet_provider_only` | Only a provider account can keep a fleet. Customers publish jobs; they do not run vehicles. |
| `identity_account_suspended` | The account has been suspended. Signing in is refused until support lifts it; contact support rather than retrying. |
| `identity_credentials_invalid` | That email address and password do not match an account. Deliberately one code for both halves, so this endpoint cannot be used to find out which addresses have accounts. |
| `identity_email_taken` | An account already exists for this email address. Sign in, or reset the password. |
| `identity_otp_invalid` | That code is not valid. Ask for a new one and try again. |
| `identity_phone_taken` | An account already exists for this mobile number. Sign in, or reset the password. |
| `identity_refresh_token_invalid` | This session has ended. Sign in again. |
| `identity_session_not_found` | No such device session on this account. A session belonging to somebody else answers identically. |
| `identity_verification_token_expired` | This verification link has expired. Ask for a new one. |
| `identity_verification_token_invalid` | This verification link is no longer valid. Ask for a new one. |
| `jobs_customer_only` | Only a customer account can create or edit a job. Providers bid on jobs; they do not publish them. |
| `jobs_not_a_draft` | The job has been published and can no longer be edited as a draft. Reload it to see its current status. |
| `jobs_not_cancellable` | The job can no longer be cancelled. Once a provider has been awarded the work, ending the job is a support matter rather than a state change. Reload it to see its current status. |
| `jobs_not_extendable` | The job's expiry cannot be extended. Either it is not being offered to providers any more, or its pickup date is what is ending it — and no amount of extra listing time keeps a job alive past the date its goods were to be collected. |
| `notifications_category_essential` | This kind of notification cannot be switched off. Docs/01 §4.5 lists the events every account is told about. |
| `notifications_no_device_session` | That credential does not name a device session, so there is nothing to register a push token against. Sign in again. |
