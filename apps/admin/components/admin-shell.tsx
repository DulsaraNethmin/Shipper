import type { ReactNode } from "react";

import { PanelNav } from "@/components/panel-nav";
import { SignOutButton } from "@/components/sign-out-button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import type { Administrator } from "@/lib/administrator";

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
 *
 * **The section list left this file with SHIP-188b and lives in `components/panel-nav.tsx`.**
 * Marking the section being read needs the current path, `usePathname` needs a client module, and
 * this file must not become one: it is what renders the administrator, and keeping it on the server
 * is half of why no bundle in this application contains anything about the credential. The shell
 * hands the navigation the permissions and nothing else.
 */
export function AdminShell({
  administrator,
  children,
}: {
  administrator: Administrator;
  children: ReactNode;
}) {
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
          <PanelNav permissions={administrator.permissions} />

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
