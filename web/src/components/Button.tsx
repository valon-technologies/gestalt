import { ButtonHTMLAttributes } from "react";

type Variant = "primary" | "secondary" | "danger";

const variantStyles: Record<Variant, string> = {
  primary:
    "bg-base-950 text-white hover:bg-base-900 dark:bg-base-100 dark:text-base-950 dark:hover:bg-base-200",
  secondary:
    "bg-alpha-10 text-primary hover:bg-base-200 dark:hover:bg-base-800",
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
      className={`rounded-md px-6 py-2.5 text-sm font-medium transition-all duration-150 ease-out disabled:opacity-50 disabled:cursor-not-allowed active:translate-y-px ${variantStyles[variant]} ${className}`}
      disabled={disabled}
      {...props}
    >
      {children}
    </button>
  );
}
