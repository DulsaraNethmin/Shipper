import { redirect } from "next/navigation";

import { SignInForm } from "@/components/sign-in-form";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { viewer } from "@/lib/administrator";

/**
 * The panel's door (SHIP-188a).
 *
 * It is outside the `(panel)` route group, so it is not wrapped in the shell — there is no
 * administrator to draw one around yet.
 *
 * # Why it redirects an administrator who is already signed in
 *
 * Otherwise a bookmarked `/sign-in` shows a form to somebody with a working session, whose only
 * outcomes are a second session they did not want or a refusal they cannot explain. The predicate
 * is the same one `app/(panel)/layout.tsx` uses, in the opposite direction, so there is no pair of
 * screens that can bounce a browser between them: `signed-in` is redirected from here and admitted
 * there, `signed-out` is admitted here and redirected there, and `unavailable` — the platform not
 * answering — is admitted by both, because somebody who cannot sign in should be able to see the
 * form and try.
 */
export default async function SignInPage() {
  if ((await viewer()).state === "signed-in") redirect("/");

  return (
    <div className="mx-auto flex min-h-dvh w-full max-w-sm flex-col justify-center gap-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Shipper Admin</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Support and moderation for the marketplace.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>
            Administrator accounts are a separate system from customer and provider accounts. An app
            sign-in will not work here.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <SignInForm />
        </CardContent>
      </Card>
    </div>
  );
}
