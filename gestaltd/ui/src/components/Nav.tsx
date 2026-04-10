"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { clearSession, getUserEmail } from "@/lib/auth";
import { getAuthInfo, logout } from "@/lib/api";
import { DOCS_PATH, LOGIN_PATH } from "@/lib/constants";
import { useTheme } from "@/hooks/use-theme";
import { MoonIcon, SunIcon, SunMoonIcon } from "./icons";

const links = [
  { href: "/", label: "Dashboard" },
  { href: "/integrations", label: "Integrations" },
  { href: "/tokens", label: "API Tokens" },
  { href: DOCS_PATH, label: "Docs" },
];

export default function Nav() {
  const pathname = usePathname();
  const [email, setEmail] = useState<string | null>(null);
  const [loginSupported, setLoginSupported] = useState(false);
  const { theme, setTheme } = useTheme();

  useEffect(() => {
    setEmail(getUserEmail());
  }, []);
  const ThemeIcon = theme === "light" ? SunIcon : theme === "dark" ? MoonIcon : SunMoonIcon;

  useEffect(() => {
    if (!email) {
      setLoginSupported(false);
      return;
    }

    let active = true;
    getAuthInfo()
      .then((info) => {
        if (active) {
          setLoginSupported(info.loginSupported);
        }
      })
      .catch(() => {
        if (active) {
          setLoginSupported(true);
        }
      });

    return () => {
      active = false;
    };
  }, [email]);

  async function handleLogout() {
    await logout().catch(() => {});
    clearSession();
    window.location.href = LOGIN_PATH;
  }

  return (
    <nav className="border-b border-alpha px-6 py-3 bg-background/80 backdrop-blur-sm sticky top-0 z-50">
      <div className="mx-auto max-w-5xl flex items-center justify-between">
        <div className="flex items-center gap-8">
          <Link href="/" className="text-lg font-heading font-bold text-primary">
            Gestalt
          </Link>
          <div className="flex gap-5">
            {links.map((link) => {
              const isActive =
                pathname === link.href ||
                (link.href !== "/" && pathname.startsWith(link.href + "/"));
              const className = `text-sm transition-colors duration-150 ${
                isActive
                  ? "text-primary font-medium"
                  : "text-muted hover:text-secondary"
              }`;
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
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-all duration-150 hover:bg-alpha-5"
            title={theme === "light" ? "Light mode" : theme === "dark" ? "Dark mode" : "System preference"}
            aria-label="Toggle theme"
          >
            <ThemeIcon className="h-[18px] w-[18px]" />
          </button>
          {email && (
            <>
              <span className="text-sm text-faint">{email}</span>
              {loginSupported && (
                <button
                  onClick={handleLogout}
                  className="text-sm text-muted hover:text-primary transition-colors duration-150"
                >
                  Logout
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </nav>
  );
}
