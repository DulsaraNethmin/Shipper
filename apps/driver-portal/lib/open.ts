import {
  isSettled,
  openDelivery,
  recordMilestone,
  type Delivery,
  type Evidence,
  type RecordOutcome,
  type Refusal,
} from "./delivery.ts";
import { keyFor, settle } from "./keys.ts";
import { isJobId, recallToken, rememberToken, tokenFromFragment } from "./link.ts";

/**
 * The whole of "what does this link open", from the address bar to an answer.
 *
 * This lived inside `components/delivery-link.tsx` when SHIP-120 landed, and it is here for one
 * reason: **a `.tsx` file cannot be imported by `node --test`**, so nothing could assert on the
 * request the portal issues while the chain ran inside the component. Node 22 strips types but not
 * JSX, and the guard in `one-job.test.ts` is the reason the chain had to be reachable — see that
 * file for what it holds, and `link.ts` for why it matters.
 *
 * The split is worth having on its own terms as well. Everything with a decision in it is here;
 * the component is left holding state and markup, which is the half a test cannot check anyway.
 *
 * # The job identifier comes from the caller and from nothing else
 *
 * `openLink` is handed the job identifier out of the URL path. It never derives one, and in
 * particular it never derives one from the token — the token is an opaque string here, passed to
 * the platform and never parsed. `link.ts` records the full argument; the short form is that
 * SHIP-108's one-job check compares the path against the grant, so a client that built the path out
 * of the grant would leave that check comparing the token with itself and it would pass for ever.
 */

/** What one attempt at opening a link produced, before the page decides how to draw it. */
export type Opened =
  | { kind: "delivery"; delivery: Delivery }
  | { kind: "refused"; refusal: Refusal }
  | { kind: "missing" };

/**
 * The token for this page view, taken from the fragment if the driver has just arrived and from
 * this tab's storage if they have reloaded.
 *
 * **The fragment is stripped only once the token is somewhere a reload can find it.** A browser
 * that refuses storage keeps a working link in its address bar, which is the worse of the two
 * exposures and much better than a page that cannot survive the pull-to-refresh a driver on one
 * bar of signal will certainly do.
 *
 * Note which argument is doing what: `jobId` decides *where the token is filed*, and the token
 * decides nothing about `jobId`. The dependency runs one way and it has to.
 */
function tokenForThisView(jobId: string): string | null {
  const arriving = tokenFromFragment(window.location.hash);
  if (arriving === null) return recallToken(jobId);

  if (rememberToken(jobId, arriving)) {
    window.history.replaceState(null, "", window.location.pathname);
  }
  return arriving;
}

/**
 * One attempt at opening the link, from the address bar through to an answer.
 *
 * `async` and resolving to a single value on purpose. The whole sequence — recognise the
 * identifier, take the token, ask the platform — produces one outcome, so the component never sets
 * state partway through and `react-hooks/set-state-in-effect` has nothing to object to. It is also
 * the shape that reads correctly: there is one question, "what does this link open", and one
 * answer.
 */
export async function openLink(jobId: string, signal: AbortSignal): Promise<Opened> {
  // A path segment that is not a job identifier never reaches the platform. That is not the page
  // deciding anything — there is nothing here to decide about, because there is no delivery this
  // could be naming and no request worth making.
  if (!isJobId(jobId)) return { kind: "refused", refusal: "invalid" };

  const token = tokenForThisView(jobId);
  if (token === null) return { kind: "missing" };

  return openDelivery(jobId, token, signal);
}

/** What one tap on a milestone button produced, before the page decides how to draw it. */
export type Told = RecordOutcome | { kind: "missing" };

/**
 * Record one milestone, from the address bar through to an answer (SHIP-121).
 *
 * The write counterpart of [openLink], and it is here for the same two reasons: a `.tsx` file
 * cannot be imported by `node --test`, and everything with a decision in it belongs where a test
 * can reach it. What is left in the component is state and markup.
 *
 * # The idempotency key's whole lifecycle is these four lines and it is the reason this is not
 * inside the component
 *
 * `lib/keys.ts` mints one per action and holds it across a reload; this function is the only place
 * that decides an action is **over**. The rule is one line — settle on an answer, keep on silence —
 * and it is testable here and untestable in JSX. Getting it wrong in either direction is a defect a
 * driver meets and nobody else does: settling on a network failure records the milestone twice when
 * they tap again, and never settling makes `Docs/02` §5's second pickup attempt come back as the
 * first one with nothing written.
 *
 * The scope is the job and the milestone together, so two recordings in flight at once — a driver
 * tapping `Picked up` while `En route to pickup` is still retrying in a tunnel — cannot take each
 * other's key or each other's answer.
 *
 * # The job identifier is the caller's, exactly as it is on the read
 *
 * `jobId` arrives from the URL path and is handed on unchanged. It is never derived from the token,
 * and the token is never parsed here — `lib/link.ts` records why at length, and
 * `lib/one-job.test.ts` asserts it against the requests this function actually issues, on the write
 * as well as on the read.
 */
export async function recordStep(
  jobId: string,
  milestone: string,
  options: { evidence?: Evidence; signal?: AbortSignal } = {},
): Promise<Told> {
  if (!isJobId(jobId)) return { kind: "refused", refusal: "invalid" };

  const token = tokenForThisView(jobId);
  if (token === null) return { kind: "missing" };

  const scope = `${jobId}.${milestone}`;
  const outcome = await recordMilestone(jobId, token, milestone, keyFor(scope), {
    evidence: options.evidence,
    signal: options.signal,
  });

  if (outcome.kind === "recorded" || isSettled(outcome.refusal)) settle(scope);
  return outcome;
}
