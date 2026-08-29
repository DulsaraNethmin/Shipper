import assert from "node:assert/strict";
import { test } from "node:test";

import { instant, shortIdentifier } from "./format.ts";

/**
 * The display helpers (SHIP-188b).
 *
 * **The shape is asserted and the value is not, deliberately.** `instant` renders in the machine's
 * timezone — see `lib/format.ts` for why one timezone beats each reader's own on a support screen —
 * so a test naming an exact hour would pass in Melbourne, fail in CI, and be silenced by whoever hit
 * it first. What matters and is stable is that the day comes before the month, the month is a word,
 * and the clock is 24-hour.
 */
test("an instant is day-first, with the month as a word and a 24-hour clock", () => {
  assert.match(instant("2026-08-29T04:05:06Z"), /^\d{1,2} [A-Z][a-z]{2} \d{4}, \d{2}:\d{2}$/);
});

/**
 * An absent timestamp is a dash.
 *
 * Every timestamp on these endpoints is "always present, empty where there is none" — the Go side
 * chose that so a console rendering without checking cannot crash — so the absence arrives as `""`
 * and has to read as *nothing recorded* rather than as a blank cell somebody reports as a bug.
 */
test("an absent instant renders as a dash rather than a blank", () => {
  assert.equal(instant(""), "—");
});

/**
 * Something that is not a timestamp is printed rather than swallowed.
 *
 * The platform does not send one, so this is the case where an assumption here has already been
 * wrong. Showing the raw value puts the defect on the screen where somebody reports it; `Invalid
 * Date` or an empty cell hides it behind something that looks like ordinary missing data.
 */
test("a value that is not a timestamp is shown as it arrived", () => {
  assert.equal(instant("the day before yesterday"), "the day before yesterday");
});

test("an identifier abbreviates to its first group", () => {
  assert.equal(shortIdentifier("0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0"), "0198f2c1");
  assert.equal(shortIdentifier("0198f2c16b407a119c3e2f9a4d51b7e0"), "0198f2c1");
  assert.equal(shortIdentifier(""), "");
});
