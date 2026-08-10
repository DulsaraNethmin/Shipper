import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";

/**
 * The M6 screens, listed here so the shape of the panel is visible before any of them
 * exists. Each is inert: a nav item that navigated to a 404 would be worse than one that
 * plainly says it is not built yet.
 *
 * They are labelled with the ticket that builds them, because "why is this greyed out" has
 * a better answer than "not yet".
 */
const SECTIONS = [
  { label: "Overview", ticket: "SHIP-22", current: true },
  { label: "Users", ticket: "SHIP-151" },
  { label: "Jobs and bids", ticket: "SHIP-152" },
  { label: "Verification queue", ticket: "SHIP-153" },
  { label: "Reports", ticket: "SHIP-156" },
  { label: "Delivery exceptions", ticket: "SHIP-157" },
  { label: "Disputes", ticket: "SHIP-164" },
  { label: "Audit log", ticket: "SHIP-165" },
] as const;

/**
 * AdminShell is the placeholder authenticated chrome (SHIP-22).
 *
 * It renders what an administrator will see around every screen — the surface's identity,
 * its navigation, and who is signed in — and nothing that would need real data or a real
 * session to be truthful.
 *
 * There is deliberately **no sign-in form**. Admin authentication is SHIP-147, it is a
 * separate system from user authentication, and it cannot be reached with a user token. A
 * placeholder form here would be a login shaped hole that somebody eventually wires to the
 * wrong verifier.
 */
export function AdminShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col">
      <header className="border-border flex items-center gap-3 border-b px-6 py-4">
        <span className="text-lg font-semibold tracking-tight">Shipper Admin</span>
        <Badge variant="secondary">Scaffold</Badge>
        <div className="ml-auto flex items-center gap-3 text-sm">
          <span className="text-muted-foreground">Not signed in</span>
          <Separator orientation="vertical" className="h-4" />
          <span className="text-muted-foreground">
            Administrator sign-in arrives with SHIP-147
          </span>
        </div>
      </header>

      <div className="flex flex-1 flex-col md:flex-row">
        <nav
          aria-label="Sections"
          className="border-border w-full shrink-0 border-b p-4 md:w-64 md:border-r md:border-b-0"
        >
          <ul className="space-y-1">
            {SECTIONS.map((section) => (
              <li key={section.label}>
                <span
                  aria-current={"current" in section && section.current ? "page" : undefined}
                  aria-disabled={!("current" in section && section.current)}
                  className={
                    "flex items-baseline justify-between rounded-md px-3 py-2 text-sm " +
                    ("current" in section && section.current
                      ? "bg-accent text-accent-foreground font-medium"
                      : "text-muted-foreground")
                  }
                >
                  {section.label}
                  <span className="text-muted-foreground/70 text-xs">{section.ticket}</span>
                </span>
              </li>
            ))}
          </ul>
        </nav>

        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  );
}
