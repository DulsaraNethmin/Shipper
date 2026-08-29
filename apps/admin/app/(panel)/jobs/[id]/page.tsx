import Link from "next/link";
import { notFound, redirect } from "next/navigation";

import { AuditTrail } from "@/components/audit-trail";
import { PanelTable, TD, TH, EmptyRow } from "@/components/panel-table";
import { PlatformRefusal, PlatformUnavailable } from "@/components/platform-refusal";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { platformHeaders } from "@/lib/credential";
import { instant, money, shortIdentifier } from "@/lib/format";
import { href, isIdentifier, one } from "@/lib/query";
import { answered, type Answer, platform } from "@/lib/upstream";

/**
 * One job, everything recorded against it, and its audit trail beneath (SHIP-188c).
 *
 * Served by `GET /v1/admin/jobs/{id}` (SHIP-152), which answers the job, every bid in every status
 * and every recorded transition **in one snapshot** — `JobConsole.Open` reads all three inside a
 * transaction precisely so this screen cannot show an `Awarded` job beside a bid list in which
 * nothing is accepted. A console making three calls to draw one screen is three chances to
 * contradict itself, and a support screen that contradicts itself is worse than a stale one because
 * somebody acts on it.
 *
 * The audit entries are a second endpoint and a separate component, and that is the one place this
 * screen is deliberately not a snapshot: the trail is append-only, so an entry arriving between the
 * two reads can only be a new row at the top.
 *
 * # The only place in this application where anything but `platform()` reaches a `/v1/` template
 *
 * Every other endpoint the panel names is a literal with one hole. This one has a path parameter, so
 * the segment is **checked before it is interpolated**: `isIdentifier` accepts thirty-six characters
 * of hexadecimal and hyphen and nothing else, and anything else is a 404 here rather than a request.
 * `new URL` would normalise a `..` and the platform would refuse a malformed identifier anyway —
 * this is the third lock, and it is the one that means no crafted segment is ever sent at all.
 *
 * # What "its parties" comes to, and the gap behind it
 *
 * The customer and every bidding provider are shown, **by identifier**, because that is all the
 * platform serves. `adminJobDetailResponse` carries `customer_id` and each bid's `provider_id` and
 * no names — and there is no endpoint that would resolve one: the twenty-four `/v1/admin/*` routes
 * have `GET /v1/admin/users` and no `GET /v1/admin/users/{id}`, and that search matches an address,
 * a name or a phone number, never an identifier. So the panel cannot name a party, and it does not
 * pretend to; each identifier links to what can actually be done with it. `Docs/11` §4 carries the
 * gap.
 */

interface JobDetailResponse {
  job: {
    id: string;
    customer_id: string;
    status: string;
    goods_description: string;
    bid_count: number;
    expires_at: string;
    created_at: string;
    updated_at: string;
  };
  bids: {
    id: string;
    provider_id: string;
    status: string;
    offered_by: string;
    amount_cents: number;
    pickup_at: string;
    deliver_by: string;
    message: string;
    superseded_by: string;
    created_at: string;
    updated_at: string;
  }[];
  history: {
    id: string;
    from: string;
    to: string;
    actor_type: string;
    actor_id: string;
    reason: string;
    actor_recorded_at: string;
    server_recorded_at: string;
  }[];
}

export default async function JobPage(props: PageProps<"/jobs/[id]">) {
  const { id } = await props.params;
  const params = await props.searchParams;

  // Before anything reaches a path. See the file note.
  if (!isIdentifier(id)) notFound();

  const headers = await platformHeaders();
  if (headers === null) redirect("/sign-in");

  let answer: Answer<JobDetailResponse>;
  try {
    const upstream = await fetch(`${platform()}/v1/admin/jobs/${id}`, {
      method: "GET",
      headers,
      cache: "no-store",
    });
    answer = await answered<JobDetailResponse>(upstream);
  } catch {
    answer = { state: "unavailable" };
  }

  if (answer.state === "signed-out") redirect("/sign-in");

  if (answer.state === "refused") {
    return (
      <div className="flex max-w-3xl flex-col gap-6">
        <Back />
        <PlatformRefusal title="That job could not be opened" refusal={answer.refusal} />
      </div>
    );
  }
  if (answer.state !== "ok") {
    return (
      <div className="flex max-w-3xl flex-col gap-6">
        <Back />
        <PlatformUnavailable title="The job could not be read" />
      </div>
    );
  }

  const { job, bids, history } = answer.body;

  // Every provider that has offered on this job, once each, oldest offer first. The bids arrive
  // oldest first, so insertion order into a Set is already the order to show them in.
  const providers = [...new Set(bids.map((bid) => bid.provider_id))];

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-3">
        <Back />
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">
            {job.goods_description === "" ? "Job with no description" : job.goods_description}
          </h1>
          <Badge variant="secondary">{job.status}</Badge>
        </div>
        <p className="text-muted-foreground font-mono text-xs">{job.id}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Parties</CardTitle>
          <CardDescription>
            By identifier. The platform serves no endpoint that resolves an account identifier to a
            name, so nothing here is a name it did not send.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <div>
            <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Customer
            </p>
            <Link
              href={href("/jobs", { customer: job.customer_id })}
              className="font-mono text-xs underline underline-offset-4"
            >
              {job.customer_id}
            </Link>
          </div>

          <div>
            <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Providers who have offered ({providers.length})
            </p>
            {providers.length === 0 ? (
              <p className="text-muted-foreground/70 text-xs italic">none</p>
            ) : (
              <ul className="space-y-1">
                {providers.map((provider) => (
                  <li key={provider} className="font-mono text-xs">
                    {provider}
                  </li>
                ))}
              </ul>
            )}
          </div>

          <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
            <Fact label="Published" value={instant(job.created_at)} />
            <Fact label="Last change" value={instant(job.updated_at)} />
            <Fact label="Expires" value={instant(job.expires_at)} />
            <Fact label="Bids" value={String(job.bid_count)} />
          </dl>
        </CardContent>
      </Card>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Status history</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Every recorded transition, oldest first, with{" "}
            <span className="text-foreground">both clocks</span> — what the actor recorded and when
            the platform accepted it. A driver records a milestone out of signal and the device syncs
            later, so a timeline showing one of them cannot answer why an update arrived when it did
            (<code className="font-mono text-xs">Docs/02</code> §3.1).
          </p>
        </div>

        <PanelTable>
          <thead>
            <tr>
              <TH>From</TH>
              <TH>To</TH>
              <TH>Actor</TH>
              <TH>Reason</TH>
              <TH>Actor recorded</TH>
              <TH>Platform accepted</TH>
            </tr>
          </thead>
          <tbody>
            {history.length === 0 ? (
              <EmptyRow span={6}>
                No transition recorded. A job that has never left Draft has none.
              </EmptyRow>
            ) : (
              history.map((event) => (
                <tr key={event.id}>
                  <TD className="whitespace-nowrap">{event.from}</TD>
                  <TD className="whitespace-nowrap">{event.to}</TD>
                  <TD>
                    <span className="block">{event.actor_type}</span>
                    {event.actor_id !== "" && (
                      <span
                        className="text-muted-foreground block font-mono text-xs"
                        title={event.actor_id}
                      >
                        {shortIdentifier(event.actor_id)}
                      </span>
                    )}
                  </TD>
                  <TD className="max-w-64">
                    {event.reason === "" ? (
                      <span className="text-muted-foreground/70 italic">none</span>
                    ) : (
                      event.reason
                    )}
                  </TD>
                  <TD className="whitespace-nowrap">{instant(event.actor_recorded_at)}</TD>
                  <TD className="whitespace-nowrap">{instant(event.server_recorded_at)}</TD>
                </tr>
              ))
            )}
          </tbody>
        </PanelTable>
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Bids</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Every offer in every status, oldest first, and which side made each one — a customer&rsquo;s
            counter-offer is a bid row like any other, and a reader who could not tell them apart
            would read a negotiation as one party bidding against themselves. The customer&rsquo;s
            budget is not here and is not on this endpoint: a bid is what a provider offered, and a
            budget is what the customer would pay and is visible to nobody but them.
          </p>
        </div>

        <PanelTable>
          <thead>
            <tr>
              <TH>Provider</TH>
              <TH>Offered by</TH>
              <TH className="text-right">Amount</TH>
              <TH>Status</TH>
              <TH>Pickup</TH>
              <TH>Deliver by</TH>
              <TH>Message</TH>
              <TH>Offered</TH>
            </tr>
          </thead>
          <tbody>
            {bids.length === 0 ? (
              <EmptyRow span={8}>No offer has been made on this job.</EmptyRow>
            ) : (
              bids.map((bid) => (
                <tr key={bid.id}>
                  <TD>
                    <span className="font-mono text-xs" title={bid.provider_id}>
                      {shortIdentifier(bid.provider_id)}
                    </span>
                  </TD>
                  <TD>{bid.offered_by}</TD>
                  <TD className="text-right tabular-nums whitespace-nowrap">
                    {money(bid.amount_cents)}
                  </TD>
                  <TD>
                    <Badge
                      variant={bid.status === "Accepted" ? "default" : "secondary"}
                      className="whitespace-nowrap"
                    >
                      {bid.status}
                    </Badge>
                    {/*
                      Which offer displaced this one, so a reader can follow a negotiation in the
                      order it happened. Empty on an offer nothing displaced.
                    */}
                    {bid.superseded_by !== "" && (
                      <span
                        className="text-muted-foreground block font-mono text-xs"
                        title={bid.superseded_by}
                      >
                        by {shortIdentifier(bid.superseded_by)}
                      </span>
                    )}
                  </TD>
                  <TD className="whitespace-nowrap">{instant(bid.pickup_at)}</TD>
                  <TD className="whitespace-nowrap">{instant(bid.deliver_by)}</TD>
                  <TD className="max-w-64">
                    {bid.message === "" ? (
                      <span className="text-muted-foreground/70 italic">none</span>
                    ) : (
                      bid.message
                    )}
                  </TD>
                  <TD className="whitespace-nowrap">{instant(bid.created_at)}</TD>
                </tr>
              ))
            )}
          </tbody>
        </PanelTable>
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Audit trail</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Every privileged action recorded against this job, newest first. Append-only: ordinary
            administrators cannot delete an entry, and the database enforces it rather than the
            application.
          </p>
        </div>

        <AuditTrail
          query={{
            basePath: `/jobs/${job.id}`,
            target: job.id,
            cursor: one(params.cursor),
            emptyMessage:
              "Nothing privileged has been done to this job. Ordinary marketplace activity — " +
              "publishing, bidding, milestones — is in the status history above rather than here.",
          }}
        />
      </section>
    </div>
  );
}

function Back() {
  return (
    <Link href="/jobs" className="text-muted-foreground text-sm underline underline-offset-4">
      ← Back to the job search
    </Link>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{label}</dt>
      <dd className="whitespace-nowrap">{value}</dd>
    </div>
  );
}
