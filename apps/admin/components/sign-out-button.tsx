"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/button";
import { idempotencyKey } from "@/lib/keys";

/**
 * Ending the session, from the account strip (SHIP-188a).
 *
 * It calls this origin's own route handler, which forwards to `DELETE
 * /v1/admin/sessions/current` — so the session is ended **at the platform**, not merely forgotten
 * here. That distinction is the whole of the ticket's third criterion: replaying the same cookie
 * afterwards answers 401 because the row is gone, and `internal/admin/adminauth.go` reads that row
 * on every request precisely so there is no window in which it does not.
 *
 * A failure leaves the administrator signed in and says so, because the route handler keeps the
 * cookie when the platform did not confirm the session ended. Its file note argues why that is the
 * cautious answer rather than the careless one.
 */
export function SignOutButton() {
  const router = useRouter();
  const [leaving, setLeaving] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);

  async function signOut() {
    if (leaving) return;

    setLeaving(true);
    setRefusal(null);

    try {
      const answer = await fetch("/api/admin/sessions/current", {
        method: "DELETE",
        headers: { Accept: "application/json", "Idempotency-Key": idempotencyKey() },
      });

      if (answer.status === 204) {
        router.replace("/sign-in");
        router.refresh();
        return;
      }

      setRefusal("Signing out failed. You are still signed in — try again.");
    } catch {
      setRefusal("The panel could not reach the platform. You are still signed in.");
    } finally {
      setLeaving(false);
    }
  }

  return (
    <div className="flex items-center gap-2">
      {refusal !== null && (
        <span role="alert" className="text-destructive text-xs">
          {refusal}
        </span>
      )}
      <Button variant="outline" size="sm" onClick={signOut} disabled={leaving}>
        {leaving ? "Signing out…" : "Sign out"}
      </Button>
    </div>
  );
}
