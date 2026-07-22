import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const pageHeaderVariants = cva("flex w-full gap-x-4 gap-y-3", {
  variants: {
    align: {
      between: "flex-col sm:flex-row sm:items-end sm:justify-between",
      center:
        "flex-col items-center text-center [&_[data-slot=page-header-content]]:items-center [&_[data-slot=page-header-actions]]:justify-center",
    },
  },
  defaultVariants: {
    align: "between",
  },
});

interface PageHeaderProps
  extends React.ComponentProps<"header">,
    VariantProps<typeof pageHeaderVariants> {}

function PageHeader({ className, align, ...props }: PageHeaderProps) {
  return (
    <header
      data-slot="page-header"
      className={cn(pageHeaderVariants({ align }), className)}
      {...props}
    />
  );
}

function PageHeaderContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="page-header-content"
      className={cn("flex min-w-0 flex-col gap-1.5", className)}
      {...props}
    />
  );
}

const pageHeaderTitleVariants = cva(
  "font-sans font-normal tracking-tight text-balance text-foreground",
  {
    variants: {
      size: {
        sm: "text-xl",
        default: "text-2xl",
        lg: "text-4xl",
        xl: "text-5xl",
        /** Detail record title above a document body — above typeset H1. */
        entity: "text-4xl font-semibold leading-tight",
      },
      // The Season Serif display face only reads well large, so it is gated to the lg
      // and xl tiers (>=36px) by the compoundVariants below. At smaller sizes `display`
      // is a no-op and the title stays KMR Melange Grotesk. `entity` also takes the
      // display face so detail titles match list chrome at text-4xl.
      // Display compounds pin `font-normal` so Season stays Regular (valon.ai); that
      // override only applies when `display` is on — Melange `entity` keeps semibold.
      display: {
        true: "",
        false: "",
      },
    },
    compoundVariants: [
      { size: "lg", display: true, class: "font-display font-normal" },
      { size: "xl", display: true, class: "font-display font-normal" },
      { size: "entity", display: true, class: "font-display font-normal" },
    ],
    defaultVariants: {
      size: "default",
      display: true,
    },
  },
);

interface PageHeaderTitleProps
  extends Omit<React.ComponentProps<"h1">, "onClick">,
    VariantProps<typeof pageHeaderTitleVariants> {
  /** When set, the title text is an in-header link (SPA or full navigation). */
  href?: string;
  /** When set (and `href` is absent), the title text is an in-header button. */
  onNavigate?: () => void;
}

const pageHeaderTitleInteractiveClassName =
  "cursor-pointer border-0 bg-transparent p-0 text-left font-[inherit] text-inherit no-underline hover:text-inherit focus-ring rounded-sm";

function PageHeaderTitle({
  className,
  size,
  display,
  href,
  onNavigate,
  children,
  ...props
}: PageHeaderTitleProps) {
  let body = children;
  if (href) {
    body = (
      <a href={href} className={pageHeaderTitleInteractiveClassName}>
        {children}
      </a>
    );
  } else if (onNavigate) {
    body = (
      <button type="button" className={pageHeaderTitleInteractiveClassName} onClick={onNavigate}>
        {children}
      </button>
    );
  }

  return (
    <h1
      data-slot="page-header-title"
      className={cn(pageHeaderTitleVariants({ size, display }), className)}
      {...props}
    >
      {body}
    </h1>
  );
}

function PageHeaderDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="page-header-description"
      className={cn("text-pretty text-sm text-muted-foreground", className)}
      {...props}
    />
  );
}

function PageHeaderActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="page-header-actions"
      className={cn("flex shrink-0 items-center gap-2", className)}
      {...props}
    />
  );
}

export {
  PageHeader,
  PageHeaderContent,
  PageHeaderTitle,
  PageHeaderDescription,
  PageHeaderActions,
  pageHeaderVariants,
  pageHeaderTitleVariants,
};
