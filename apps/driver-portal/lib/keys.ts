/**
 * One idempotency key per action, held until that action settles (SHIP-121).
 *
 * `CLAUDE.md` makes it an invariant that every state-changing endpoint accepts an idempotency key,
 * and `httpx.Idempotent`'s own refusal says what the key means: "generate one value per action and
 * reuse it for every retry of that action". Both halves of that sentence are load-bearing on a
 * phone in a yard, and they pull in opposite directions:
 *
 * - **Reuse it for every retry.** A driver taps `Picked up`, the request goes out, the phone loses
 *   signal before the answer comes back. They tap again. A fresh key would record a second
 *   milestone; the same key gets the first one's answer, from Redis while the entry lives and from
 *   `uq_milestones_idempotency (job_id, idempotency_key)` for ever after.
 * - **One value per action.** A driver reaches the pickup, finds nobody there, and records
 *   `en_route_to_pickup` again on the way back. `Docs/02` §5 calls that an ordinary failed pickup
 *   attempt and `000601` deliberately has no uniqueness on `(job_id, milestone)` so that it can be
 *   recorded. That is a **second action**, and reusing the first one's key would hand the driver
 *   back the first recording and write nothing.
 *
 * **So a key is minted per attempt and discarded when the attempt settles**, which is what
 * distinguishes the two cases without the page having to know anything about milestones. A retry of
 * an unsettled attempt reuses; a new tap after a settled one mints.
 *
 * # "Settled" is an answer from the platform, and a network failure is not one
 *
 * A `2xx`, or a `4xx` the driver caused, is an answer: the platform has decided, and the next tap is
 * a new action. No answer at all — the phone had no signal, the request timed out, the origin
 * returned `503` — is **not** an answer, and it is the exact case the key exists for, so the key
 * stays. `lib/delivery.ts` decides which is which and this file does not: it holds a string.
 *
 * # Why `sessionStorage` and not a React ref
 *
 * A ref dies on reload, and a driver on one bar of signal reloads. The sequence this protects
 * against is real and is the one a phone actually does: tap, request goes out, answer never comes
 * back, driver pulls to refresh, taps again. With a ref that is two milestones; with this it is one.
 *
 * It is the same store `lib/link.ts` holds the credential in and is deliberately **not** the same
 * file. What is here is not a credential: an idempotency key is a value the client invents to name
 * its own request, it authorises nothing, and it is sent in a header the platform reads and refuses
 * to act on twice. Keeping the two apart is what lets `lib/surface.test.ts` go on saying that the
 * *credential* is named in exactly one place.
 *
 * `sessionStorage` rather than `localStorage` for the reason `link.ts` gives, plus one of this
 * file's own: a key that outlived the tab would be presented weeks later against a job whose
 * `milestones` row still holds it, and the driver would be told their recording had already
 * happened when they meant to make a new one.
 */

/**
 * Where one action's key is held.
 *
 * Scoped by job **and** by what the action is, so two milestones in flight at once — a driver
 * tapping `Picked up` while `En route to pickup` is still retrying in a tunnel — do not share a key
 * and get each other's answers.
 */
function storageKey(scope: string): string {
  return `shipper.idempotency-key.${scope}`;
}

/**
 * A key of this browser's own, from a CSPRNG.
 *
 * `crypto.randomUUID` rather than anything derived from the clock or from a counter, and the reason
 * is not tidiness. It was load-bearing until SHIP-147b and it is defence in depth now, which is a
 * demotion rather than a reason to weaken it.
 *
 * **Until SHIP-147b, keys on a driver-token route were scoped `anonymous`** — a driver token
 * produces no `authctx.Subject` and the scope is computed group-wide, outside the middleware — so
 * `idem:v1:anonymous:<key>` was a namespace shared with every other anonymous caller, and a stored
 * response was reachable by anybody who could reproduce the key **and** the exact request. 122 bits
 * from a CSPRNG is what made the first of those two unreachable, and SHIP-122's upload — whose
 * stored response carries a credential — is why it was written down here rather than assumed.
 *
 * `httpx.SubjectScope` now answers `credential:<salted digest of the bearer token>` for a credential
 * that produces no subject, so this browser's keys land in a namespace only the holder of this exact
 * link can compute. The shared namespace is gone; an unguessable key is still the right thing to
 * mint, because nothing here should depend on a platform-side scope decision staying as it is.
 *
 * The fallback is for a browser with no `crypto.randomUUID`, which on a driver's handset means an
 * old Android WebView. It is deliberately still random rather than sequential: `Math.random` is not
 * a CSPRNG and is stated as the weaker path rather than presented as an equal one.
 */
function mint(): string {
  const source = globalThis.crypto;
  if (source !== undefined && typeof source.randomUUID === "function") return source.randomUUID();

  return `k-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

/**
 * The key for this action: the one already in flight, or a fresh one.
 *
 * A browser with storage disabled — private mode on some devices, a locked-down managed handset —
 * gets a fresh key every call, which means a retry after a dropped connection can record twice.
 * That is the honest degradation and it is the same trade `link.ts` takes: a page that refused to
 * work without storage would leave a driver unable to record anything at all, and the platform's
 * `uq_milestones_idempotency` still makes each *key* record once.
 */
export function keyFor(scope: string): string {
  let held: string | null = null;
  try {
    held = window.sessionStorage.getItem(storageKey(scope));
  } catch {
    held = null;
  }
  if (held !== null && held !== "") return held;

  const key = mint();
  try {
    window.sessionStorage.setItem(storageKey(scope), key);
  } catch {
    // Nothing to do about it, and nothing worth telling the driver. See above.
  }
  return key;
}

/**
 * The action is over: the platform answered, one way or the other.
 *
 * Called on every settled outcome and on no unsettled one, so the next tap on the same milestone is
 * a new action with a new key — which is what makes `Docs/02` §5's second pickup attempt recordable.
 */
export function settle(scope: string): void {
  try {
    window.sessionStorage.removeItem(storageKey(scope));
  } catch {
    // A browser that refused to store it has nothing to remove.
  }
}

/**
 * A key for an action that is over the moment it is answered (SHIP-122).
 *
 * **The presign is the case, and it is the opposite of a milestone.** `POST
 * /v1/driver/jobs/{id}/proof-uploads` answers with a short-lived URL, and reusing a stored key makes
 * SHIP-15's middleware replay the stored response — so every retry gets the *same* URL with its
 * expiry already running down, dead until Redis evicts it. What a driver retrying an upload needs is
 * a new slot, which means a new key and a new object key.
 *
 * That is not a hole in the idempotency contract, it is the contract read correctly: "one value per
 * action, reused for every retry of **that action**". Asking for somewhere to put a photograph after
 * the first place expired is a different action. The platform's own contract fragment says the same
 * thing to every client.
 *
 * It is deliberately not held anywhere. Nothing needs to find it again, and a key in storage that
 * nothing settles is a key that comes back on the next attempt.
 */
export function freshKey(): string {
  return mint();
}
