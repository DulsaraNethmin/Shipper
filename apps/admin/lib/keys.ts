/**
 * One idempotency key per action, minted in the browser (SHIP-188a).
 *
 * `CLAUDE.md` makes it an invariant that every state-changing endpoint accepts an idempotency key,
 * and `httpx.Idempotent`'s own refusal says what the key means: "generate one value per action and
 * reuse it for every retry of that action".
 *
 * # Why the browser mints it and not the route handler
 *
 * The route handler is a hop, and a key minted at a hop changes on every attempt — which is the
 * header doing the exact opposite of its job. The browser is the only participant that knows
 * whether this is a new action or another go at the last one, so it is the only one that can decide
 * whether the value changes. SHIP-188d's *Done when* states the same rule for the verification
 * decision, where a double-click must record one decision rather than two.
 *
 * # It is a fresh key per submission, and there is no store
 *
 * The driver portal holds a key in `sessionStorage` so that a tap, a lost signal and a second tap
 * are one milestone. Neither action here needs that, and both would be harmed by it:
 *
 * - **Sign-in.** A wrong password answered `400`, then the right one typed and submitted, is a
 *   different request under the same key — which `httpx.Idempotent` refuses as
 *   `idempotency_key_reused`, because the fingerprint covers the body. Holding a key across
 *   submissions would make the second attempt fail on a password that is correct.
 * - **Sign-out.** A retry with a fresh key reaches the guard and is answered `401`, because the
 *   first attempt ended the session. That is the truth and the panel renders it as signed out.
 *
 * There is also nowhere to put one. `lib/surface.test.ts` holds this application to no
 * browser-side storage of any kind, which is the guarantee `lib/session.ts` exists to make, and a
 * key store would be the first exception somebody argued for.
 *
 * # The value is unguessable, and that is not decoration
 *
 * `httpx.SubjectScope` answers `credential:<salted digest of the bearer token>` for a credential
 * that produces no `authctx.Subject`, and an administrator grant is deliberately not one
 * (`internal/admin/adminauth.go`). So these keys land in a namespace only the holder of this exact
 * session token can compute — but **sign-in carries no credential at all**, so its key is scoped
 * `anonymous` and shares a namespace with every other unauthenticated caller. 122 bits from a
 * CSPRNG is what keeps a stored response out of reach there.
 */

/**
 * A key for one action, from a CSPRNG.
 *
 * The fallback is for a browser with no `crypto.randomUUID`. It is still random rather than
 * sequential, and it is stated as the weaker path rather than presented as an equal one:
 * `Math.random` is not a CSPRNG.
 */
export function idempotencyKey(): string {
  const source = globalThis.crypto;
  if (source !== undefined && typeof source.randomUUID === "function") return source.randomUUID();

  return `k-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}
