import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Shipper — Delivery",
  description: "The job a driver was sent a link to.",
};

/**
 * There is no application chrome here, and that is the design rather than an omission.
 *
 * A driver reaches this portal from a link, holds exactly one job, and has no account to
 * navigate to (Docs/06 §2, Docs/07 §1). Navigation would imply somewhere else to go, and
 * the only honest answer to "where else" is nowhere: the job-scoped token grants access to
 * one job and nothing else, and that is enforced on the platform (SHIP-108).
 *
 * No web font is loaded. `next/font/google` fetches at build time, and this is the surface
 * most likely to be opened on a phone with two bars of signal — the system stack costs
 * nothing to fetch and renders immediately.
 */
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en-AU" className="h-full antialiased">
      <body className="bg-background text-foreground min-h-full">{children}</body>
    </html>
  );
}
