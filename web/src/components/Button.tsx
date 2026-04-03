import { ButtonHTMLAttributes } from "react";

type Variant = "primary" | "secondary" | "danger";

const variantStyles: Record<Variant, string> = {
  primary:
    "bg-base-950 text-base-white hover:bg-[rgba(35,24,16,0.9)] dark:bg-base-100 dark:text-base-950 dark:hover:bg-base-200",
  secondary:
    "bg-alpha-10 text-primary hover:bg-[rgba(35,24,16,0.18)] dark:hover:bg-base-800",
  danger:
    "bg-ember-600 text-white hover:bg-ember-700 dark:bg-ember-500 dark:hover:bg-ember-600",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
}

export default function Button({
  variant = "primary",
  className = "",
  disabled,
  children,
  ...props
}: ButtonProps) {
  return (
    <button
      className={`inline-flex min-h-12 items-center justify-center rounded-md px-6 py-3 text-sm font-bold tracking-[0.01em] transition-all duration-150 ease-out focus:outline-none focus:ring-2 focus:ring-base-950/15 focus:ring-offset-2 focus:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 active:translate-y-px dark:focus:ring-base-100/20 ${variantStyles[variant]} ${className}`}
      disabled={disabled}
      {...props}
    >
      {children}
    </button>
  );
}
