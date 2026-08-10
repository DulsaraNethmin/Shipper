import Link from "next/link";
import { Button } from "@/components/ui/button";

/**
 * The root of the portal, which in production almost nobody sees: a driver arrives on a
 * job link, not here.
 *
 * It exists so that someone who reaches the origin without a link is told what this is and
 * is not left at a 404 wondering whether the link was broken. It offers no way in — there
 * is nothing to sign in to, and the link is the only entry (SHIP-120).
 */
export default function LandingPage() {
  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Shipper delivery portal</h1>
        <p className="text-muted-foreground mt-2 text-sm">
          This page is opened from the link your transport provider sent you. It shows one
          delivery and nothing else.
        </p>
      </div>

      <p className="text-muted-foreground text-sm">
        There is no account to create and no app to install. If you were expecting a
        delivery here, open the link again from the message you were sent.
      </p>

      <Button asChild variant="outline" size="lg" className="h-12 justify-center">
        <Link href="/job">See the placeholder delivery page</Link>
      </Button>

      <p className="text-muted-foreground/80 text-xs">
        Scaffold only (SHIP-23). The link-authenticated delivery page is SHIP-120.
      </p>
    </main>
  );
}
