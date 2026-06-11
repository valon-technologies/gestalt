"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { usePathname } from "next/navigation";
import VersionPicker from "../VersionPicker";

// nextra-theme-docs exposes no slot above the sidebar nav, and the sidebar is
// rendered by the MDX page wrapper (not the layout), so it can remount on
// navigation — the picker is portaled into a slot prepended to the aside on
// every route change.
export default function SidebarVersionPicker() {
  const pathname = usePathname();
  const [slot, setSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    const sidebar = document.querySelector("aside.nextra-sidebar");
    if (!sidebar) {
      setSlot(null);
      return;
    }
    const element = document.createElement("div");
    element.className = "docs-sidebar-version";
    sidebar.prepend(element);
    setSlot(element);
    return () => {
      element.remove();
    };
  }, [pathname]);

  return slot ? createPortal(<VersionPicker />, slot) : null;
}
