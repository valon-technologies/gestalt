import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

// Hover/press feedback is an on-color state-layer overlay, not a brightness
// filter (press-feedback.md / RES-20260617-004): the `::after` scrim paints in
// currentColor — the variant's on-color/label ink — so it lightens a near-black
// fill and darkens a near-cream one with no luminance check, where
// `brightness()` had no headroom on the dark `default` fill. Opacity snaps
// (never transitioned). Disabled is a recolor to a flat neutral, not opacity-50
// (disabled-states.md / RES-20260617-003): the brand hue is erased so disabled
// can't read as a dimmer enabled. Disabled opts out of the scrim.
//
// We deliberately DON'T set `disabled:pointer-events-none` — that would suppress
// `cursor-not-allowed`. A disabled control can still match `:hover`, but every
// variant's hover utility (danger/ghost bg/text, and the scrim)
// is overridden by a `disabled:` rule emitted LATER at equal specificity, so the
// disabled recolor always wins the cascade — no hover leaks through.
//
// Icon glyphs scale per size via --control-icon-* (control-sizing.md /
// RES-20260701-002). Icons with an explicit size-* class opt out of the default.
const buttonVariants = cva(
  "relative inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md text-sm font-medium transition-[color,background-color,border-color] duration-hover-out ease-out-quart hover:duration-hover-in focus-ring after:pointer-events-none after:absolute after:inset-0 after:rounded-[inherit] after:bg-current after:opacity-0 after:transition-none hover:after:opacity-[var(--state-overlay-hover,0.08)] active:after:opacity-[var(--state-overlay-press,0.14)] disabled:cursor-not-allowed disabled:border-border disabled:bg-disabled disabled:text-disabled-foreground disabled:shadow-none disabled:after:hidden [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-default)]",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground",
        destructive: "bg-destructive text-destructive-foreground",
        success: "border border-success-foreground/30 bg-success/40 text-success-foreground hover:border-success-foreground/50 hover:bg-success hover:text-success-foreground",
        danger: "border border-destructive/30 bg-destructive/5 text-destructive hover:border-destructive hover:bg-destructive hover:text-destructive-foreground",
        outline: "border border-input bg-background hover:bg-neutral-hover active:bg-neutral-pressed hover:after:opacity-0 active:after:opacity-0 disabled:bg-transparent",
        secondary: "bg-secondary text-secondary-foreground",
        // Ghost is transparent chrome: hover/press use the base ::after on-color scrim only
        // (RES-20260617-004), so it composites on neutral and tinted parents alike — not
        // --accent-hover, which is reserved for dropdown items and breadcrumb triggers.
        ghost: "text-muted-foreground hover:text-foreground aria-checked:bg-foreground aria-checked:text-background disabled:bg-transparent",
        ghostSuccess: "text-muted-foreground hover:bg-success hover:text-success-foreground active:bg-success/70 disabled:bg-transparent",
        ghostDestructive: "text-muted-foreground hover:bg-destructive hover:text-destructive-foreground active:bg-destructive/70 disabled:bg-transparent",
      },
      size: {
        xs: "h-control-xs gap-1 rounded-md px-2 text-control-xs has-[>svg]:px-1.5 [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-xs)]",
        sm: "h-control-sm rounded-md px-2 text-control-sm [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-sm)]",
        default: "h-control-default px-2 py-2 text-control-default",
        lg: "h-control-lg rounded-md px-8 text-control-lg [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-lg)]",
        icon: "size-control-default",
        "icon-xs": "size-control-xs rounded-md [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-xs)]",
        "icon-sm": "size-control-sm [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-sm)]",
        "icon-lg": "size-control-lg [&_svg:not([class*='size-'])]:size-[length:var(--control-icon-lg)]",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
