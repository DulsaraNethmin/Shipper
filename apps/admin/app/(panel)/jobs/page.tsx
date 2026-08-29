import Link from "next/link";
import { redirect } from "next/navigation";

import { Pager } from "@/components/pager";
import { EmptyRow, PanelTable, TD, TH } from "@/components/panel-table";
import { PlatformRefusal, PlatformUnavailable } from "@/components/platform-refusal";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { platformHeaders } from "@/lib/credential";
import { instant, shortIdentifier } from "@/lib/format";
import { href, isIdentifier, one } from "@/lib/query";
import { jobStatusLabels, jobStatusValues } from "@/lib/statuses.gen";
import { answered, type Answer, type Page, platform } from "@/lib/upstream";

/**
 * The job search (SHIP-188b), served by `GET /v1/admin/jobs` (SHIP-152).
 *
 * # What the columns are, and why the list is shorter than it could be
 *
 * `adminJobResponse` carries no budget, no addresses and no contact details, and this screen shows
 * everything it does carry. That shape is deliberate on the Go side and is held to its key set by
 * `TestTheAdminJobShapesCarryNothingPrivate` — an assertion about what is *present*, because SHIP-83
 * established that searching a response for the word "budget" is not the check that catches a leak: a
 * field called `max_price` passes that search and discloses the same fact. `Docs/01` §4.3 is about
 * providers rather than administrators, so nothing here would breach it; what applies is `Docs/01`
 * §5.1's minimisation, and the endpoint having no such field is a stronger guarantee than a panel
 * choosing not to render one.
 *
 * # The status filter takes the stored form, and that decides where its values come from
 *
 * `GET /v1/admin/jobs` filters on and returns `Docs/02` §1's **stored** spelling — `En route to
 * pickup`, not `en_route_to_pickup`. `adminJobResponse.Status` records why: an operator reading this
 * screen beside a `psql` window should see one vocabulary rather than two, and the filter and the
 * display have to agree or every filter is a guess.
 *
 * So the options are the labels of `lib/statuses.gen.ts` rather than its values, and the module is
 * generated from `contracts/statuses.yaml` rather than typed here — which is the whole of SHIP-56a
 * and is why this panel got a second output rather than a hand-written list of twelve strings.
 *
 * # A term that is an identifier is the job, and not a description to match against
 *
 * The last of `Docs/09`'s SHIP-188b terms — "finds … a job by identifier" — and the platform serves
 * nothing for it: `SearchJobs` matches `q` against `goods_description` and only that. An operator who
 * pastes a job identifier out of a log line, an audit entry or another screen would otherwise read an
 * empty table, which is the single most misleading answer this screen can give — it says the job does
 * not exist, about a job they are holding the identifier of.
 *
 * So an identifier is recognised and the browser is sent to the job. The alternative was a new `id`
 * filter on an endpoint SHIP-152 already closed — platform work inside a panel ticket, and a second
 * way of asking a question `GET /v1/admin/jobs/{id}` already answers exactly.
 *
 * **It is `redirect` rather than a link offered beside the table**, so the URL a person ends on is
 * the job's own and can be pasted into a ticket. Nothing is lost: the detail screen renders the
 * platform's `not_found` for an identifier that names no job, which is a better answer than a search
 * that quietly found nothing.
 */

/** One job as `adminJobResponse` serves it. Nothing commercial and nothing locating. */
interface JobRow {
  id: string;
  customer_id: string;
  status: string;
  goods_description: string;
  bid_count: number;
  expires_at: string;
  created_at: string;
  updated_at: string;
}

/**
 * The statuses to offer, in `Docs/02` §1's own order.
 *
 * The *labels* rather than the values: the generated module keys on the wire form because that is
 * what a client branches on, and the label is the stored form — which is what this endpoint both
 * accepts and returns. Mapping here rather than hand-listing keeps the one source, and a status added
 * to the specification appears in this filter with no edit at all.
 */
const JOB_STATUSES = jobStatusValues.map((value) => jobStatusLabels[value]);

export default async function JobsPage(props: PageProps<"/jobs">) {
  const params = await props.searchParams;
  const term = one(params.q);
  const status = one(params.status);
  const customer = one(params.customer);
  const cursor = one(params.cursor);

  // Before the credential is even read: this is navigation rather than a search, and the job's own
  // screen asks the platform for what it needs. See the file note.
  if (isIdentifier(term)) redirect(`/jobs/${term}`);

  const headers = await platformHeaders();
  if (headers === null) redirect("/sign-in");

  // One template, one hole, and the hole is where the platform is. The query goes on afterwards
  // through `URLSearchParams`, so nothing typed into a search box can reach the path — see
  // `app/api/admin/sessions/route.ts` on why this application has no shared forwarder to pass a
  // path to.
  const endpoint = new URL(`${platform()}/v1/admin/jobs`);
  const search = new URLSearchParams();
  if (term !== "") search.set("q", term);
  if (status !== "") search.set("status", status);
  if (customer !== "") search.set("customer", customer);
  if (cursor !== "") search.set("cursor", cursor);
  endpoint.search = search.toString();

  let answer: Answer<Page<JobRow>>;
  try {
    const upstream = await fetch(endpoint, { method: "GET", headers, cache: "no-store" });
    answer = await answered<Page<JobRow>>(upstream);
  } catch {
    answer = { state: "unavailable" };
  }

  if (answer.state === "signed-out") redirect("/sign-in");

  const rows = answer.state === "ok" && Array.isArray(answer.body.data) ? answer.body.data : [];
  const next = answer.state === "ok" ? answer.body.next_cursor : undefined;

  const first = href("/jobs", { q: term, status, customer });

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Jobs and bids</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Search every job by what is being sent, and narrow by status or by the customer who
          published it. Paste a job identifier and you are taken straight to it. Requires{" "}
          <code className="font-mono text-xs">jobs.read</code>, which every role holds.
        </p>
      </div>

      <form method="get" action="/jobs" className="flex flex-wrap items-end gap-3">
        <div className="flex min-w-64 flex-1 flex-col gap-2">
          <Label htmlFor="q">Goods description, or a job identifier</Label>
          <Input
            id="q"
            name="q"
            defaultValue={term}
            placeholder="two-seater sofa, or 0198f2c1-…"
            autoCapitalize="none"
            spellCheck={false}
          />
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="status">Status</Label>
          <select
            id="status"
            name="status"
            defaultValue={status}
            className="border-input bg-background text-foreground focus-visible:border-ring focus-visible:ring-ring/50 h-9 rounded-lg border px-3 text-sm shadow-xs outline-none focus-visible:ring-3"
          >
            <option value="">Any</option>
            {JOB_STATUSES.map((label) => (
              <option key={label} value={label}>
                {label}
              </option>
            ))}
          </select>
        </div>

        {/*
          The party filter travels as a hidden field so that typing a term does not silently drop
          it — a form submits what it holds, and a filter that survives one search and not the next
          is worse than one that does not exist. It is cleared by the link beside the table rather
          than by a control here, because it is not something anybody types: it arrives from the
          account search, where the identifier was already in somebody's hand.
        */}
        {customer !== "" && <input type="hidden" name="customer" value={customer} />}

        <Button type="submit">Search</Button>
      </form>

      {customer !== "" && (
        <div className="border-border flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-sm">
          <span className="text-muted-foreground">Published by</span>
          <span className="font-mono text-xs" title={customer}>
            {shortIdentifier(customer)}
          </span>
          <Link
            href={href("/jobs", { q: term, status })}
            className="text-foreground ml-auto text-xs underline underline-offset-4"
          >
            Show every customer
          </Link>
        </div>
      )}

      {answer.state === "refused" ? (
        <PlatformRefusal title="That search was refused" refusal={answer.refusal} />
      ) : answer.state === "unavailable" ? (
        <PlatformUnavailable title="The job search is not answering" />
      ) : (
        <>
          <PanelTable>
            <thead>
              <tr>
                <TH>Job</TH>
                <TH>Status</TH>
                <TH>Goods</TH>
                <TH className="text-right">Bids</TH>
                <TH>Customer</TH>
                <TH>Published</TH>
                <TH>Expires</TH>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <EmptyRow span={7}>
                  No job matched. Every job is an administrator&rsquo;s to open, so this is an
                  absence rather than a limit on what you may see.
                </EmptyRow>
              ) : (
                rows.map((job) => (
                  <tr key={job.id}>
                    <TD>
                      <span className="font-mono text-xs" title={job.id}>
                        {shortIdentifier(job.id)}
                      </span>
                    </TD>
                    <TD>
                      <Badge variant="secondary" className="whitespace-nowrap">
                        {job.status}
                      </Badge>
                    </TD>
                    <TD className="max-w-80">
                      {/*
                        Empty on a draft that never reached the details step. `NULL ILIKE …` is
                        NULL rather than false on the platform, so such a job never matches a term
                        — which is the right answer, since it has no description to match.
                      */}
                      {job.goods_description === "" ? (
                        <span className="text-muted-foreground/70 italic">no description</span>
                      ) : (
                        job.goods_description
                      )}
                    </TD>
                    <TD className="text-right tabular-nums">
                      {/*
                        Every bid in every status, which is the question a list answers: three
                        withdrawn offers and no offers at all are very different facts.
                      */}
                      {job.bid_count}
                    </TD>
                    <TD>
                      <Link
                        href={href("/jobs", { customer: job.customer_id })}
                        className="font-mono text-xs underline underline-offset-4"
                        title={job.customer_id}
                      >
                        {shortIdentifier(job.customer_id)}
                      </Link>
                    </TD>
                    <TD className="whitespace-nowrap">{instant(job.created_at)}</TD>
                    <TD className="whitespace-nowrap">{instant(job.expires_at)}</TD>
                  </tr>
                ))
              )}
            </tbody>
          </PanelTable>

          <Pager
            first={first}
            next={
              next === undefined ? undefined : href("/jobs", { q: term, status, customer, cursor: next })
            }
            showing={rows.length}
          />
        </>
      )}
    </div>
  );
}
