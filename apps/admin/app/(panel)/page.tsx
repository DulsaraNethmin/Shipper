import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

/**
 * The first authenticated screen (SHIP-22, rewritten by SHIP-188a).
 *
 * It still renders no marketplace data. A table of jobs would have to decide which columns exist,
 * and one of the answers to that is a customer's budget, which no admin surface should be given the
 * chance to show by accident (`Docs/01` §4.3). SHIP-188b, SHIP-188c and SHIP-188d make those
 * decisions with their own tickets and their own reading of that section.
 *
 * What is different from the scaffold is that reaching this page now means something: it is behind
 * `app/(panel)/layout.tsx`, so it cannot be drawn without a credential the platform accepted on
 * this request.
 */
export default function OverviewPage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          You are signed in. The panel reads your name, role and permissions from the platform on
          every render — the strip above and the list beside it are that answer, not a cached copy.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>What this surface is for</CardTitle>
          <CardDescription>
            Support and moderation for the marketplace — separate by design from the customer and
            provider experience.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground space-y-3 text-sm">
          <p>
            Administrators search users, jobs and bids, review provider verification evidence, work
            the reported-content and delivery-exception queues, and resolve disputes. The platform
            serves all of it today; the screens arrive with SHIP-188b, SHIP-188c and SHIP-188d.
          </p>
          <p>
            The panel consumes the same versioned public API as the mobile app and the driver
            portal. It keeps its own server-side data access for privileged screens, which is an
            application detail rather than a shared platform tier — there is no BFF.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>How your session is held</CardTitle>
          <CardDescription>
            Decided at SHIP-188a, and different from the driver portal&rsquo;s answer on purpose.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ul className="text-muted-foreground space-y-3 text-sm">
            <li>
              <span className="text-foreground font-medium">
                Your token is in an httpOnly cookie and no script can read it.
              </span>{" "}
              It is set by a route handler on this origin, appears in no response body, and is in no
              browser storage. A driver&rsquo;s job-scoped link is held differently because it grants
              one delivery; this session reaches every account.
            </li>
            <li>
              <span className="text-foreground font-medium">
                Signing out ends the session at the platform.
              </span>{" "}
              Not just here — the row is deleted, so the same credential replayed afterwards is
              refused. There is no window, because the platform re-reads that row on every request.
            </li>
            <li>
              <span className="text-foreground font-medium">
                Permissions are granular and default to the minimum.
              </span>{" "}
              What is listed beside the navigation is what your role holds (SHIP-148). The panel
              reads it to hide and disable; every decision is still the platform&rsquo;s, on every
              request.
            </li>
            <li>
              <span className="text-foreground font-medium">
                Every privileged change is audited, and the audit is append-only.
              </span>{" "}
              Ordinary administrators cannot delete an entry; the database enforces it rather than
              the application (SHIP-149, SHIP-150).
            </li>
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
