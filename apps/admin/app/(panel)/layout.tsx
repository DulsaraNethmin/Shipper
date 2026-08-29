import { redirect } from "next/navigation";

import { AdminShell } from "@/components/admin-shell";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { viewer } from "@/lib/administrator";

/**
 * The door to every authenticated screen (SHIP-188a).
 *
 * **The check is here rather than in each page**, so a screen added by SHIP-188b, SHIP-188c or
 * SHIP-188d is behind it by existing in this group rather than by remembering to ask. A guard a
 * page has to opt into is one a page will eventually not opt into.
 *
 * It is **not** an authorisation decision (`Docs/07` §3). It decides nothing about what this
 * administrator may see — it resolves the cookie into an administrator by asking the platform, and
 * every screen inside it asks the platform again for everything it renders. What it prevents is a
 * shell drawn around a session that does not exist, which is a rendering question rather than a
 * security one; the security is that the platform refuses the request either way.
 *
 * # Three answers, three screens
 *
 * `Docs/09`'s *Done when* asks that "an absent or expired cookie lands on the sign-in form rather
 * than a blank screen". A platform that is not answering is neither of those, and sending that case
 * to the sign-in form would tell an administrator their credential is bad when what is bad is the
 * platform — so it gets a screen that says what is actually wrong. `lib/administrator.ts` argues the
 * distinction where the three answers are produced.
 */
export default async function PanelLayout({ children }: LayoutProps<"/">) {
  const who = await viewer();

  if (who.state === "signed-out") redirect("/sign-in");

  if (who.state === "unavailable") {
    return (
      <div className="mx-auto flex min-h-dvh max-w-xl flex-col justify-center p-6">
        <Card>
          <CardHeader>
            <CardTitle>The platform is not answering</CardTitle>
            <CardDescription>
              The panel reached for your session and got nothing back.
            </CardDescription>
          </CardHeader>
          <CardContent className="text-muted-foreground space-y-3 text-sm">
            <p>
              You have not been signed out — your session is intact and this screen will give way
              to the panel as soon as the platform answers again. Reload in a moment.
            </p>
            <p>
              If this persists, the API the panel is configured against is unreachable rather than
              refusing. That is an operational problem rather than one with your account.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return <AdminShell administrator={who.administrator}>{children}</AdminShell>;
}
