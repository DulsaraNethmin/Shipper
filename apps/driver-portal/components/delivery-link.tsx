"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { REFUSALS, openDelivery, type Delivery, type Refusal } from "@/lib/delivery";
import { dayFirst } from "@/lib/format";
import { isJobId, recallToken, rememberToken, tokenFromFragment } from "@/lib/link";

/**
 * The delivery a link opens (SHIP-120).
 *
 * This is a client component and it has to be: the token arrives in the URL fragment, which no
 * server ever receives, and that is the whole point of putting it there (`lib/link.ts`). A server
 * component could not read it, so the only shapes available were a credential in the path — logged
 * at every hop, for as long as those logs are kept — or this.
 *
 * Nothing here decides anything. It reads the link, presents it to the platform, and renders the
 * answer. There is no filtering, no "is this job mine", and no branch that could hide a delivery
 * the platform was willing to serve: on this surface the platform decides and the page displays
 * (`Docs/07` §3).
 */

/** What the page is showing. */
type View =
  | { kind: "opening" }
  | { kind: "missing" }
  | { kind: "delivery"; delivery: Delivery }
  | { kind: "refused"; refusal: Refusal };

/**
 * The token for this page view, taken from the fragment if the driver has just arrived and from
 * this tab's storage if they have reloaded.
 *
 * **The fragment is stripped only once the token is somewhere a reload can find it.** A browser
 * that refuses storage keeps a working link in its address bar, which is the worse of the two
 * exposures and much better than a page that cannot survive the pull-to-refresh a driver on one
 * bar of signal will certainly do.
 */
function tokenForThisView(jobId: string): string | null {
  const arriving = tokenFromFragment(window.location.hash);
  if (arriving === null) return recallToken(jobId);

  if (rememberToken(jobId, arriving)) {
    window.history.replaceState(null, "", window.location.pathname);
  }
  return arriving;
}

/**
 * One attempt at opening the link, from the address bar through to a view.
 *
 * `async` and outside the component on purpose. The whole sequence — recognise the identifier,
 * take the token, ask the platform — resolves to a single view, so the component never sets state
 * partway through and `react-hooks/set-state-in-effect` has nothing to object to. It is also the
 * shape that reads correctly: there is one question, "what does this link open", and one answer.
 */
async function openLink(jobId: string, signal: AbortSignal): Promise<View> {
  // A path segment that is not a job identifier never reaches the platform. That is not the page
  // deciding anything — there is nothing here to decide about, because there is no delivery this
  // could be naming and no request worth making.
  if (!isJobId(jobId)) return { kind: "refused", refusal: "invalid" };

  const token = tokenForThisView(jobId);
  if (token === null) return { kind: "missing" };

  const outcome = await openDelivery(jobId, token, signal);
  return outcome.kind === "delivery"
    ? { kind: "delivery", delivery: outcome.delivery }
    : { kind: "refused", refusal: outcome.refusal };
}

export function DeliveryLink({ jobId }: { jobId: string }) {
  const [view, setView] = useState<View>({ kind: "opening" });
  const inFlight = useRef<AbortController | null>(null);

  const open = useCallback(() => {
    inFlight.current?.abort();
    const controller = new AbortController();
    inFlight.current = controller;

    void openLink(jobId, controller.signal).then(
      (next) => {
        if (!controller.signal.aborted) setView(next);
      },
      () => {
        // openLink resolves rather than rejects: openDelivery turns every failure into an outcome,
        // and the link reading cannot throw. A browser that surprises us still gets a page.
        if (!controller.signal.aborted) setView({ kind: "refused", refusal: "unexpected" });
      },
    );
  }, [jobId]);

  useEffect(() => {
    open();
    return () => inFlight.current?.abort();
  }, [open]);

  const again = useCallback(() => {
    setView({ kind: "opening" });
    open();
  }, [open]);

  if (view.kind === "opening") return <Opening />;
  if (view.kind === "missing") return <NoLink />;
  if (view.kind === "refused") return <Refused refusal={view.refusal} onRetry={again} />;
  return <Detail delivery={view.delivery} />;
}

/** The shell every state renders inside, so the page does not jump as one replaces another. */
function Frame({ children }: { children: React.ReactNode }) {
  return <main className="mx-auto flex max-w-md flex-col gap-4 p-4 pb-16">{children}</main>;
}

function Opening() {
  return (
    <Frame>
      <h1 className="text-xl font-semibold tracking-tight">Delivery</h1>
      <p className="text-muted-foreground text-sm" role="status">
        Opening your delivery…
      </p>
    </Frame>
  );
}

/**
 * The address bar has a job identifier in it and no credential.
 *
 * Deliberately not one of the [REFUSALS]: nothing was refused, because nothing was asked. The
 * instruction is the useful part — the message thread is where the working link lives.
 */
function NoLink() {
  return (
    <Frame>
      <h1 className="text-xl font-semibold tracking-tight">Open the link you were sent</h1>
      <p className="text-muted-foreground text-sm">
        This page needs the full link from the message the transport provider sent you. Open it
        again from that message rather than from your browser history.
      </p>
      <p className="text-muted-foreground text-sm">
        There is no account to sign in to, so keep that message — it is the way back in.
      </p>
    </Frame>
  );
}

function Refused({ refusal, onRetry }: { refusal: Refusal; onRetry: () => void }) {
  const { title, body } = REFUSALS[refusal];
  const retryable = refusal === "unavailable" || refusal === "unexpected";

  return (
    <Frame>
      <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
      <p className="text-muted-foreground text-sm">{body}</p>
      {retryable ? (
        <Button size="lg" className="h-12 justify-center text-base" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </Frame>
  );
}

/** One labelled fact. */
function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-sm">{value}</dd>
    </div>
  );
}

/**
 * The delivery itself — five fields, because five is what the platform serves.
 *
 * Every one of them is rendered. `Docs/11` §3 records the reasoning: the endpoint's response *is*
 * the answer to "what may a driver see", so a page that quietly dropped a field would be taking
 * that decision back off the platform, and a page that added one would be inventing it. The two
 * identifiers are the least interesting to look at and the most useful on a phone call, which is
 * why they are small and at the bottom rather than absent.
 */
function Detail({ delivery }: { delivery: Delivery }) {
  const assigned = dayFirst(delivery.assigned_at);
  const expires = dayFirst(delivery.link_expires_at);

  return (
    <Frame>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">Delivery</h1>
        <p className="text-muted-foreground text-sm">
          This link is for {delivery.driver_name}.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Your assignment</CardTitle>
          <CardDescription>
            The transport provider put you on this job. It is the only one this link opens.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <dl className="flex flex-col gap-3">
            <Fact label="Driver" value={delivery.driver_name} />
            <Fact label="Assigned" value={assigned ?? "Not recorded"} />
            <Fact
              label="This link works until"
              value={expires ?? "Not recorded"}
            />
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Pickup and drop-off</CardTitle>
          <CardDescription>
            Where to collect from, where to take it, what the goods are and who to call.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-muted-foreground text-sm">
          <p>
            Not carried by this link yet. Until it is, the transport provider who sent you here has
            the details.
          </p>
        </CardContent>
      </Card>

      <p className="text-muted-foreground text-sm">
        Recording milestones and capturing proof of delivery are not part of this page yet either.
      </p>

      <Separator />

      <section className="text-muted-foreground flex flex-col gap-3 text-xs">
        <p className="text-foreground font-medium">If you need to quote this delivery</p>
        <dl className="flex flex-col gap-3">
          <div className="flex flex-col gap-0.5">
            <dt>Job</dt>
            <dd className="text-foreground font-mono break-all">{delivery.job_id}</dd>
          </div>
          <div className="flex flex-col gap-0.5">
            <dt>Assignment</dt>
            <dd className="text-foreground font-mono break-all">{delivery.assignment_id}</dd>
          </div>
        </dl>
        <p>
          Keep the message this link came in. There is no account and no password, so that message
          is the way back into this page.
        </p>
      </section>
    </Frame>
  );
}
