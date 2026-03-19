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
    <nav className="border-b border-gray-200 bg-white px-6 py-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-8">
          <Link href="/" className="text-lg font-semibold text-gray-900">
            Toolshed
          </Link>
          <div className="flex gap-4">
            {links.map((link) => {
              const className = `text-sm ${
                pathname === link.href
                  ? "font-medium text-blue-600"
                  : "text-gray-600 hover:text-gray-900"
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
            <span className="text-sm text-gray-500">{email}</span>
            <button
              onClick={handleLogout}
              className="text-sm text-gray-500 hover:text-gray-700"
            >
              Logout
            </button>
          </div>
        )}
      </div>
    </nav>
  );
}
