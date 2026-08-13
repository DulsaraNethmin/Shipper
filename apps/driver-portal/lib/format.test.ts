import assert from "node:assert/strict";
import { test } from "node:test";

import { dayFirst } from "./format.ts";

/** Dates are day-first and in the driver's own time zone (SHIP-120). */

/**
 * Whitespace collapsed to ordinary spaces before comparing.
 *
 * `Intl` separates the time from its meridiem with a narrow no-break space, which renders exactly
 * as intended and is invisible in a failure message — an assertion written against a plain space
 * fails printing two strings that look identical.
 */
const plainly = (shown: string | null) => (shown ?? "").replace(/\s/gu, " ");

test("a UTC timestamp is shown day-first, in the reader's zone", () => {
  const shown = dayFirst("2026-08-13T05:04:00Z", "Australia/Sydney");

  assert.notEqual(shown, null);
  // 05:04 UTC is 15:04 in Sydney in August, so the conversion is visible in the result rather
  // than merely assumed — a formatter left on UTC would print 13 Aug 2026, 5:04 am.
  assert.equal(plainly(shown), "13 Aug 2026, 3:04 pm");
});

test("the day comes before the month, which is where an American default would show", () => {
  const shown = plainly(dayFirst("2026-03-04T01:00:00Z", "Australia/Sydney"));

  assert.equal(shown.startsWith("4 Mar"), true, `formatted as ${shown}`);
});

test("a value that is not a timestamp formats as nothing rather than as itself", () => {
  assert.equal(dayFirst(""), null);
  assert.equal(dayFirst("not a date"), null);
  assert.equal(dayFirst("undefined"), null);
});
