import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";

/**
 * The milestones a driver records, named exactly as Docs/02 §1 writes them so the Dart, Go
 * and TypeScript copies can each be diffed against the document rather than against one
 * another. They are inert here; recording them is SHIP-121.
 */
const MILESTONES = [
  "En route to pickup",
  "Picked up",
  "In transit",
  "Delivered",
] as const;

/**
 * The placeholder delivery page (SHIP-23).
 *
 * It is deliberately not parameterised by a token. The real page is reached at a
 * token-bearing URL and renders only after the platform has validated that token
 * (SHIP-108, SHIP-120); a route here that accepted one would be a shape inviting somebody
 * to render a job beside a token this application never checked. Authorisation is the
 * platform's decision, never the client's (Docs/07 §3).
 *
 * Nothing on this page is fetched, and no component here takes job data as a prop. The
 * fields a driver may see are settled with SHIP-120 — but two things are already fixed and
 * are called out below, because a placeholder that quietly assumed otherwise is how they
 * would get built in.
 */
export default function PlaceholderJobPage() {
  return (
    <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-16">
      <header className="flex items-center gap-2">
        <h1 className="text-xl font-semibold tracking-tight">Delivery</h1>
        <Badge variant="secondary">Placeholder</Badge>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Pickup and drop-off</CardTitle>
          <CardDescription>
            The delivery detail for this one job appears here (SHIP-120).
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground space-y-2 text-sm">
          <p>No data is loaded. This page reads nothing from the API yet.</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Progress</CardTitle>
          <CardDescription>
            Each milestone is recorded from here, one tap, no typing (SHIP-121).
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          {MILESTONES.map((milestone) => (
            <Button
              key={milestone}
              variant="outline"
              size="lg"
              disabled
              /* Large targets: this is used one-handed, outdoors, on a phone (SHIP-121). */
              className="h-14 justify-start text-base"
            >
              {milestone}
            </Button>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Proof of delivery</CardTitle>
          <CardDescription>
            A photograph, or a recorded reason why there is none — never neither.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground space-y-2 text-sm">
          <p>
            Capture goes through the browser camera and uploads straight to private storage
            with a short-lived signed URL. The photograph never passes through the API
            (SHIP-122).
          </p>
        </CardContent>
      </Card>

      <Separator />

      <section className="text-muted-foreground space-y-2 text-xs">
        <p className="text-foreground font-medium">Two things this page will never show</p>
        <p>
          <span className="text-foreground">Anything about money.</span> The customer&rsquo;s
          budget is never exposed to a provider or to their driver, in any form — not an
          amount, not a range, not a note that one was given. Neither are bids or
          negotiations.
        </p>
        <p>
          <span className="text-foreground">Anything about another job.</span> The link
          reaches exactly one delivery. No other job, no customer history, and no account
          surface is reachable from here, whatever is typed into the address bar — and the
          platform enforces that rather than this page hiding it.
        </p>
      </section>
    </main>
  );
}
