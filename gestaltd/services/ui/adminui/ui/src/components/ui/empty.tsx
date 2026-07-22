import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

/**
 * Empty — no-data / no-results / first-run placeholder.
 *
 * `size`:
 * - `default` — page / table / card first-run (flex-1, large padding, centered).
 * - `sm` — dense nested rails and panels (flex-none, compact padding, start-aligned,
 *   rounded-md so it nests one step under rounded-lg chrome). Owns density; call sites
 *   should not re-fight p-6/md:p-12 or flex-1 (registry-call-site-overrides /
 *   RES-20260722-001).
 */
const emptyVariants = cva(
  "group/empty flex min-w-0 flex-col text-balance",
  {
    variants: {
      size: {
        default:
          "flex-1 items-center justify-center gap-6 rounded-lg p-6 text-center md:p-12",
        sm: "flex-none items-stretch justify-start gap-0 rounded-md p-3 text-left",
      },
    },
    defaultVariants: {
      size: "default",
    },
  },
);

function Empty({
  className,
  size,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof emptyVariants>) {
  const resolvedSize = size ?? "default";
  return (
    <div
      data-slot="empty"
      data-size={resolvedSize}
      className={cn(emptyVariants({ size: resolvedSize }), className)}
      {...props}
    />
  );
}

function EmptyHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-header"
      className={cn(
        "flex max-w-sm flex-col items-center gap-2 text-center",
        "group-data-[size=sm]/empty:max-w-none group-data-[size=sm]/empty:items-start group-data-[size=sm]/empty:gap-0 group-data-[size=sm]/empty:text-left",
        className,
      )}
      {...props}
    />
  );
}

const emptyMediaVariants = cva(
  "mb-2 flex shrink-0 items-center justify-center [&_svg:not([class*='size-'])]:size-6 group-data-[size=sm]/empty:mb-1 group-data-[size=sm]/empty:[&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-transparent text-muted-foreground",
        icon: "size-12 rounded-lg bg-muted text-foreground group-data-[size=sm]/empty:size-8 group-data-[size=sm]/empty:rounded-md",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

interface EmptyMediaProps
  extends React.ComponentProps<"div">,
    VariantProps<typeof emptyMediaVariants> {}

function EmptyMedia({ className, variant, ...props }: EmptyMediaProps) {
  return (
    <div
      data-slot="empty-media"
      data-variant={variant ?? "default"}
      className={cn(emptyMediaVariants({ variant }), className)}
      {...props}
    />
  );
}

function EmptyTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-title"
      className={cn(
        "text-lg font-medium tracking-tight text-foreground",
        // sm: Regular (400) + xs — Melange Medium is too heavy for ambient rail hints;
        // apps may still mute with text-muted-foreground-soft at the call site.
        "group-data-[size=sm]/empty:text-xs group-data-[size=sm]/empty:font-normal group-data-[size=sm]/empty:leading-4 group-data-[size=sm]/empty:tracking-normal",
        className,
      )}
      {...props}
    />
  );
}

function EmptyDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="empty-description"
      className={cn(
        "text-pretty text-sm/relaxed text-muted-foreground [&>a]:underline [&>a]:underline-offset-4 [&>a:hover]:text-foreground",
        "group-data-[size=sm]/empty:text-xs/relaxed",
        className,
      )}
      {...props}
    />
  );
}

function EmptyContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-content"
      className={cn(
        "flex w-full max-w-sm min-w-0 flex-col items-center gap-3 text-sm text-balance",
        "group-data-[size=sm]/empty:items-start group-data-[size=sm]/empty:gap-2 group-data-[size=sm]/empty:text-xs",
        className,
      )}
      {...props}
    />
  );
}

export {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
  EmptyContent,
  emptyVariants,
  emptyMediaVariants,
};
