import Link from "next/link";
import { notFound, redirect } from "next/navigation";

import { AuditTrail } from "@/components/audit-trail";
import { PlatformRefusal, PlatformUnavailable } from "@/components/platform-refusal";
import { VerificationDecision } from "@/components/verification-decision";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { viewer } from "@/lib/administrator";
import { platformHeaders } from "@/lib/credential";
import { instant } from "@/lib/format";
import { isIdentifier, one } from "@/lib/query";
import { answered, type Answer, type Page, platform } from "@/lib/upstream";

/**
 * `Docs/04` §3's evidence, and the decision taken on it (SHIP-188d).
 *
 * Served by `GET /v1/admin/verifications/{id}/documents` (SHIP-155). The `{id}` is the **provider's
 * account identifier**, not a document's and not a record's: `provider_verifications.provider_id` is
 * the primary key, so a provider's file is addressed by the provider, and the queue's `provider_id`
 * is the value that goes here.
 *
 * # The images are opened through the platform's own short-lived URLs, and nothing here keeps one
 *
 * Each document arrives with a freshly signed `download_url` and the instant it stops working. They
 * go straight into an `<img>` and into nothing else: there is no browser storage anywhere in this
 * panel — `lib/surface.test.ts` bans all three kinds outright — the page is rendered `no-store`, and
 * a reload signs new ones. **There is no object key to keep either**, and that is the platform's
 * decision rather than this screen's: `evidenceDocumentResponse` deliberately carries none, because
 * a key is a durable handle into the bucket holding identity documents and sending one beside a
 * temporary credential would hand out the thing the credential exists to make temporary.
 *
 * Every load of this screen writes an access entry naming the administrator who made it. That is the
 * compensating control for `verifications.read` being the broad permission it is — `Docs/04` §9's
 * question is answerable afterwards as "who looked" rather than only as "who could have looked".
 *
 * # What this screen cannot show, and why it does not guess
 *
 * **The provider's current state.** There is no `GET /v1/admin/verifications/{id}` among the
 * twenty-four routes — the queue lists a state and the evidence endpoint serves documents, and
 * neither answers "where does this one provider stand" for a provider you already have. Carrying the
 * state from the queue in a query parameter was the obvious fix and is refused: it would render a
 * value the browser supplied as though the platform had said it, and it would be wrong for exactly
 * the reviewer who left the queue open for ten minutes.
 *
 * What is shown instead is real and comes from an endpoint: the decision history out of the audit
 * trail, whose newest `verification.decided` entry carries `{"from": …, "to": …}` as the platform
 * wrote it. A provider with no entry has never been decided, which is `Pending`. And the decision
 * itself answers with `from` and `to` — read under the row's lock, so it is the only account of the
 * previous state that is true at the instant it is taken. `Docs/11` §4 carries the gap.
 */

/** One document, exactly as `evidenceDocumentResponse` serves it. */
interface EvidenceDocument {
  id: string;
  kind: string;
  content_type: string;
  content_length: number;
  etag: string;
  submitted_at: string;
  download_url: string;
  download_expires_at: string;
}

export default async function VerificationPage(props: PageProps<"/verifications/[id]">) {
  const { id } = await props.params;
  const params = await props.searchParams;

  // Before anything reaches a path — the same check the job detail screen makes, for the same
  // reason: this is one of two places where something other than platform() is interpolated into a
  // /v1/ template.
  if (!isIdentifier(id)) notFound();

  const headers = await platformHeaders();
  if (headers === null) redirect("/sign-in");

  let answer: Answer<Page<EvidenceDocument>>;
  try {
    const upstream = await fetch(`${platform()}/v1/admin/verifications/${id}/documents`, {
      method: "GET",
      headers,
      cache: "no-store",
    });
    answer = await answered<Page<EvidenceDocument>>(upstream);
  } catch {
    answer = { state: "unavailable" };
  }

  if (answer.state === "signed-out") redirect("/sign-in");

  const documents = answer.state === "ok" && Array.isArray(answer.body.data) ? answer.body.data : [];

  // Read to disable, never to decide (`Docs/07` §3). A support administrator may open this screen —
  // `verifications.read` is theirs and `Docs/04` §3 defines the review as looking at the images —
  // and may not record an outcome. Without this they compose a reason, click, and are refused; the
  // navigation already disables what a role cannot reach, and the same rule belongs on the control
  // that would be refused. The platform decides either way, on every request.
  //
  // It costs a second `GET /v1/admin/me` on this screen alone. That is the design rather than an
  // oversight: `lib/administrator.ts` resolves the viewer on every render precisely so a revocation
  // takes effect at once, and a cached copy here would give that back to save one indexed read.
  const who = await viewer();
  const mayDecide =
    who.state === "signed-in" && who.administrator.permissions.includes("verifications.decide");

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-3">
        <Link
          href="/verifications?state=Pending"
          className="text-muted-foreground text-sm underline underline-offset-4"
        >
          ← Back to the verification queue
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">Provider verification</h1>
        <p className="text-muted-foreground font-mono text-xs">{id}</p>
      </div>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Evidence</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Opened through short-lived signed URLs the platform issues per request. The panel keeps
            neither a URL nor an object key — the platform sends no key at all, and a reload signs
            new URLs. This read is recorded against your account.
          </p>
        </div>

        {answer.state === "refused" ? (
          <PlatformRefusal title="The evidence was not shown" refusal={answer.refusal} />
        ) : answer.state !== "ok" ? (
          <PlatformUnavailable title="The evidence is not answering" />
        ) : documents.length === 0 ? (
          <Card>
            <CardHeader>
              <CardTitle>No document has been submitted</CardTitle>
              <CardDescription>
                This provider has uploaded nothing to review. A decision can still be recorded — a
                rejection for want of evidence is a decision — and the reason is what says so.
              </CardDescription>
            </CardHeader>
          </Card>
        ) : (
          <ul className="grid gap-4 md:grid-cols-2">
            {documents.map((document) => (
              <li key={document.id}>
                <Card>
                  <CardHeader>
                    <CardTitle>{document.kind}</CardTitle>
                    <CardDescription>
                      Submitted {instant(document.submitted_at)} · {document.content_type} ·{" "}
                      {kilobytes(document.content_length)}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {/*
                      The signed URL, used and not stored. `unoptimized` in spirit: this is a plain
                      <img> rather than next/image because the optimiser would fetch and cache the
                      bytes on the panel's own filesystem — which is exactly the durable copy of an
                      identity document that private storage and a short-lived URL exist to prevent.
                      eslint's no-img-element rule is about layout shift and bandwidth, and neither
                      outweighs that here.
                    */}
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img
                      src={document.download_url}
                      alt={`${document.kind} submitted by this provider`}
                      className="border-border max-h-96 w-full rounded-lg border object-contain"
                    />
                    <dl className="text-muted-foreground space-y-1 text-xs">
                      <div className="flex gap-2">
                        <dt className="font-medium">Link expires</dt>
                        <dd>{instant(document.download_expires_at)}</dd>
                      </div>
                      <div className="flex gap-2">
                        <dt className="font-medium">Entity tag</dt>
                        {/*
                          So a reviewer can tell they are looking at the recorded bytes: a mismatch
                          with the store's own tag means the object was overwritten inside a
                          pre-signed PUT's window.
                        */}
                        <dd className="font-mono">{document.etag}</dd>
                      </div>
                    </dl>
                  </CardContent>
                </Card>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Decision</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Recorded with your account, the time and your reason, in the provider&rsquo;s evidence
            trail and in the append-only audit log. Both are append-only, so a decision cannot be
            tidied up afterwards.
          </p>
        </div>
        <VerificationDecision providerId={id} mayDecide={mayDecide} />
      </section>

      <section className="flex flex-col gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Decision history</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Every privileged action recorded against this provider, newest first. The newest{" "}
            <code className="font-mono text-xs">verification.decided</code> entry carries the move
            the platform wrote — no entry at all means this provider has never been decided, which is
            Pending.
          </p>
        </div>

        <AuditTrail
          query={{
            basePath: `/verifications/${id}`,
            target: id,
            cursor: one(params.cursor),
            emptyMessage:
              "Nothing has been recorded against this provider. They have never been decided, " +
              "which is Pending.",
          }}
        />
      </section>
    </div>
  );
}

/** A size a person reads, rather than a count of bytes. */
function kilobytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "size unknown";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} kB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
