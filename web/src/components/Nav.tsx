"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { clearSession, getUserEmail } from "@/lib/auth";
import { LOGIN_PATH } from "@/lib/constants";

const links = [
  { href: "/", label: "Dashboard" },
  { href: "/integrations", label: "Integrations" },
  { href: "/tokens", label: "API Tokens" },
  { href: "/docs", label: "Docs" },
];

export default function Nav() {
  const pathname = usePathname();
  const email = getUserEmail();

  function handleLogout() {
    clearSession();
    window.location.href = LOGIN_PATH;
  }

  return (
    <nav className="border-b border-border bg-surface px-6 py-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-8">
          <Link href="/" className="text-lg font-heading font-bold text-timber-800">
            Toolshed
          </Link>
          <div className="flex gap-4">
            {links.map((link) => {
              const className = `text-sm ${
                pathname === link.href
                  ? "font-medium text-timber-600"
                  : "text-stone-500 hover:text-stone-800"
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
        {email && (
          <div className="flex items-center gap-4">
            <span className="text-sm text-stone-400">{email}</span>
            <button
              onClick={handleLogout}
              className="text-sm text-stone-400 hover:text-stone-600"
            >
              Logout
            </button>
          </div>
        )}
      </div>
    </nav>
  );
}
