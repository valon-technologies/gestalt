"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { usePathname } from "next/navigation";
import VersionPicker from "../VersionPicker";

// nextra-theme-docs exposes no slot above the sidebar nav, and the sidebar is
// rendered by the MDX page wrapper (not the layout), so it can remount on
// navigation. The slot element is created once and re-prepended whenever a
// fresh aside appears — reusing the same node keeps the portal (and the
// picker's fetched version manifest) mounted across route changes instead of
// remounting and refetching on every navigation.
export default function SidebarVersionPicker() {
  const pathname = usePathname();
  const slotRef = useRef<HTMLElement | null>(null);
  const [slot, setSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    const sidebar = document.querySelector("aside.nextra-sidebar");
    if (!sidebar) {
      setSlot(null);
      return;
    }
    if (!slotRef.current) {
      const element = document.createElement("div");
      element.className = "docs-sidebar-version";
      slotRef.current = element;
    }
    if (slotRef.current.parentElement !== sidebar) {
      sidebar.prepend(slotRef.current);
    }
    setSlot(slotRef.current);
  }, [pathname]);

  useEffect(() => {
    return () => {
      slotRef.current?.remove();
    };
  }, []);

  return slot ? createPortal(<VersionPicker />, slot) : null;
}
