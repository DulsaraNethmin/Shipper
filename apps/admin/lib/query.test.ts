import assert from "node:assert/strict";
import { test } from "node:test";

import { href, one } from "./query.ts";

/**
 * The URL is the search screens' only state, so these are the functions that decide what a search
 * *is* (SHIP-188b). They run under `node --test` with no framework and no build — Node strips the
 * types and resolves `./query.ts` by its real name, which is what `allowImportingTsExtensions` in
 * `tsconfig.json` is for.
 */

test("a parameter that appears once reads as itself, trimmed", () => {
  assert.equal(one("alice"), "alice");
  assert.equal(one("  alice  "), "alice");
  assert.equal(one(""), "");
  assert.equal(one(undefined), "");
});

/**
 * A repeated parameter takes the first value rather than refusing.
 *
 * No form here produces one, so `?q=a&q=b` is a hand-edited URL — and answering it with an error
 * would make a mistyped link unrecoverable, where the honest reading is a search for `a`. Nothing
 * downstream trusts the result: the platform re-validates every parameter it is sent.
 */
test("a repeated parameter takes the first value", () => {
  assert.equal(one(["alice", "bob"]), "alice");
  assert.equal(one([]), "");
});

test("a link carries the parameters that were asked for, in the caller's order", () => {
  assert.equal(href("/users", { q: "alice", status: "active" }), "/users?q=alice&status=active");
});

/**
 * **An empty value is dropped rather than sent empty**, which is the assertion that keeps two links
 * to the same search identical. A browser submits `?q=&status=` for an untouched form, the platform
 * reads both as no filter, and a URL carrying them is a different string for the same question.
 */
test("an empty or absent value is left out of the link", () => {
  assert.equal(href("/users", { q: "", status: undefined, cursor: "" }), "/users");
  assert.equal(href("/users", { q: "alice", status: "", cursor: undefined }), "/users?q=alice");
});

/** A term reaches the query string encoded, so nothing a person types can change the link's shape. */
test("a term is encoded rather than interpolated", () => {
  assert.equal(href("/jobs", { q: "two-seater sofa & rug" }), "/jobs?q=two-seater+sofa+%26+rug");
  assert.equal(href("/jobs", { q: "a/b?c=d#e" }), "/jobs?q=a%2Fb%3Fc%3Dd%23e");
});
