"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { clearSession, getUserEmail } from "@/lib/auth";
import { logout } from "@/lib/api";
import { LOGIN_PATH } from "@/lib/constants";
import { useTheme } from "@/hooks/use-theme";
import { MoonIcon, SunIcon, SunMoonIcon } from "./icons";

const links = [
  { href: "/", label: "Dashboard" },
  { href: "/integrations", label: "Integrations" },
  { href: "/tokens", label: "API Tokens" },
  { href: "/docs", label: "Docs" },
];

export default function Nav() {
  const pathname = usePathname();
  const email = getUserEmail();
  const { theme, setTheme } = useTheme();
  const ThemeIcon = theme === "light" ? SunIcon : theme === "dark" ? MoonIcon : SunMoonIcon;

  async function handleLogout() {
    await logout().catch(() => {});
    clearSession();
    window.location.href = LOGIN_PATH;
  }

  return (
    <nav className="border-b border-border bg-surface px-6 py-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-8">
          <Link href="/" className="text-lg font-heading font-bold text-timber-800 dark:text-timber-200">
            Gestalt
          </Link>
          <div className="flex gap-4">
            {links.map((link) => {
              const className = `text-sm ${
                pathname === link.href
                  ? "font-medium text-timber-600 dark:text-timber-400"
                  : "text-stone-500 hover:text-stone-800 dark:text-stone-400 dark:hover:text-stone-200"
              }`;
              if (link.href === "/docs") {
                return (
                  <a key={link.href} href={link.href} className={className}>
                    {link.label}
                  </a>
                );
              }
              return (
                <Link key={link.href} href={link.href} className={className}>
                  {link.label}
                </Link>
              );
            })}
          </div>
        </div>
        <div className="flex items-center gap-4">
          <button
            onClick={() => {
              if (theme === "light") setTheme("dark");
              else if (theme === "dark") setTheme("system");
              else setTheme("light");
            }}
            className="flex h-8 w-8 items-center justify-center rounded text-stone-400 transition-colors hover:bg-stone-100 hover:text-stone-600 dark:text-stone-500 dark:hover:bg-stone-800 dark:hover:text-stone-300"
            title={theme === "light" ? "Light mode" : theme === "dark" ? "Dark mode" : "System preference"}
            aria-label="Toggle theme"
          >
            <ThemeIcon className="h-5 w-5" />
          </button>
          {email && (
            <>
              <span className="text-sm text-stone-400 dark:text-stone-500">{email}</span>
              <button
                onClick={handleLogout}
                className="text-sm text-stone-400 hover:text-stone-600 dark:text-stone-500 dark:hover:text-stone-300"
              >
                Logout
              </button>
            </>
          )}
        </div>
      </div>
    </nav>
  );
}
