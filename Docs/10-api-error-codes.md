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
| `identity_email_taken` | An account already exists for this email address. Sign in, or reset the password. |
| `identity_phone_taken` | An account already exists for this mobile number. Sign in, or reset the password. |
