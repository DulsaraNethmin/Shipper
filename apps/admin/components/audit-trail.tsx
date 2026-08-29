import { Pager } from "@/components/pager";
import { EmptyRow, PanelTable, TD, TH } from "@/components/panel-table";
import { PlatformRefusal, PlatformUnavailable } from "@/components/platform-refusal";
import { platformHeaders } from "@/lib/credential";
import { instant, shortIdentifier } from "@/lib/format";
import { href } from "@/lib/query";
import { answered, type Answer, type Page, platform } from "@/lib/upstream";

/**
 * The append-only trail (SHIP-188c), served by `GET /v1/admin/audit` (SHIP-165, SHIP-150).
 *
 * # A component rather than a screen, because it is read in two places
 *
 * `Docs/09`'s SHIP-188c row asks for two things of one trail. The first is `Docs/01` §8's journey —
 * "an administrator finds the job and reads its audit trail" — which is this filtered to one job and
 * rendered beneath its detail. The second is "the entry SHIP-188d's decision writes appears in that
 * trail", and **that entry is not a job's**: `verifications.go` records `verification.decided`
 * against `AuditTargetUser`, so no job-scoped view could ever contain it. The trail therefore needs a
 * screen of its own as well, which is SHIP-165's viewer and the `audit.read` section the panel's
 * navigation has listed since SHIP-22.
 *
 * One component, one `fetch`, one upstream path — so the panel's rule holds while two screens read
 * it. A second copy of this in the job page would be a second file naming `/v1/admin/audit`, which
 * `lib/surface.test.ts` refuses.
 *
 * # Nothing is hidden, and that is the point of the table
 *
 * `auditEntryResponse` carries every column, and this renders every column — including the metadata,
 * as the bytes the platform wrote. `Docs/04` §9 makes the trail what holds administrators to
 * account, and a viewer that showed a tidied version of the record would be a second record: the one
 * somebody checks would be the wrong one. Nothing commercial is ever written into an entry, so
 * showing all of it discloses nothing that was not already recorded on purpose.
 *
 * There is no write, no edit and no delete here, and there is nowhere for one to be added: the
 * endpoint has no such method, `permissions.go` has no permission that would authorise it, and
 * `000003`'s triggers refuse both from any connection.
 */

/** One entry, exactly as `auditEntryResponse` serves it. */
interface AuditRow {
  id: string;
  actor_type: string;
  actor_id: string;
  action: string;
  target_type: string;
  target_id: string;
  reason: string;
  metadata: unknown;
  created_at: string;
}

/** Which slice of the trail to read, and where the paging links point. */
export interface TrailQuery {
  /** The screen these links belong to — `/audit`, or a job's own path. */
  basePath: string;

  /** The other parameters that screen is carrying, so a paging link keeps them. */
  carried?: Record<string, string | undefined>;

  actor?: string;
  target?: string;
  action?: string;
  from?: string;
  to?: string;
  cursor?: string;

  /** What the empty state says. It differs between a job's trail and the whole trail. */
  emptyMessage: string;
}

export async function AuditTrail({ query }: { query: TrailQuery }) {
  const headers = await platformHeaders();

  // The layout resolved a session before this rendered, so a missing credential here means it died
  // in between. The screens redirect on that; a component embedded in one of them says so and lets
  // the rest of the page stand, because a job's detail is still worth reading.
  if (headers === null) {
    return <PlatformUnavailable title="The audit trail could not be read" />;
  }

  const endpoint = new URL(`${platform()}/v1/admin/audit`);
  const search = new URLSearchParams();
  for (const [key, value] of [
    ["actor", query.actor],
    ["target", query.target],
    ["action", query.action],
    ["from", query.from],
    ["to", query.to],
    ["cursor", query.cursor],
  ] as const) {
    if (value !== undefined && value !== "") search.set(key, value);
  }
  endpoint.search = search.toString();

  let answer: Answer<Page<AuditRow>>;
  try {
    const upstream = await fetch(endpoint, { method: "GET", headers, cache: "no-store" });
    answer = await answered<Page<AuditRow>>(upstream);
  } catch {
    answer = { state: "unavailable" };
  }

  if (answer.state === "refused") {
    return <PlatformRefusal title="The audit trail was not shown" refusal={answer.refusal} />;
  }
  if (answer.state !== "ok") {
    return <PlatformUnavailable title="The audit trail is not answering" />;
  }

  const rows = Array.isArray(answer.body.data) ? answer.body.data : [];
  const next = answer.body.next_cursor;
  const carried = query.carried ?? {};

  return (
    <div className="flex flex-col gap-3">
      <PanelTable>
        <thead>
          <tr>
            <TH>When</TH>
            <TH>Actor</TH>
            <TH>Action</TH>
            <TH>Target</TH>
            <TH>Reason</TH>
            <TH>Recorded</TH>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <EmptyRow span={6}>{query.emptyMessage}</EmptyRow>
          ) : (
            rows.map((entry) => (
              <tr key={entry.id}>
                <TD className="whitespace-nowrap">{instant(entry.created_at)}</TD>
                <TD>
                  <span className="block">{entry.actor_type}</span>
                  {/*
                    Empty for `system`, which has no account — and ck_audit_log_actor_id requires
                    exactly that, so the absence is a fact rather than a missing value.
                  */}
                  {entry.actor_id !== "" && (
                    <span
                      className="text-muted-foreground block font-mono text-xs"
                      title={entry.actor_id}
                    >
                      {shortIdentifier(entry.actor_id)}
                    </span>
                  )}
                </TD>
                <TD className="font-mono text-xs whitespace-nowrap">{entry.action}</TD>
                <TD>
                  <span className="block">{entry.target_type}</span>
                  <span
                    className="text-muted-foreground block font-mono text-xs"
                    title={entry.target_id}
                  >
                    {shortIdentifier(entry.target_id)}
                  </span>
                </TD>
                <TD className="max-w-72">
                  {entry.reason === "" ? (
                    <span className="text-muted-foreground/70 italic">none recorded</span>
                  ) : (
                    entry.reason
                  )}
                </TD>
                <TD>
                  {/*
                    The metadata as the platform wrote it. It is passed through the endpoint as raw
                    JSON rather than decoded and re-encoded, so that what a reader sees is byte for
                    byte the record; re-serialising it here would give that back, so it is stringified
                    once and shown.
                  */}
                  <pre className="text-muted-foreground max-w-72 overflow-x-auto text-xs whitespace-pre-wrap">
                    {JSON.stringify(entry.metadata)}
                  </pre>
                </TD>
              </tr>
            ))
          )}
        </tbody>
      </PanelTable>

      <Pager
        first={href(query.basePath, carried)}
        next={next === undefined ? undefined : href(query.basePath, { ...carried, cursor: next })}
        showing={rows.length}
      />
    </div>
  );
}
