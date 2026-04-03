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
  const ThemeIcon =
    theme === "light" ? SunIcon : theme === "dark" ? MoonIcon : SunMoonIcon;

  async function handleLogout() {
    await logout().catch(() => {});
    clearSession();
    window.location.href = LOGIN_PATH;
  }

  return (
    <nav className="nav-shell">
      <div className="nav-inner gap-4">
        <div className="flex min-w-0 items-center gap-4 md:gap-8">
          <Link href="/" className="brand-lockup shrink-0">
            Gestalt
          </Link>
          <div className="hidden items-center gap-1 md:flex">
            {links.map((link) => {
              const isActive = pathname === link.href;
              const className = isActive
                ? "nav-link-active"
                : "nav-link px-3 py-2";
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
        <div className="flex items-center gap-2 md:gap-4">
          <button
            onClick={() => {
              if (theme === "light") setTheme("dark");
              else if (theme === "dark") setTheme("system");
              else setTheme("light");
            }}
            className="icon-button"
            title={
              theme === "light"
                ? "Light mode"
                : theme === "dark"
                  ? "Dark mode"
                  : "System preference"
            }
            aria-label="Toggle theme"
          >
            <ThemeIcon className="h-[18px] w-[18px]" />
          </button>
          {email && (
            <>
              <span className="hidden text-sm text-faint lg:block">{email}</span>
              <button onClick={handleLogout} className="nav-link px-3 py-2">
                Logout
              </button>
            </>
          )}
        </div>
      </div>
      <div className="mx-auto flex w-full justify-center pb-3 md:hidden">
        <div className="flex w-[min(100%,1300px)] gap-1 overflow-x-auto px-4">
          {links.map((link) => {
            const isActive = pathname === link.href;
            const className = isActive
              ? "nav-link-active"
              : "nav-link px-3 py-2";
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
    </nav>
  );
}
