"use client";

import { useEffect, useState } from "react";
import { useTheme } from "nextra-theme-docs";

// Hand-drawn glyphs: stroked primitives on a 256 grid, bold stroke (24).
// Visual style inspired by Phosphor Icons (phosphoricons.com, MIT) — no
// Phosphor assets or path data are used (RES-20260610-009).
function glyph(children: React.ReactNode) {
  return (
    <svg
      aria-hidden="true"
      fill="none"
      height="14"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="24"
      viewBox="0 0 256 256"
      width="14"
    >
      {children}
    </svg>
  );
}

const icons = {
  light: glyph(
    <>
      <circle cx="128" cy="128" r="52" />
      <line x1="128" y1="20" x2="128" y2="44" />
      <line x1="128" y1="212" x2="128" y2="236" />
      <line x1="20" y1="128" x2="44" y2="128" />
      <line x1="212" y1="128" x2="236" y2="128" />
      <line x1="52" y1="52" x2="69" y2="69" />
      <line x1="187" y1="187" x2="204" y2="204" />
      <line x1="52" y1="204" x2="69" y2="187" />
      <line x1="187" y1="69" x2="204" y2="52" />
    </>,
  ),
  dark: glyph(
    <path d="M216.7 152.6A91.9 91.9 0 0 1 103.4 39.3 92 92 0 1 0 216.7 152.6Z" />,
  ),
  system: glyph(
    <>
      <rect x="32" y="48" width="192" height="144" rx="16" />
      <line x1="96" y1="224" x2="160" y2="224" />
    </>,
  ),
} as const;

// Nextra's own ThemeSwitch returns null when the Layout `darkMode` affordances
// are disabled, so the shared footer carries its own switch on next-themes.
export default function ThemeSelect() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  const value = mounted ? (theme ?? "system") : "system";

  return (
    <label className="shell-theme-select">
      <span className="shell-theme-select-label">Theme</span>
      <span className="shell-theme-select-control">
        <span aria-hidden="true" className="shell-theme-select-icon">
          {icons[value as keyof typeof icons] ?? icons.system}
        </span>
        <select
          className="shell-select"
          value={value}
          onChange={(event) => setTheme(event.target.value)}
        >
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
      </span>
    </label>
  );
}
