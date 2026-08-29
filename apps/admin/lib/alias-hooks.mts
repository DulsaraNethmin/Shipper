import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

/**
 * The `@/` path alias, resolved for `node --test` (SHIP-188a).
 *
 * `tsconfig.json` maps `@/*` to the application root and the bundler honours it, but Node does not
 * read a `tsconfig`. So `lib/sessions.test.ts` — which imports the route handlers in order to assert
 * on the requests they send upstream — cannot load a file that says `@/lib/session` without this.
 *
 * It is registered from the one test that needs it rather than from the `test` script, so the other
 * test files run under a plain runtime and nothing about how they resolve modules changes. Fifteen
 * lines of resolution is the whole cost of the panel's outbound hop being under test at all; the
 * alternative was rewriting an application import to suit a test runner, which is the tail wagging
 * the dog.
 *
 * The mapping is deliberately the same one `tsconfig.json` states — alias to the application root,
 * then the extensions a TypeScript project resolves — so a file that loads here loads in the
 * bundler too. Anything it cannot find is handed back untouched, and Node reports it in its own
 * words.
 */

/** The application root: this file is in `lib/`, so one level up. */
const ROOT = new URL("../", import.meta.url);

/** What TypeScript would try, in the order it tries it. */
const SUFFIXES = ["", ".ts", ".tsx", "/index.ts", "/index.tsx"];

type ResolveContext = { conditions: string[]; importAttributes: object; parentURL?: string };
type Resolved = { url: string; format?: string | null; shortCircuit?: boolean };
type NextResolve = (specifier: string, context: ResolveContext) => Resolved | Promise<Resolved>;

export async function resolve(
  specifier: string,
  context: ResolveContext,
  next: NextResolve,
): Promise<Resolved> {
  if (!specifier.startsWith("@/")) return next(specifier, context);

  const target = new URL(specifier.slice(2), ROOT);
  for (const suffix of SUFFIXES) {
    const candidate = new URL(target.href + suffix);
    if (existsSync(fileURLToPath(candidate))) return next(candidate.href, context);
  }

  return next(specifier, context);
}
