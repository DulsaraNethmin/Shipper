/**
 * Dates, the way a driver in Australia reads them (SHIP-120).
 *
 * `CLAUDE.md` puts day-first in user-facing copy, and the platform serves UTC. The conversion is
 * the browser's, because the only clock that matters to somebody standing at a roller door is the
 * one on their phone — a link that expires "at 06:00" is useless if that is 06:00 somewhere else.
 *
 * The time zone is a parameter so this is testable without a machine set to Australian time. It is
 * left undefined in the application, which is what makes `Intl` use the device's.
 */

/**
 * A UTC timestamp as `13 Aug 2026, 3:04 pm`, or null when it is not a timestamp.
 *
 * Null rather than the input, because a page that echoed an unparseable value would put an ISO
 * string or the word `undefined` in front of a driver. The caller says what it shows instead, and
 * that is a decision about copy rather than about formatting.
 */
export function dayFirst(iso: string, timeZone?: string): string | null {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return null;

  return new Intl.DateTimeFormat("en-AU", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
    timeZone,
  }).format(at);
}
