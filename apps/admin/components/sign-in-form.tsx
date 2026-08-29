"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { idempotencyKey } from "@/lib/keys";

/**
 * The sign-in screen's form (SHIP-188a).
 *
 * # What this component never touches
 *
 * The credential. It posts an email address and a password to this origin's own route handler and
 * reads a body that has had the token removed from it; the token is installed as an httpOnly cookie
 * by that handler and there is no line of JavaScript anywhere in this application that can read it.
 * `lib/session.ts` argues why an administrator session gets a different answer from the driver
 * portal's `sessionStorage`, and `lib/surface.test.ts` holds this file to it.
 *
 * # Why the panel reloads rather than routing on what it just received
 *
 * The sign-in response carries the administrator, and it would be quicker to render the shell from
 * it. `router.refresh()` instead, so the first authenticated screen is drawn from `GET
 * /v1/admin/me` resolved on the server against the cookie the browser now holds. That is one extra
 * round trip in exchange for the panel never rendering a session it has not proved it can use — if
 * the cookie failed to stick, for any of the reasons a browser rejects one, this fails at the door
 * instead of showing a shell whose every subsequent request is a 401.
 */
export function SignInForm() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;

    setSubmitting(true);
    setRefusal(null);

    try {
      const answer = await fetch("/api/admin/sessions", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",

          // One value per action, minted here because this is the only participant that knows
          // whether a submission is a new attempt or a retry of the last one. A fresh key per
          // submission is deliberate: see lib/keys.ts on why a held key would make a corrected
          // password fail as `idempotency_key_reused`.
          "Idempotency-Key": idempotencyKey(),
        },
        body: JSON.stringify({ email, password }),
      });

      if (answer.ok) {
        router.replace("/");
        router.refresh();
        return;
      }

      setRefusal(await told(answer));
    } catch {
      setRefusal("The panel could not reach the platform. Try again in a moment.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
      <div className="flex flex-col gap-2">
        <Label htmlFor="email">Email address</Label>
        <Input
          id="email"
          name="email"
          type="email"
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          required
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          disabled={submitting}
        />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          name="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          disabled={submitting}
        />
      </div>

      {refusal !== null && (
        <p
          role="alert"
          className="border-destructive/30 bg-destructive/10 text-destructive rounded-lg border px-3 py-2 text-sm"
        >
          {refusal}
        </p>
      )}

      <Button type="submit" size="lg" disabled={submitting}>
        {submitting ? "Signing in…" : "Sign in"}
      </Button>
    </form>
  );
}

/**
 * What the administrator is told when the platform says no.
 *
 * The platform's `message` is rendered rather than a local copy, because it is the component that
 * knows *which* no this is — and because the panel must not invent a distinction the platform
 * deliberately refuses to draw. `internal/admin/http.go` answers a wrong password and an unknown
 * address identically so that sign-in is not an oracle for which addresses are administrators, and
 * a form that helpfully split them apart would hand back exactly what that answer withholds.
 */
async function told(answer: Response): Promise<string> {
  const after = answer.headers.get("Retry-After");

  let body: { error?: { message?: unknown } } | null = null;
  try {
    body = (await answer.json()) as { error?: { message?: unknown } };
  } catch {
    body = null;
  }

  const message =
    typeof body?.error?.message === "string" && body.error.message !== ""
      ? body.error.message
      : "That sign-in could not be completed.";

  // The platform sets Retry-After when it throttles, and the route handler forwards it. Somebody
  // told to wait without being told how long either gives up or hammers the endpoint.
  if (answer.status === 429 && after !== null) {
    return `${message} Try again in ${after} seconds.`;
  }
  return message;
}
