import Link from "next/link";
import { redirect } from "next/navigation";

import { Pager } from "@/components/pager";
import { EmptyRow, PanelTable, TD, TH } from "@/components/panel-table";
import { PlatformRefusal, PlatformUnavailable } from "@/components/platform-refusal";
import { Badge } from "@/components/ui/badge";
import { platformHeaders } from "@/lib/credential";
import { instant, shortIdentifier } from "@/lib/format";
import { href, one } from "@/lib/query";
import { answered, type Answer, type Page, platform } from "@/lib/upstream";

/**
 * The verification queue (SHIP-188d), served by `GET /v1/admin/verifications` (SHIP-153).
 *
 * # Oldest first, and the platform decides that rather than this screen
 *
 * A queue and a search want opposite orderings. The account and job searches are newest-first
 * because what somebody is looking for is overwhelmingly recent; this has somebody waiting at the
 * front of it. An unreviewed provider cannot bid at all (`Docs/04` §1), so the oldest entry is the
 * provider who has been unable to trade for longest.
 *
 * # `state` is sent explicitly, always, and never defaulted here
 *
 * `Docs/09`'s SHIP-188d row asks for exactly that, and the platform enforces it: `verificationQueryFrom`
 * refuses a request with no `state` rather than assuming `Pending`. The reason is worth keeping in
 * view from this side — `Docs/04` §5's first queue is "new or **changed**" submissions, four of the
 * five outcomes are worth listing, and a default would make an absent parameter mean two different
 * things depending on whether the console remembered to send it.
 *
 * So this screen has a state in its URL at all times. Landing on `/verifications` with none is a
 * redirect to `?state=Pending` rather than a request with the parameter left off — the reviewer's
 * ordinary starting point, chosen here and visible in the address bar, rather than a default the
 * platform applied silently.
 */

/** One provider waiting, exactly as `verificationEntryResponse` serves them. */
interface QueueRow {
  provider_id: string;
  name: string;
  email: string;
  phone: string;
  state: string;
  submitted_at: string;
}

/**
 * `Docs/04` §4's five outcomes, from `internal/profiles`' `States`.
 *
 * Written out rather than generated, on `contracts/statuses.yaml`'s own test: an enumeration belongs
 * there when more than one language has to know it. The mobile client reads a provider's *own* state
 * from `GET /v1/provider/verification` and shows it; nothing else outside Go needs the list. A
 * generated module for one consumer would be the larger guess — and the platform refuses an
 * unrecognised value with the five named, so a list that fell behind fails loudly on the first
 * search rather than quietly returning the wrong rows.
 */
const STATES = ["Pending", "Verified", "Restricted", "Rejected", "Suspended"] as const;

export default async function VerificationsPage(props: PageProps<"/verifications">) {
  const params = await props.searchParams;
  const state = one(params.state);
  const cursor = one(params.cursor);

  // Chosen here and put in the URL, rather than left off and defaulted by nobody. See the file note.
  if (state === "") redirect("/verifications?state=Pending");

  const headers = await platformHeaders();
  if (headers === null) redirect("/sign-in");

  const endpoint = new URL(`${platform()}/v1/admin/verifications`);
  const search = new URLSearchParams({ state });
  if (cursor !== "") search.set("cursor", cursor);
  endpoint.search = search.toString();

  let answer: Answer<Page<QueueRow>>;
  try {
    const upstream = await fetch(endpoint, { method: "GET", headers, cache: "no-store" });
    answer = await answered<Page<QueueRow>>(upstream);
  } catch {
    answer = { state: "unavailable" };
  }

  if (answer.state === "signed-out") redirect("/sign-in");

  const rows = answer.state === "ok" && Array.isArray(answer.body.data) ? answer.body.data : [];
  const next = answer.state === "ok" ? answer.body.next_cursor : undefined;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Verification queue</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Providers awaiting review, longest wait first — an unreviewed provider cannot bid at all,
          so the top of this list is whoever has been unable to trade for longest. Reading the queue
          is <code className="font-mono text-xs">verifications.read</code>, which every role holds;
          deciding is <code className="font-mono text-xs">verifications.decide</code>, which support
          does not.
        </p>
      </div>

      {/*
        Tabs rather than a select-and-submit, because the state is never absent and there are five
        of them: one click per state, each a plain link carrying its own URL. Paging resets with the
        state, which is right — a cursor belongs to the result set it was issued for.
      */}
      <nav aria-label="Verification state" className="flex flex-wrap gap-2">
        {STATES.map((option) => (
          <Link
            key={option}
            href={href("/verifications", { state: option })}
            aria-current={option === state ? "page" : undefined}
            className={
              "rounded-md px-3 py-1.5 text-sm " +
              (option === state
                ? "bg-accent text-accent-foreground font-medium"
                : "text-muted-foreground hover:bg-accent/50 hover:text-foreground")
            }
          >
            {option}
          </Link>
        ))}
      </nav>

      {answer.state === "refused" ? (
        <PlatformRefusal title="That queue was refused" refusal={answer.refusal} />
      ) : answer.state === "unavailable" ? (
        <PlatformUnavailable title="The verification queue is not answering" />
      ) : (
        <>
          <PanelTable>
            <thead>
              <tr>
                <TH>Provider</TH>
                <TH>Email</TH>
                <TH>Phone</TH>
                <TH>State</TH>
                <TH>Waiting since</TH>
                <TH>Review</TH>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <EmptyRow span={6}>
                  No provider is {state.toLowerCase()}. The other four states are a click away
                  above.
                </EmptyRow>
              ) : (
                rows.map((entry) => (
                  <tr key={entry.provider_id}>
                    <TD>
                      {entry.name === "" ? (
                        <span className="text-muted-foreground/70 italic">no name recorded</span>
                      ) : (
                        <span className="text-foreground font-medium">{entry.name}</span>
                      )}
                      <span
                        className="text-muted-foreground block font-mono text-xs"
                        title={entry.provider_id}
                      >
                        {shortIdentifier(entry.provider_id)}
                      </span>
                    </TD>
                    <TD>{entry.email}</TD>
                    <TD>{entry.phone}</TD>
                    <TD>
                      <Badge variant={entry.state === "Verified" ? "default" : "secondary"}>
                        {entry.state}
                      </Badge>
                    </TD>
                    <TD className="whitespace-nowrap">{instant(entry.submitted_at)}</TD>
                    <TD>
                      <Link
                        href={`/verifications/${entry.provider_id}`}
                        className="text-foreground text-xs underline underline-offset-4"
                      >
                        Open evidence
                      </Link>
                    </TD>
                  </tr>
                ))
              )}
            </tbody>
          </PanelTable>

          <Pager
            first={href("/verifications", { state })}
            next={next === undefined ? undefined : href("/verifications", { state, cursor: next })}
            showing={rows.length}
          />
        </>
      )}
    </div>
  );
}
