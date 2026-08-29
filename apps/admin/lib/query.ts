/**
 * The search screens' state lives in the URL, and this is what reads and writes it (SHIP-188b).
 *
 * # Why the URL and not a client component's `useState`
 *
 * `Docs/09`'s SHIP-188b row asks that both searches "page through the platform's own cursor rather
 * than a client-side slice". Keeping the term, the filters and the cursor in the query string is the
 * strongest form of that: there is nowhere for a slice to live, because the screen is rendered on
 * the server from the parameters it was asked for and holds no result set between renders.
 *
 * It also buys three things a component state would have cost:
 *
 * - **A search is a link.** A support engineer can paste one into a ticket, and the person who opens
 *   it sees the same rows — subject to their own permissions, which the platform re-checks.
 * - **Back works.** Paging forward and pressing back returns to the previous page rather than to
 *   whatever screen preceded the search.
 * - **Nothing renders before the platform has answered.** There is no moment where the panel shows
 *   an empty table it is about to fill, which is the moment a 403 gets mistaken for no results.
 *
 * Everything here is a pure function of its arguments, so `lib/query.test.ts` runs it under
 * `node --test` with no framework, no build and no Next request scope.
 */

/**
 * The value of a search parameter that should appear once.
 *
 * Next hands back `string[]` for a repeated parameter — `?q=a&q=b`. No form in this panel produces
 * one, so it is either somebody editing a URL or a client that has gone wrong, and **the first value
 * is taken rather than refusing**: a screen that answered a mangled URL with an error would be
 * making a mistyped link unrecoverable, when the honest reading of `?q=a&q=b` is a search for `a`.
 * The platform re-validates whatever comes out of here, so nothing downstream trusts it.
 */
export function one(value: string | string[] | undefined): string {
  if (value === undefined) return "";
  return (Array.isArray(value) ? (value[0] ?? "") : value).trim();
}

/**
 * A link to the same screen with a different set of parameters.
 *
 * **An empty value is dropped rather than sent empty.** `?q=&status=` is what a browser submits for
 * an untouched form, and it is what the platform treats as no filter at all — but it is not what a
 * person wants to paste into a ticket, and it makes two links to the same search look different. So
 * the query string carries what was actually asked for and nothing else, which is also what makes
 * "the first page" and "the first page again after paging" the same URL.
 *
 * The order is the caller's rather than sorted: `q` before `status` before `cursor` reads the way
 * the screen does.
 */
export function href(path: string, params: Record<string, string | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") search.set(key, value);
  }

  const query = search.toString();
  return query === "" ? path : `${path}?${query}`;
}
