import type { Metadata } from "next";
import { AdminShell } from "@/components/admin-shell";
import "./globals.css";

export const metadata: Metadata = {
  title: "Shipper Admin",
  description: "Support and moderation for the Shipper marketplace.",
};

/**
 * The shell wraps every route rather than each page drawing its own, which is what makes
 * it a shell: the navigation and the account strip are in one place, and a screen added at
 * M6 inherits them without deciding anything.
 *
 * No web font is loaded. `next/font/google` fetches at build time, so a CI runner without
 * outbound network — or an air-gapped build — fails on a typeface rather than on the code.
 * The stack in globals.css is the operating system's, which renders the same everywhere it
 * matters and costs nothing to fetch.
 */
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en-AU" className="h-full antialiased">
      <body className="bg-background text-foreground min-h-full">
        <AdminShell>{children}</AdminShell>
      </body>
    </html>
  );
}
