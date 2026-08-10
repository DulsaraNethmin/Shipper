import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

/**
 * The placeholder screen inside the shell (SHIP-22).
 *
 * It renders no marketplace data at all — not even an empty table with columns. A table of
 * jobs would have to decide which columns exist, and one of the answers to that is a
 * customer's budget, which no provider-facing or half-designed surface should ever be
 * given the chance to show (Docs/01 §4.3). The screens that need those decisions make them
 * with their own tickets.
 */
export default function OverviewPage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          The administration panel is scaffolded and serves this shell. Nothing here reads
          from the API yet.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>What this surface is for</CardTitle>
          <CardDescription>
            Support and moderation for the marketplace — separate by design from the
            customer and provider experience.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground space-y-3 text-sm">
          <p>
            Administrators search users, jobs and bids, review provider verification
            evidence, work the reported-content and delivery-exception queues, and resolve
            disputes. All of that is milestone M6.
          </p>
          <p>
            The panel consumes the same versioned public API as the mobile app and the
            driver portal. It keeps its own server-side data access for privileged screens,
            which is an application detail rather than a shared platform tier — there is no
            BFF.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Decided already, before any screen is built</CardTitle>
          <CardDescription>
            These are constraints on what may be built here, not preferences.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ul className="text-muted-foreground space-y-3 text-sm">
            <li>
              <span className="text-foreground font-medium">
                Administrator sign-in is a separate system.
              </span>{" "}
              It is independent of user authentication and can never be reached with a
              user&rsquo;s token. That is SHIP-147, and it is why this scaffold has no
              sign-in form of its own.
            </li>
            <li>
              <span className="text-foreground font-medium">
                Permissions are granular and default to the minimum.
              </span>{" "}
              An administrator holds the authorisation for the task in front of them and no
              more (SHIP-148).
            </li>
            <li>
              <span className="text-foreground font-medium">
                Every privileged change is audited, and the audit is append-only.
              </span>{" "}
              Ordinary administrators cannot delete an entry; the database enforces it
              rather than the application (SHIP-149, SHIP-150).
            </li>
            <li>
              <span className="text-foreground font-medium">
                Verification evidence renders through short-lived signed URLs.
              </span>{" "}
              Every view is itself logged, because the documents are private evidence about
              a real person or organisation (SHIP-155).
            </li>
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
