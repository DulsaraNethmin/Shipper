import Link from "next/link";

import { Button } from "@/components/ui/button";

/**
 * Paging, which on these endpoints means the platform's cursor and nothing else (SHIP-188b).
 *
 * # There is no page number, and there cannot be one
 *
 * `internal/pagination` issues an opaque cursor over `(created_at, id)` and the endpoints select one
 * row more than they were asked for so that "is there another page" is answered by the rows rather
 * than by a second `COUNT`. Nothing anywhere knows how many pages there are, deliberately: a total
 * over a moving table is a number that is wrong by the time it is rendered, and it costs a full scan
 * to be wrong.
 *
 * So this is Next and First, and that is the honest shape of the thing underneath.
 *
 * # Why there is no Back
 *
 * A cursor is forward-only. The panel could carry the cursors it has already used in the URL and
 * step back through them, and the URL would grow by forty characters a page — which is fine for
 * three pages and is a link nobody can paste by ten. **The browser's own back button already does
 * this correctly**, because every page of a search is a distinct URL and a real navigation. Adding a
 * second, worse mechanism beside a working one is how a screen ends up with two ideas of where it
 * is.
 *
 * First is here because back is not always where somebody wants to be: eight pages in, the way out
 * is the start of the search rather than eight presses.
 */
export function Pager({
  first,
  next,
  showing,
}: {
  /** The href of this search with no cursor — the same URL its first page had. */
  first: string;

  /** The href of the next page, or `undefined` when the platform said there is not one. */
  next?: string;

  /** How many rows are on this page, so the count and the controls stay in one place. */
  showing: number;
}) {
  const paged = next !== undefined;

  return (
    <div className="flex flex-wrap items-center gap-3 text-sm">
      <span className="text-muted-foreground">
        {showing === 0
          ? "No rows"
          : `${showing} ${showing === 1 ? "row" : "rows"} on this page`}
      </span>

      <div className="ml-auto flex items-center gap-2">
        <Button asChild variant="outline" size="sm">
          <Link href={first}>First page</Link>
        </Button>

        {paged ? (
          <Button asChild variant="outline" size="sm">
            <Link href={next}>Next page</Link>
          </Button>
        ) : (
          // Disabled rather than absent. A control that disappears at the end of a list moves
          // everything beside it, and the reader has to work out whether they reached the end or
          // whether the panel forgot to draw it.
          <Button variant="outline" size="sm" disabled aria-disabled>
            Next page
          </Button>
        )}
      </div>
    </div>
  );
}
