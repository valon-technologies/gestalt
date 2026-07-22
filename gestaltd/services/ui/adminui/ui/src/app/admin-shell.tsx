import type { ReactNode } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import { adminConfig } from "@/lib/admin-config";

type AdminShellProps = {
  children: ReactNode;
};

const navItems = [
  { to: "/metrics", label: "Metrics" },
  { to: "/registry", label: "App Registry" },
] as const;

export function AdminShell({ children }: AdminShellProps) {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const { brandHref } = adminConfig();

  return (
    <div className="min-h-svh bg-background">
      <header className="border-b border-border bg-card">
        <div className="mx-auto flex min-h-[60px] w-full max-w-[1100px] items-center justify-between gap-4 px-4">
          <a href={brandHref} className="text-lg font-semibold text-foreground no-underline">
            Gestalt
          </a>
          <nav className="flex items-center gap-2">
            {navItems.map((item) => {
              const active = pathname === item.to || pathname.startsWith(`${item.to}/`);
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  className={cn(
                    "rounded-md px-2.5 py-1.5 text-sm text-muted-foreground no-underline transition-colors",
                    active && "bg-muted text-foreground",
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>
      </header>
      <main className="mx-auto w-full max-w-[1100px] px-4 py-8">{children}</main>
    </div>
  );
}
