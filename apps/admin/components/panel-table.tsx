import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

/**
 * The panel's table, as a handful of styled elements rather than a component with a column model
 * (SHIP-188b).
 *
 * # Why not a generic `<DataTable columns={…} rows={…} />`
 *
 * Because the columns are the interesting part. `Docs/01` §4.3 makes a customer's budget invisible
 * to a provider, and `Docs/01` §5.1 asks the platform to minimise how far an address or a contact
 * detail travels — so *which fields a screen renders* is a decision each screen takes against those
 * sections, and the Go side already holds its two response shapes to a fixed key set for exactly
 * this reason (`TestTheAdminJobShapesCarryNothingPrivate`). A generic table would put that decision
 * behind a configuration object, where the next screen inherits it by copying rather than by
 * reading. Writing the cells out means a reviewer sees, in the page, every field the panel shows.
 *
 * So this is presentation and nothing else: one place for the border, the zebra and the spacing, so
 * two screens do not drift apart, and no place at all for what goes in them.
 *
 * # It scrolls inside itself
 *
 * A support screen is read on whatever was to hand, and a table that widens the document gives every
 * other element a horizontal scrollbar. The overflow is on the wrapper.
 */
export function PanelTable({ children }: { children: ReactNode }) {
  return (
    <div className="border-border overflow-x-auto rounded-xl border">
      <table className="w-full border-collapse text-left text-sm">{children}</table>
    </div>
  );
}

export function TH({ children, className }: { children?: ReactNode; className?: string }) {
  return (
    <th
      scope="col"
      className={cn(
        "text-muted-foreground border-border border-b px-3 py-2 text-xs font-medium tracking-wide whitespace-nowrap uppercase",
        className,
      )}
    >
      {children}
    </th>
  );
}

export function TD({ children, className }: { children?: ReactNode; className?: string }) {
  return (
    <td className={cn("border-border border-b px-3 py-2 align-top last:border-0", className)}>
      {children}
    </td>
  );
}

/**
 * The row that says a search matched nothing.
 *
 * **It says "no rows matched" and never anything about permissions**, which matters because the two
 * are rendered by different components on purpose: a refusal goes to `PlatformRefusal` and never
 * reaches a table at all. `Docs/09`'s SHIP-188b row asks precisely that these not be confused, and
 * the way to keep them apart is that the empty state has no branch in it — it means one thing,
 * always, and a screen that has a refusal to render never gets here.
 */
export function EmptyRow({ span, children }: { span: number; children: ReactNode }) {
  return (
    <tr>
      <td colSpan={span} className="text-muted-foreground px-3 py-8 text-center text-sm">
        {children}
      </td>
    </tr>
  );
}
