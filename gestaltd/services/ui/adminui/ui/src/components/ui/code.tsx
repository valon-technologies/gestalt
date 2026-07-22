import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

// Inline `<code>` for UI copy (settings labels, empty states, docs rows) —
// not a fence (`CodeBlock`) and not a keyboard glyph (`Kbd` / Plate kbd-node).
// Paint: muted fill + hairline border + mono, em-relative padding. Corners use
// `rounded-sm` (--radius-sm ≈ 4px) — same soft-rect as Badge, not Button's
// `rounded-md`. Plate `CodeLeaf` / `CodeLeafStatic` consume `codeVariants()`.
const codeVariants = cva(
  "whitespace-pre-wrap rounded-sm border border-border bg-muted px-[0.25em] py-[0.12em] font-mono text-[0.9em] font-normal text-foreground [font-variant-ligatures:none]",
);

export interface CodeProps
  extends React.ComponentProps<"code">,
    VariantProps<typeof codeVariants> {}

function Code({ className, ...props }: CodeProps) {
  return (
    <code data-slot="code" className={cn(codeVariants(), className)} {...props} />
  );
}

export { Code, codeVariants };
