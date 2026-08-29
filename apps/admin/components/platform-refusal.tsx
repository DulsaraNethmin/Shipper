import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { Refusal } from "@/lib/upstream";

/**
 * What a screen shows when the platform said no (SHIP-188b).
 *
 * # The clause this component exists for
 *
 * `Docs/09`'s SHIP-188b row: "a search made by a session without the permission renders the
 * platform's refusal rather than an empty table". That sentence names the failure it is guarding
 * against, and it is a failure of *rendering* rather than of security — the platform refused
 * correctly either way. What an empty table does is tell a support engineer their search found
 * nothing, when what happened is that their role does not hold `users.read`. They then search again,
 * differently, for something that was never going to be shown to them, and conclude the account does
 * not exist.
 *
 * # The platform's own words, not a local translation
 *
 * `message` is rendered as it arrived. The platform is the component that knows which no this is,
 * `Docs/10` §4.6 writes those messages for a person to read, and the field errors for a mistyped
 * status or an expired cursor are in them. A panel that substituted its own copy would drift from
 * the platform silently and would lose all of that — and `Docs/07` §3 is the general form of the
 * same rule: the client renders the decision, it does not make one.
 *
 * `code` is shown too, small and in monospace. It is what a client branches on rather than something
 * a person acts on, but it is also what somebody puts in a message to whoever can grant them the
 * permission — and `forbidden` beside a sentence is the difference between "the panel is broken" and
 * "my role is short one thing".
 *
 * # The field problems are rendered, and leaving them out was the first version of this
 *
 * A `validation_failed` answers with "Some of the details you entered need attention" and puts the
 * sentence that matters — which field, and what it will accept — in `details`. The card that showed
 * only the top-level message was demonstrably worse than useless: a mistyped standing produced a
 * refusal that named neither the field nor the three values, so the only way forward was to guess.
 * `Docs/10` §4.6 has the platform report every problem rather than the first, and this is the half of
 * that which reaches a person.
 *
 * The request identifier is shown for the same reason it is in every error body: the person reading
 * this screen is usually not the person who can read the logs, and one they can quote turns "the
 * panel would not let me" into something somebody can look up.
 */
export function PlatformRefusal({ title, refusal }: { title: string; refusal: Refusal }) {
  return (
    <Card role="alert" className="border-destructive/40">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>The platform refused this request. Nothing was changed.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <p className="text-foreground">{refusal.message}</p>

        {refusal.details.length > 0 && (
          <ul className="space-y-1">
            {refusal.details.map((problem, at) => (
              <li key={`${problem.field}-${at}`} className="text-foreground">
                {problem.field !== "" && (
                  <span className="text-muted-foreground font-mono text-xs">{problem.field}: </span>
                )}
                {problem.message}
              </li>
            ))}
          </ul>
        )}

        <p className="text-muted-foreground font-mono text-xs">
          {refusal.code}
          {refusal.requestId !== "" && <> · request {refusal.requestId}</>}
        </p>
      </CardContent>
    </Card>
  );
}

/**
 * What a screen shows when the platform did not answer at all.
 *
 * Separate from a refusal for the reason `lib/administrator.ts` keeps `unavailable` apart from
 * `signed-out`: an outage rendered as a refusal sends somebody looking at their own permissions, and
 * a refusal rendered as an outage sends them to reload a page that will refuse again. Neither person
 * can act on the wrong one.
 */
export function PlatformUnavailable({ title }: { title: string }) {
  return (
    <Card role="alert">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>The panel reached for this and got nothing back.</CardDescription>
      </CardHeader>
      <CardContent className="text-muted-foreground space-y-3 text-sm">
        <p>
          You are still signed in. Reload in a moment — if this persists, the API the panel is
          configured against is unreachable rather than refusing, which is an operational problem
          rather than one with your account.
        </p>
      </CardContent>
    </Card>
  );
}
