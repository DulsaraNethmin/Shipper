import type { ReactNode } from "react";

import { SignOutButton } from "@/components/sign-out-button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import type { Administrator } from "@/lib/administrator";

/**
 * The panel's sections, each named with the permission the platform requires of it and the ticket
 * that builds it.
 *
 * **The permission is read to hide and to disable, and never to decide** (`Docs/07` §3, and
 * `internal/admin/http.go`'s `administratorResponse` says the same thing from the other side). An
 * administrator who edited the array this is checked against would find every endpoint refusing
 * them exactly as before, because the platform reads the role out of the session row on each
 * request. What this buys is an administrator not clicking into a screen that was only ever going
 * to answer 403.
 *
 * They are still inert. SHIP-188b, SHIP-188c and SHIP-188d build the three the demonstration needs;
 * a nav item that navigated to a 404 would be worse than one that plainly says it is not built yet.
 *
 * **Reports is listed and has no ticket that builds it**, which is deliberate rather than an
 * oversight to tidy away. `Docs/11` §5 has struck SHIP-156 for eleven consecutive passes: there is
 * no `reports` table, no report route among the twenty-four, and no row anywhere that creates one.
 * Deleting the row from this list would remove the last place the gap is visible to somebody who
 * does not read the delivery status document.
 */
const SECTIONS = [
  { label: "Overview", ticket: "SHIP-188a", permission: null, built: true },
  { label: "Users", ticket: "SHIP-188b", permission: "users.read", built: false },
  { label: "Jobs and bids", ticket: "SHIP-188b", permission: "jobs.read", built: false },
  { label: "Job detail and audit", ticket: "SHIP-188c", permission: "audit.read", built: false },
  { label: "Verification queue", ticket: "SHIP-188d", permission: "verifications.read", built: false },
  { label: "Reports", ticket: "SHIP-156", permission: "moderation.read", built: false },
  { label: "Delivery exceptions", ticket: "SHIP-157", permission: "moderation.read", built: false },
  { label: "Disputes", ticket: "SHIP-164", permission: "disputes.read", built: false },
] as const;

/**
 * AdminShell is the authenticated chrome (SHIP-22, filled in by SHIP-188a).
 *
 * It renders what an administrator sees around every screen: the surface's identity, its
 * navigation, who is signed in and what they hold, and a way out. Everything on it comes from `GET
 * /v1/admin/me`, resolved on the server against the httpOnly cookie — so the shell cannot be drawn
 * at all without a credential the platform has just accepted, and there is nothing here for a
 * client to have decided.
 *
 * The scaffold that stood here said "Not signed in" and "Administrator sign-in arrives with
 * SHIP-147". Both are now false, which is why this file changed rather than gaining a branch.
 */
export function AdminShell({
  administrator,
  children,
}: {
  administrator: Administrator;
  children: ReactNode;
}) {
  const held = new Set(administrator.permissions);

  return (
    <div className="flex min-h-dvh flex-col">
      <header className="border-border flex flex-wrap items-center gap-3 border-b px-6 py-4">
        <span className="text-lg font-semibold tracking-tight">Shipper Admin</span>
        <Badge variant="secondary">{administrator.role}</Badge>
        <div className="ml-auto flex items-center gap-3 text-sm">
          <span className="text-foreground font-medium">{administrator.name}</span>
          <Separator orientation="vertical" className="h-4" />
          <span className="text-muted-foreground">{administrator.email}</span>
          <Separator orientation="vertical" className="h-4" />
          <SignOutButton />
        </div>
      </header>

      <div className="flex flex-1 flex-col md:flex-row">
        <nav
          aria-label="Sections"
          className="border-border w-full shrink-0 border-b p-4 md:w-72 md:border-r md:border-b-0"
        >
          <ul className="space-y-1">
            {SECTIONS.map((section) => {
              const permitted = section.permission === null || held.has(section.permission);
              return (
                <li key={section.label}>
                  <span
                    aria-current={section.built ? "page" : undefined}
                    aria-disabled={!section.built}
                    title={
                      permitted
                        ? undefined
                        : `Your role does not hold ${section.permission}`
                    }
                    className={
                      "flex items-baseline justify-between gap-2 rounded-md px-3 py-2 text-sm " +
                      (section.built
                        ? "bg-accent text-accent-foreground font-medium"
                        : permitted
                          ? "text-muted-foreground"
                          : "text-muted-foreground/50")
                    }
                  >
                    {section.label}
                    <span className="text-muted-foreground/70 text-xs">
                      {permitted ? section.ticket : "not held"}
                    </span>
                  </span>
                </li>
              );
            })}
          </ul>

          <Separator className="my-4" />

          <div className="space-y-2">
            <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Permissions held
            </p>
            <ul className="flex flex-wrap gap-1">
              {administrator.permissions.length === 0 ? (
                <li className="text-muted-foreground text-xs">None</li>
              ) : (
                administrator.permissions.map((permission) => (
                  <li key={permission}>
                    <Badge variant="secondary" className="font-mono text-[0.7rem]">
                      {permission}
                    </Badge>
                  </li>
                ))
              )}
            </ul>
          </div>
        </nav>

        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  );
}
