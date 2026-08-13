/**
 * The root of the portal, which in production almost nobody sees: a driver arrives on a
 * delivery link, not here.
 *
 * It exists so that someone who reaches the origin without a link is told what this is and is not
 * left at a 404 wondering whether the link was broken. **It offers no way in, and after SHIP-120
 * that is a stronger statement than it was at SHIP-23**: there is now a real route behind a real
 * credential, and this page still has no field to type one into, no sign-in, and no link to
 * anywhere. A driver has no account, so the only entry is the link, and the only place the link
 * lives is the message the transport provider sent.
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

      <p className="text-muted-foreground text-sm">
        Open it from the message rather than from your browser history — the link carries the only
        credential there is, and your history may not have kept it.
      </p>
    </main>
  );
}
