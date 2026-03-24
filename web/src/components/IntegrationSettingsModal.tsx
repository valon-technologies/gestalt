"use client";

import { useEffect, useRef, useState } from "react";
import { Integration } from "@/lib/api";
import Button from "./Button";
import { CheckCircleIcon, CloseIcon } from "./icons";

interface IntegrationSettingsModalProps {
  integration: Integration;
  onClose: () => void;
  onReconnect: () => void;
  onReconnectManual?: () => void;
  onDisconnect: (instance?: string) => void;
  reconnecting: boolean;
  disconnecting: boolean;
  error: string | null;
}

export default function IntegrationSettingsModal({
  integration,
  onClose,
  onReconnect,
  onReconnectManual,
  onDisconnect,
  reconnecting,
  disconnecting,
  error,
}: IntegrationSettingsModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [confirmingDisconnect, setConfirmingDisconnect] = useState(false);
  const [disconnectInstance, setDisconnectInstance] = useState<string | undefined>();

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
                onClick={() => { setConfirmingDisconnect(false); setDisconnectInstance(undefined); }}
                disabled={disconnecting}
              >
                Cancel
              </Button>
              <Button
                variant="danger"
                className="flex-1"
                onClick={() => onDisconnect(disconnectInstance)}
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
                {integration.instances && integration.instances.length > 0 && (
                  <div className="mt-4 space-y-2">
                    {integration.instances.map((inst) => (
                      <div key={inst.name} className="flex items-center justify-between rounded border border-border px-3 py-2">
                        <div className="flex items-center gap-2">
                          <CheckCircleIcon className="h-4 w-4 text-grove-500" />
                          <span className="text-sm text-stone-700">{inst.name}</span>
                        </div>
                        <button
                          onClick={() => { setDisconnectInstance(inst.name); setConfirmingDisconnect(true); }}
                          disabled={disconnecting}
                          className="text-xs text-ember-500 hover:text-ember-600"
                        >
                          Disconnect
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}

                <div className="mt-6 flex flex-col gap-2">
                  <Button
                    className="w-full"
                    onClick={onReconnect}
                    disabled={reconnecting}
                  >
                    {reconnecting ? "Connecting..." : "Add Connection"}
                  </Button>
                  {onReconnectManual && (
                    <Button
                      variant="secondary"
                      className="w-full"
                      onClick={onReconnectManual}
                      disabled={reconnecting}
                    >
                      Add with API Token
                    </Button>
                  )}
                </div>
              </>
            ) : (
              <>
                <p className="mt-4 text-sm text-stone-500">Not connected</p>

                {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}

                <div className="mt-6 flex flex-col gap-2">
                  <Button
                    className="w-full"
                    onClick={onReconnect}
                    disabled={reconnecting}
                  >
                    {reconnecting ? "Connecting..." : onReconnectManual ? "Connect with OAuth" : "Connect"}
                  </Button>
                  {onReconnectManual && (
                    <Button
                      variant="secondary"
                      className="w-full"
                      onClick={onReconnectManual}
                      disabled={reconnecting}
                    >
                      Use API Token
                    </Button>
                  )}
                </div>
              </>
            )}
          </>
        )}
      </div>
    </dialog>
  );
}
