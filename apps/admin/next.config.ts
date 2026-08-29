import type { NextConfig } from "next";

/**
 * The admin panel's configuration (SHIP-22, given its one entry by SHIP-188d).
 *
 * # Nothing this panel renders may be stored by any cache
 *
 * Next serves a dynamic route with `Cache-Control: no-cache, must-revalidate`, which is the right
 * default for a web application and is not enough here — measured on the evidence screen rather than
 * assumed. `no-cache` requires a cache to revalidate before *reusing* a response; it explicitly
 * permits *storing* one. So the bytes of a screen showing a provider's licence photograph could sit
 * in a shared support machine's disk cache after the reviewer signed out, and in an intermediary's
 * too, because the default also carries no `private`.
 *
 * That is the one thing `Docs/09`'s SHIP-188d row asks not to happen — "the panel keeps neither a
 * URL nor an object key" — and the panel not writing one anywhere is only half of it if the browser
 * writes the whole page. `private, no-store` closes both: nothing intermediary may hold it, and
 * nothing may hold it at all.
 *
 * **It is every path rather than the evidence screen's, and that is deliberate.** A rule scoped to
 * one route is a rule the next screen has to remember, and every screen in this panel is behind a
 * session and renders somebody's account, job, contact details or audit trail. There is no page here
 * that a cache should keep. Scoping it narrowly would also be the kind of exception somebody widens
 * later without re-reading why it was narrow.
 *
 * `_next/static` and `_next/image` are excluded because they are content-hashed build output with no
 * session in them, and making those uncacheable would re-fetch the whole bundle on every navigation
 * for no benefit at all.
 */
const nextConfig: NextConfig = {
  async headers() {
    return [
      {
        source: "/((?!_next/static|_next/image|favicon.ico).*)",
        headers: [
          { key: "Cache-Control", value: "private, no-store" },

          // Not part of the caching decision, and here because this is the file that answers
          // "what does every response from this panel carry". A signed evidence URL is the most
          // valuable thing in a Referer header this product will ever produce, and the object
          // store is a different origin: `same-origin` stops the URL of the page a reviewer is
          // looking at travelling anywhere off it.
          { key: "Referrer-Policy", value: "same-origin" },
        ],
      },
    ];
  },
};

export default nextConfig;
