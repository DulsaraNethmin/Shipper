import { AuditTrail } from "@/components/audit-trail";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { one } from "@/lib/query";

/**
 * The whole trail, searchable (SHIP-188c), reading SHIP-165's `GET /v1/admin/audit`.
 *
 * # Why this screen exists rather than only the job's own trail
 *
 * `Docs/09`'s SHIP-188c row asks for two things. The first is the job-scoped view, which is beneath
 * every job's detail and is `Docs/01` §8's journey. The second is that "the entry SHIP-188d's
 * decision writes appears in that trail" — and it never could in the first, because
 * `verifications.go` records `verification.decided` against `AuditTargetUser`. A verification
 * decision is done to a **provider**, not to a job, so a job-scoped trail cannot contain one however
 * many jobs that provider has.
 *
 * This is also the `audit.read` section the navigation has listed since SHIP-22, and SHIP-165's own
 * *Done when* — "immutable history is searchable by actor, target, and date". It names no endpoint
 * of its own: the trail is one component reading one path, embedded here and in the job screen.
 *
 * # The action filter is a text box, and that is a decision rather than an omission
 *
 * The other filters take identifiers and dates. `action` takes one of eleven names that live in
 * `internal/admin/audit.go` and in no shared contract — `contracts/statuses.yaml` is scoped to
 * *status* enumerations and says so, and an audit action is not one. A `<select>` here would
 * therefore be a hand-written second copy of a vocabulary, which is the thing SHIP-56a exists to
 * prevent and which would fall silently behind the day somebody adds a twelfth.
 *
 * A text box costs one mistake and teaches the answer: the platform refuses an unrecognised action
 * with "That is not an action this platform records. Use one of …", listing every one of them, and
 * the refusal card renders that list. Nobody has to guess twice, and nothing here can drift.
 */
export default async function AuditPage(props: PageProps<"/audit">) {
  const params = await props.searchParams;

  const actor = one(params.actor);
  const target = one(params.target);
  const action = one(params.action);
  const from = one(params.from);
  const to = one(params.to);

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Audit trail</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Every privileged action, newest first, searchable by who did it, what it was done to, which
          action, and when. Append-only — there is no endpoint that edits or deletes an entry, no
          permission that would authorise one, and a database trigger that refuses both from any
          connection. Requires <code className="font-mono text-xs">audit.read</code>, which every
          role holds, including the least privileged: a trail only the people it records can read is
          not a control.
        </p>
      </div>

      <form method="get" action="/audit" className="flex flex-wrap items-end gap-3">
        <Field id="actor" label="Actor" value={actor} placeholder="account identifier" />
        <Field id="target" label="Target" value={target} placeholder="job or account identifier" />
        <Field id="action" label="Action" value={action} placeholder="verification.decided" />
        <Field id="from" label="From" value={from} placeholder="2026-08-01" />
        <Field id="to" label="To" value={to} placeholder="2026-09-01" />
        <Button type="submit">Search</Button>
      </form>

      <AuditTrail
        query={{
          basePath: "/audit",
          carried: { actor, target, action, from, to },
          actor,
          target,
          action,
          from,
          to,
          cursor: one(params.cursor),
          emptyMessage:
            "No entry matched. The trail records privileged actions only — ordinary marketplace " +
            "activity is in each job's own status history.",
        }}
      />
    </div>
  );
}

function Field({
  id,
  label,
  value,
  placeholder,
}: {
  id: string;
  label: string;
  value: string;
  placeholder: string;
}) {
  return (
    <div className="flex min-w-44 flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        name={id}
        defaultValue={value}
        placeholder={placeholder}
        autoCapitalize="none"
        spellCheck={false}
      />
    </div>
  );
}
