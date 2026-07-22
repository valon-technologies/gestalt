import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

// In-page region chrome: title · description · actions *outside* a Card / above
// a card grid. Sibling to PageHeader (document h1). Default title is h2 — safe
// to repeat. Spec: guidelines/section-header.md · RES-20260721-008.

const sectionHeaderVariants = cva("flex w-full gap-x-4 gap-y-3", {
  variants: {
    align: {
      // Baseline (not end): actions sit on the title line when a description stacks
      // under it — items-end drops the button to the description.
      between: "flex-col sm:flex-row sm:items-baseline sm:justify-between",
      center:
        "flex-col items-center text-center [&_[data-slot=section-header-content]]:items-center [&_[data-slot=section-header-actions]]:justify-center",
    },
  },
  defaultVariants: {
    align: "between",
  },
});

interface SectionHeaderProps
  extends React.ComponentProps<"div">,
    VariantProps<typeof sectionHeaderVariants> {}

function SectionHeader({ className, align, ...props }: SectionHeaderProps) {
  return (
    <div
      data-slot="section-header"
      className={cn(sectionHeaderVariants({ align }), className)}
      {...props}
    />
  );
}

function SectionHeaderContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-header-content"
      className={cn("flex min-w-0 flex-col gap-1", className)}
      {...props}
    />
  );
}

const sectionHeaderTitleVariants = cva(
  "font-sans font-normal text-balance text-foreground",
  {
    variants: {
      size: {
        sm: "text-lg",
        default: "text-xl",
        lg: "text-2xl",
      },
    },
    defaultVariants: {
      size: "default",
    },
  },
);

type SectionHeaderTitleTag = "h2" | "h3";

interface SectionHeaderTitleProps
  extends Omit<React.ComponentProps<"h2">, "as">,
    VariantProps<typeof sectionHeaderTitleVariants> {
  /** Heading level. Default `h2` — never `h1` (that is PageHeader). */
  as?: SectionHeaderTitleTag;
}

function SectionHeaderTitle({
  className,
  size,
  as: Comp = "h2",
  ...props
}: SectionHeaderTitleProps) {
  return (
    <Comp
      data-slot="section-header-title"
      className={cn(sectionHeaderTitleVariants({ size }), className)}
      {...props}
    />
  );
}

function SectionHeaderDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="section-header-description"
      className={cn("text-pretty text-sm text-muted-foreground", className)}
      {...props}
    />
  );
}

function SectionHeaderActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-header-actions"
      className={cn("flex shrink-0 items-center gap-2", className)}
      {...props}
    />
  );
}

export {
  SectionHeader,
  SectionHeaderContent,
  SectionHeaderTitle,
  SectionHeaderDescription,
  SectionHeaderActions,
  sectionHeaderVariants,
  sectionHeaderTitleVariants,
};
