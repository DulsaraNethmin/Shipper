import { openDelivery, type Delivery, type Refusal } from "./delivery.ts";
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
