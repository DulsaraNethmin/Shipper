import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Shipper Admin",
  description: "Support and moderation for the Shipper marketplace.",
};

/**
 * The document, and nothing else (SHIP-22, narrowed by SHIP-188a).
 *
 * **The shell used to be here and has moved to `app/(panel)/layout.tsx`.** It had to: a layout that
 * wraps every route wraps the sign-in screen too, and the shell now renders an administrator's name,
 * role and permissions — none of which exists before somebody signs in. The route group is what
 * separates "inside the panel" from "at its door" without adding a segment to any URL.
 *
 * No web font is loaded. `next/font/google` fetches at build time, so a CI runner without outbound
 * network — or an air-gapped build — fails on a typeface rather than on the code. The stack in
 * globals.css is the operating system's, which renders the same everywhere it matters and costs
 * nothing to fetch.
 */
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en-AU" className="h-full antialiased">
      <body className="bg-background text-foreground min-h-full">{children}</body>
    </html>
  );
}
