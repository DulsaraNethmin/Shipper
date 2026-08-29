import { cookies } from "next/headers";

import { SESSION_COOKIE } from "@/lib/session";

/**
 * The one place a server render turns the browser's cookie into a request the platform will accept
 * (SHIP-188b).
 *
 * # Why this file exists, when `lib/administrator.ts` already did it
 *
 * It did it *once*. SHIP-188a had a single server module that read the cookie, and inlining the
 * three lines was right for one caller. SHIP-188b adds two screens, SHIP-188c a third and SHIP-188d
 * two more, and the same three lines copied five times is five files that name the cookie and five
 * that hold a bearer token — which is five entries on each of `lib/surface.test.ts`'s allow-lists,
 * and an allow-list that grows with every screen has stopped being a guard and become a register.
 *
 * With this file the lists stay at their length whatever the panel grows: **the credential is read
 * in exactly one place and handed on as a `Headers`, never as a string.** A page receives something
 * it can put on a request and cannot log, print, or accidentally render — the token is not a value
 * any screen ever holds.
 *
 * # It is a server module and cannot become anything else
 *
 * `next/headers` is a build failure inside a `"use client"` module, so the structural guarantee
 * SHIP-188a established is unchanged and is now concentrated rather than spread: the only code that
 * can read the credential is code that cannot run in a browser.
 *
 * # There is deliberately no `fetch` here, and no path
 *
 * This file could obviously hold a `request(path, …)` and save every caller four lines. That is the
 * shared forwarder `app/api/admin/sessions/route.ts` refuses at length: one function whose
 * destination is an argument is a general, credential-forwarding front door onto twenty-four
 * privileged endpoints, however narrow its callers happen to be today. So this hands back headers
 * and knows nothing about where they are going — each screen names its own endpoint as a literal,
 * and `lib/surface.test.ts` counts them.
 */

/**
 * The headers a server render puts on a platform request, or `null` when this browser holds no
 * session.
 *
 * `null` rather than a throw, because "not signed in" is an ordinary answer on every screen and the
 * caller has a screen to render for it. A throw would make the ordinary case an exception and the
 * exceptional case — a platform that is not answering — indistinguishable from it.
 *
 * The credential goes in a header and never in a path or a query string. A request line reaches
 * every access log between here and the platform; a header does not.
 */
export async function platformHeaders(): Promise<Headers | null> {
  const token = (await cookies()).get(SESSION_COOKIE)?.value;
  if (token === undefined || token === "") return null;

  return new Headers({
    Accept: "application/json",
    Authorization: `Bearer ${token}`, // spelling:ok — RFC 9110 header name
  });
}
