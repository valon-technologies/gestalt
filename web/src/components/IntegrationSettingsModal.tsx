"use client";

import { useEffect, useRef, useState } from "react";
import { Integration } from "@/lib/api";
import Button from "./Button";
import { CheckCircleIcon, CloseIcon } from "./icons";

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
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [confirmingDisconnect, setConfirmingDisconnect] = useState(false);

  useEffect(() => {
    dialogRef.current?.showModal();
  }, []);

  const displayName = integration.display_name || integration.name;
  const headingId = `settings-modal-heading-${integration.name}`;

  function handleCancel(e: React.SyntheticEvent<HTMLDialogElement>) {
    if (disconnecting) {
      e.preventDefault();
    }
  }

  function handleBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === e.currentTarget && !disconnecting) {
      e.currentTarget.close();
    }
  }

  function closeDialog() {
    dialogRef.current?.close();
  }

  return (
    <dialog
      ref={dialogRef}
      aria-labelledby={headingId}
      onCancel={handleCancel}
      onClose={onClose}
      onClick={handleBackdropClick}
      className="m-auto w-full max-w-sm rounded-lg border border-border bg-surface p-0 shadow-warm"
    >
      <div className="p-6">
        {confirmingDisconnect ? (
          <>
            <h2
              id={headingId}
              className="text-lg font-heading font-semibold text-stone-900"
            >
              Disconnect {displayName}?
            </h2>
            <p className="mt-2 text-sm text-stone-500">
              This will remove your {displayName} integration. You can reconnect
              at any time.
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
              <h2
                id={headingId}
                className="text-lg font-heading font-semibold text-stone-900"
              >
                {displayName}
              </h2>
              <button
                onClick={closeDialog}
                className="rounded p-1 text-stone-400 hover:text-stone-600 transition-colors"
                aria-label="Close"
              >
                <CloseIcon className="h-4 w-4" />
              </button>
            </div>

            {integration.connected ? (
              <>
                <div className="mt-4 flex items-center gap-2">
                  <CheckCircleIcon className="h-5 w-5 text-grove-500" />
                  <span className="text-sm font-medium text-grove-600">
                    Connected
                  </span>
                </div>

                {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}

                <div className="mt-6">
                  <Button
                    className="w-full"
                    onClick={onReconnect}
                    disabled={reconnecting}
                  >
                    {reconnecting ? "Connecting..." : "Reconnect"}
                  </Button>
                </div>

                <div className="mt-4 border-t border-border pt-4">
                  <Button
                    variant="danger"
                    className="w-full"
                    onClick={() => setConfirmingDisconnect(true)}
                  >
                    Disconnect
                  </Button>
                </div>
              </>
            ) : (
              <>
                <p className="mt-4 text-sm text-stone-500">Not connected</p>

                {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}

                <div className="mt-6">
                  <Button
                    className="w-full"
                    onClick={onReconnect}
                    disabled={reconnecting}
                  >
                    {reconnecting ? "Connecting..." : "Connect"}
                  </Button>
                </div>
              </>
            )}
          </>
        )}
      </div>
    </dialog>
  );
}
