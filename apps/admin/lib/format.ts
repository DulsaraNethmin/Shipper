/**
 * How the panel renders the few values that are not already a person's words (SHIP-188b).
 *
 * Small, and pure functions of their arguments so `lib/format.test.ts` can run them under
 * `node --test`. Nothing here decides anything: a status arrives from the platform in `Docs/02` §1's
 * own spelling and is printed unaltered, because `CLAUDE.md` requires the document's names and a
 * panel that prettified them would put support and the database in two vocabularies.
 */

/**
 * An RFC 3339 instant as an Australian reader expects it.
 *
 * Day-first, per `CLAUDE.md`'s conventions — and with the month as a word rather than a number,
 * which is the one decision here worth stating. `08/09/2026` is the fourth of the ambiguities this
 * product can least afford in a support screen: an administrator reading a delivery timeline beside
 * a customer on the telephone has no way to tell a British reading from an American one, and both
 * are plausible to somebody who does not know which convention the panel chose. `8 Sep 2026` cannot
 * be misread.
 *
 * # It is the server's timezone, and that is a decision rather than an oversight
 *
 * These screens are server-rendered, so the instant is formatted where the panel runs rather than
 * where the reader is. The alternative — formatting in the browser — would mean a client component
 * for every timestamp and a hydration mismatch on each one, in exchange for a support engineer and
 * the colleague they are on a call with seeing *different* times for the same event. One timezone,
 * the deployment's, is the answer that lets two people read the same screen aloud to each other.
 *
 * `Docs/09`'s SHIP-189 has to set `TZ` for the demonstration deployment; until then it is the
 * machine's, which for development is the right one anyway.
 *
 * An empty string in, an em dash out. Every timestamp on these endpoints is "always present, empty
 * where there is none" — a deliberate choice on the Go side so a console that renders without
 * checking does not crash — so the absence arrives here as `""` and has to render as something a
 * person reads as *nothing recorded* rather than as a blank cell that looks like a bug.
 */
export function instant(value: string): string {
  if (value === "") return "—";

  const at = new Date(value);
  if (Number.isNaN(at.getTime())) return value;

  return new Intl.DateTimeFormat("en-AU", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(at);
}

/**
 * The first group of an identifier, for a table cell.
 *
 * A full UUID is 36 characters and there are two of them on every row of the job search, which is
 * more than half the width of the table spent on values nobody reads character by character. The
 * first group is enough to tell two rows apart at a glance and to match against an id somebody is
 * holding.
 *
 * **The full value is never only in the abbreviation.** Every caller puts it in a `title` and, where
 * the cell is a link, in the href — so it can be read, copied and followed. An abbreviation that was
 * the only copy would make the panel worse than a `psql` window at the one thing an operator most
 * often needs, which is the identifier itself.
 */
export function shortIdentifier(id: string): string {
  const at = id.indexOf("-");
  return at < 0 ? id.slice(0, 8) : id.slice(0, at);
}

/**
 * An amount in cents, as AUD (SHIP-188c).
 *
 * `CLAUDE.md` fixes the currency, so it is not a parameter — there is no second currency in this
 * product and a symbol that could vary would be inventing one. `amount_cents` is an integer on the
 * wire for the ordinary reason: money in a binary float rounds where nobody expects it, and the one
 * place this panel could introduce that is the division below, which is why it happens once, here,
 * rather than in each cell.
 *
 * A bid row may carry no amount at all — `adminBidResponse.AmountCents` is "zero where the row
 * carries none" — and zero is rendered as a dash rather than as `$0.00`, because an offer of nothing
 * and an offer with no price recorded are different facts and only the second exists.
 */
export function money(cents: number): string {
  if (!Number.isFinite(cents) || cents === 0) return "—";

  return new Intl.NumberFormat("en-AU", {
    style: "currency",
    currency: "AUD",
    currencyDisplay: "narrowSymbol",
  }).format(cents / 100);
}
