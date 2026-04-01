"use client";

import { useEffect, useState } from "react";

type Theme = "light" | "dark" | "system";

function readTheme(): Theme {
  if (typeof window === "undefined") return "system";
  const stored = localStorage.getItem("theme");
  return stored === "light" || stored === "dark" || stored === "system"
    ? stored
    : "system";
}

function applyTheme(theme: Theme) {
  if (typeof window === "undefined") return;
  const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.classList.toggle(
    "dark",
    theme === "dark" || (theme === "system" && prefersDark),
  );
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>("system");

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const syncTheme = () => {
      const current = readTheme();
      setThemeState(current);
      applyTheme(current);
    };

    syncTheme();
    window.addEventListener("storage", syncTheme);
    media.addEventListener("change", syncTheme);
    return () => {
      window.removeEventListener("storage", syncTheme);
      media.removeEventListener("change", syncTheme);
    };
  }, []);

  function setTheme(nextTheme: Theme) {
    localStorage.setItem("theme", nextTheme);
    setThemeState(nextTheme);
    applyTheme(nextTheme);
    window.dispatchEvent(new Event("storage"));
  }

  return { theme, setTheme };
}
