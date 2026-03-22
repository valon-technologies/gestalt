"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Integration } from "@/lib/api";
import Button from "./Button";
import { CheckCircleIcon } from "./icons";

const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

function CloseIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <line x1="18" y1="6" x2="6" y2="18" />
      <line x1="6" y1="6" x2="18" y2="18" />
    </svg>
  );
}

interface IntegrationSettingsModalProps {
  integration: Integration;
  onClose: () => void;
  onReconnect: () => void;
  onDisconnect: () => void;
  reconnecting: boolean;
  disconnecting: boolean;
  error: string | null;
}

export default function IntegrationSettingsModal({
  integration,
  onClose,
  onReconnect,
  onDisconnect,
  reconnecting,
  disconnecting,
  error,
}: IntegrationSettingsModalProps) {
  const [mounted, setMounted] = useState(false);
  const [confirmingDisconnect, setConfirmingDisconnect] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setMounted(true);
  }, []);

  const handleDismiss = useCallback(() => {
    if (!disconnecting) onClose();
  }, [disconnecting, onClose]);

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === "Escape") {
      handleDismiss();
      return;
    }
    if (e.key === "Tab" && panelRef.current) {
      const focusable = panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }, [handleDismiss]);

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, []);

  useEffect(() => {
    if (!panelRef.current) return;
    panelRef.current.querySelector<HTMLElement>(FOCUSABLE)?.focus();
  }, [confirmingDisconnect, mounted]);

  if (!mounted) return null;

  const displayName = integration.display_name || integration.name;
  const headingId = `settings-modal-heading-${integration.name}`;

  const modal = (
    <div className="fixed inset-0 z-50" role="presentation">
      <div className="fixed inset-0 bg-stone-900/50" aria-hidden="true" />
      <div className="fixed inset-0 flex items-center justify-center p-4" onClick={handleDismiss}>
        <div
          ref={panelRef}
          role="dialog"
          aria-modal="true"
          aria-labelledby={headingId}
          className="relative w-full max-w-sm rounded-lg border border-border bg-surface p-6 shadow-warm"
          onClick={(e) => e.stopPropagation()}
        >
          {confirmingDisconnect ? (
            <>
              <h2 id={headingId} className="text-lg font-heading font-semibold text-stone-900">
                Disconnect {displayName}?
              </h2>
              <p className="mt-2 text-sm text-stone-500">
                This will remove your {displayName} integration. You can reconnect at any time.
              </p>
              {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}
              <div className="mt-6 flex gap-3">
                <Button
                  variant="secondary"
                  className="flex-1"
                  onClick={() => setConfirmingDisconnect(false)}
                  disabled={disconnecting}
                >
                  Cancel
                </Button>
                <Button
                  variant="danger"
                  className="flex-1"
                  onClick={onDisconnect}
                  disabled={disconnecting}
                >
                  {disconnecting ? "Disconnecting..." : "Disconnect"}
                </Button>
              </div>
            </>
          ) : (
            <>
              <div className="flex items-start justify-between">
                <h2 id={headingId} className="text-lg font-heading font-semibold text-stone-900">
                  {displayName}
                </h2>
                <button
                  onClick={onClose}
                  className="rounded p-1 text-stone-400 hover:text-stone-600 transition-colors"
                  aria-label="Close"
                >
                  <CloseIcon className="h-4 w-4" />
                </button>
              </div>

              <div className="mt-4 flex items-center gap-2">
                <CheckCircleIcon className="h-5 w-5 text-grove-500" />
                <span className="text-sm font-medium text-grove-600">Connected</span>
              </div>

              {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}

              <div className="mt-6">
                <Button className="w-full" onClick={onReconnect} disabled={reconnecting}>
                  {reconnecting ? "Connecting..." : "Reconnect"}
                </Button>
              </div>

              <div className="mt-4 border-t border-border pt-4">
                <Button variant="danger" className="w-full" onClick={() => setConfirmingDisconnect(true)}>
                  Disconnect
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );

  return createPortal(modal, document.body);
}
