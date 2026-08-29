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
import { answered, type Answer, type Page, platform } from "@/lib/upstream";

/**
 * The account search (SHIP-188b), served by `GET /v1/admin/users` (SHIP-151, SHIP-30a).
 *
 * # All four of the *Done when*'s terms, and where each is served
 *
 * "finds an account by email, phone, name or status". Three of them are one box: the platform
 * matches `q` against the address, the name and the phone number in a single predicate, because
 * somebody searching for `alice` means all three equally. The fourth is a separate control, because
 * a standing is a closed list rather than something to type — and `internal/admin/http.go` refuses an
 * unrecognised one rather than ignoring it, so a `<select>` here is not decoration: a free-text box
 * would let somebody type `suspeneded` and read the platform's refusal instead of a result.
 *
 * # Rendered on the server, and that is what makes the paging honest
 *
 * The term, the standing and the cursor are query parameters, so this screen holds no result set
 * between renders and there is nowhere for a client-side slice to live — which is the *Done when*'s
 * "page through the platform's own cursor rather than a client-side slice" as a property of the
 * design rather than a claim about it. `lib/query.ts` has the rest of the argument.
 *
 * # One endpoint, named once, as a literal
 *
 * The path below is a template with exactly one hole and that hole is where the platform is; the
 * query string is set on a `URL` afterwards, so nothing a person types can reach the path at all.
 * `lib/surface.test.ts` records which file may name which endpoint and fails if this one names a
 * second — the property `app/api/admin/sessions/route.ts` argues at length, and the reason there is
 * no shared `request(path, …)` helper anywhere in this application.
 */

/** One account, exactly as `userResponse` serves it. Nothing commercial: there is no budget here. */
interface AccountRow {
  id: string;
  name: string;
  email: string;
  phone: string;
  role: string;
  status: string;
  email_verified_at: string;
  phone_verified_at: string;
  created_at: string;
}

/**
 * The account standings, from `internal/admin/users.go`'s `UserStandings`.
 *
 * Written out rather than generated. `contracts/statuses.yaml`'s own header draws the line — an
 * enumeration belongs there "when more than one language has to know it" — and this one is read by
 * the panel and by nothing else: the mobile client never sees another account's standing, and the
 * driver portal has no account at all. A generated module for a single consumer would be the larger
 * guess, and the platform refuses an unrecognised value, so a list that fell behind fails loudly on
 * the first search rather than quietly returning everything.
 */
const STANDINGS = ["active", "restricted", "suspended"] as const;

export default async function UsersPage(props: PageProps<"/users">) {
  const params = await props.searchParams;
  const term = one(params.q);
  const status = one(params.status);
  const cursor = one(params.cursor);

  const headers = await platformHeaders();
  if (headers === null) redirect("/sign-in");

  const endpoint = new URL(`${platform()}/v1/admin/users`);
  const search = new URLSearchParams();
  if (term !== "") search.set("q", term);
  if (status !== "") search.set("status", status);
  if (cursor !== "") search.set("cursor", cursor);
  endpoint.search = search.toString();

  let answer: Answer<Page<AccountRow>>;
  try {
    const upstream = await fetch(endpoint, { method: "GET", headers, cache: "no-store" });
    answer = await answered<Page<AccountRow>>(upstream);
  } catch {
    answer = { state: "unavailable" };
  }

  // The session died between the layout's `GET /v1/admin/me` and this call. Rare, and the correct
  // answer is the form rather than an error card.
  if (answer.state === "signed-out") redirect("/sign-in");

  // Read out here rather than in the markup, so the table renders one shape whatever arrived. The
  // platform never sends a null collection — `pagination.NewPage` is built on a `make(…, 0, …)`
  // precisely so a console that iterates without checking cannot crash — but `answered` casts what
  // it is given, and a body that is not the contract should show an empty table rather than throw
  // the whole screen away.
  const rows = answer.state === "ok" && Array.isArray(answer.body.data) ? answer.body.data : [];
  const next = answer.state === "ok" ? answer.body.next_cursor : undefined;

  const first = href("/users", { q: term, status });

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Users</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Search every customer and provider account by email address, name or phone number, and
          narrow by standing. Requires <code className="font-mono text-xs">users.read</code>, which
          every role holds.
        </p>
      </div>

      {/*
        A plain GET form. It submits by navigating, so a search is a URL somebody can paste into a
        ticket, the back button steps through the pages that were actually read, and the screen
        works with no JavaScript at all — which for a support surface reached from whatever was to
        hand is worth more than an input that filters as you type.

        There is no cursor field, so submitting a new search always starts at the first page. That
        is not a detail: a cursor carried across a changed term is a cursor into a different result
        set, and the rows it lands on would be arbitrary.
      */}
      <form method="get" action="/users" className="flex flex-wrap items-end gap-3">
        <div className="flex min-w-64 flex-1 flex-col gap-2">
          <Label htmlFor="q">Email, name or phone</Label>
          <Input
            id="q"
            name="q"
            defaultValue={term}
            placeholder="alice@example.com"
            autoCapitalize="none"
            spellCheck={false}
          />
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="status">Standing</Label>
          <select
            id="status"
            name="status"
            defaultValue={status}
            className="border-input bg-background text-foreground focus-visible:border-ring focus-visible:ring-ring/50 h-9 rounded-lg border px-3 text-sm shadow-xs outline-none focus-visible:ring-3"
          >
            <option value="">Any</option>
            {STANDINGS.map((standing) => (
              <option key={standing} value={standing}>
                {standing}
              </option>
            ))}
          </select>
        </div>

        <Button type="submit">Search</Button>
      </form>

      {answer.state === "refused" ? (
        <PlatformRefusal title="That search was refused" refusal={answer.refusal} />
      ) : answer.state === "unavailable" ? (
        <PlatformUnavailable title="The account search is not answering" />
      ) : (
        <>
          <PanelTable>
            <thead>
              <tr>
                <TH>Name</TH>
                <TH>Email</TH>
                <TH>Phone</TH>
                <TH>Role</TH>
                <TH>Standing</TH>
                <TH>Registered</TH>
                <TH>Jobs</TH>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <EmptyRow span={7}>
                  {/*
                    An identifier gets its own sentence, because the empty table would otherwise be
                    a lie by omission. `GET /v1/admin/users` matches an address, a name or a phone
                    number and never an identifier, and there is no `GET /v1/admin/users/{id}` among
                    the twenty-four routes — so an account identifier finds nothing here, and the
                    honest thing is to say that rather than let somebody read it as "no such
                    account". The job search can send an identifier to the job it names; this one
                    has nowhere to send it. Docs/11 §4 carries the gap.
                  */}
                  {isIdentifier(term)
                    ? "That is an account identifier, and this search does not match one — it " +
                      "matches an email address, a name or a phone number. The platform serves no " +
                      "endpoint that opens an account by its identifier, so this is a gap in the " +
                      "product rather than an account that does not exist."
                    : "No account matched. Every account is searchable here, so this is an " +
                      "absence rather than a limit on what you may see."}
                </EmptyRow>
              ) : (
                rows.map((account) => (
                  <tr key={account.id}>
                    <TD>
                      <span className="text-foreground font-medium">
                        {/*
                          Empty for an account registered before `000006` added the column. A
                          name cannot be backfilled, so the absence is shown rather than filled
                          with a placeholder that would read as a name somebody chose.
                        */}
                        {account.name === "" ? (
                          <span className="text-muted-foreground/70 italic">
                            no name recorded
                          </span>
                        ) : (
                          account.name
                        )}
                      </span>
                      <span
                        className="text-muted-foreground block font-mono text-xs"
                        title={account.id}
                      >
                        {shortIdentifier(account.id)}
                      </span>
                    </TD>
                    <TD>
                      <span className="block">{account.email}</span>
                      <Verified at={account.email_verified_at} />
                    </TD>
                    <TD>
                      <span className="block">{account.phone}</span>
                      <Verified at={account.phone_verified_at} />
                    </TD>
                    <TD>{account.role}</TD>
                    <TD>
                      <Badge variant={account.status === "active" ? "secondary" : "destructive"}>
                        {account.status}
                      </Badge>
                    </TD>
                    <TD className="whitespace-nowrap">{instant(account.created_at)}</TD>
                    <TD>
                      {/*
                        The *Done when*'s "a job by … party". `GET /v1/admin/jobs` narrows to one
                        account with `customer`, which takes an identifier — and this is where an
                        operator has one in their hand. A provider has no jobs of their own under
                        that filter, which is why the link says whose jobs it is about.
                      */}
                      <Link
                        href={href("/jobs", { customer: account.id })}
                        className="text-foreground text-xs underline underline-offset-4"
                      >
                        As customer
                      </Link>
                    </TD>
                  </tr>
                ))
              )}
            </tbody>
          </PanelTable>

          <Pager
            first={first}
            next={next === undefined ? undefined : href("/users", { q: term, status, cursor: next })}
            showing={rows.length}
          />
        </>
      )}
    </div>
  );
}

/**
 * Whether a channel has been confirmed.
 *
 * `000002` records why these are instants rather than booleans, and the panel shows the instant
 * because *when* somebody confirmed an address is the fact a support call turns on — "they verified
 * last March" and "they verified four minutes ago" are very different conversations.
 */
function Verified({ at }: { at: string }) {
  return at === "" ? (
    <span className="text-muted-foreground/70 block text-xs">unverified</span>
  ) : (
    <span className="text-muted-foreground block text-xs">verified {instant(at)}</span>
  );
}
