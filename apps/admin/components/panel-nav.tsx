"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

/**
 * The panel's sections, each named with the permission the platform requires of it and the ticket
 * that builds it (SHIP-22, filled in by SHIP-188a, and moved here from `admin-shell.tsx` by
 * SHIP-188b).
 *
 * **The permission is read to hide and to disable, and never to decide** (`Docs/07` §3, and
 * `internal/admin/http.go`'s `administratorResponse` says the same thing from the other side). An
 * administrator who edited the array this is checked against would find every endpoint refusing them
 * exactly as before, because the platform reads the role out of the session row on each request.
 * What this buys is somebody not clicking into a screen that was only ever going to answer 403.
 *
 * **Reports is listed and has no ticket that builds it**, which is deliberate rather than an
 * oversight to tidy away. `Docs/11` §5 has struck SHIP-156 for twelve consecutive passes: there is no
 * `reports` table, no report route among the twenty-four, and no row anywhere that creates one.
 * Deleting it from this list would remove the last place the gap is visible to somebody who does not
 * read the delivery status document.
 *
 * It lives beside the navigation rather than in the shell because the shell no longer renders it —
 * marking the current section needs the path, the path needs `usePathname`, and that needs a client
 * module. Keeping the shell a server component matters more than keeping one file: the shell is what
 * renders the administrator, and `lib/surface.test.ts` holds every client module to naming no
 * credential.
 */
const SECTIONS = [
  { label: "Overview", href: "/", ticket: "SHIP-188a", permission: null },
  { label: "Users", href: "/users", ticket: "SHIP-188b", permission: "users.read" },
  { label: "Jobs and bids", href: "/jobs", ticket: "SHIP-188b", permission: "jobs.read" },
  { label: "Audit trail", href: "/audit", ticket: "SHIP-188c", permission: "audit.read" },
  { label: "Reports", href: null, ticket: "SHIP-156", permission: "moderation.read" },
  { label: "Delivery exceptions", href: null, ticket: "SHIP-157", permission: "moderation.read" },
  { label: "Disputes", href: null, ticket: "SHIP-164", permission: "disputes.read" },
] as const;

/**
 * PanelNav renders the sections, marks the one being read, and disables what this role cannot reach.
 *
 * A section with no `href` is not built yet and stays an inert `<span>`: a navigation item that
 * navigated to a 404 would be worse than one that plainly says which ticket fills it.
 *
 * A section this administrator does not hold the permission for is also inert, and says so. It is
 * not hidden — somebody who cannot see the verification queue should be able to tell that it exists
 * and that their role is what is short, which is the difference between asking for the permission
 * and reporting the panel as broken.
 */
export function PanelNav({ permissions }: { permissions: string[] }) {
  const held = new Set(permissions);
  const path = usePathname();

  return (
    <ul className="space-y-1">
      {SECTIONS.map((section) => {
        const permitted = section.permission === null || held.has(section.permission);
        const reachable = section.href !== null && permitted;

        // `startsWith` for everything but the overview, so a job's own screen keeps "Jobs and
        // bids" marked. The overview is an exact match or it would be current on every page.
        const current =
          section.href !== null &&
          (section.href === "/" ? path === "/" : path.startsWith(section.href));

        const className =
          "flex items-baseline justify-between gap-2 rounded-md px-3 py-2 text-sm " +
          (current
            ? "bg-accent text-accent-foreground font-medium"
            : reachable
              ? "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
              : "text-muted-foreground/50");

        const note = permitted ? section.ticket : "not held";

        return (
          <li key={section.label}>
            {reachable ? (
              <Link
                href={section.href}
                aria-current={current ? "page" : undefined}
                className={className}
              >
                {section.label}
                <span className="text-muted-foreground/70 text-xs">{note}</span>
              </Link>
            ) : (
              <span
                aria-disabled
                title={
                  permitted ? undefined : `Your role does not hold ${section.permission}`
                }
                className={className}
              >
                {section.label}
                <span className="text-muted-foreground/70 text-xs">{note}</span>
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}
