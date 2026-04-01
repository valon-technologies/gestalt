"use client";

import { useEffect, useRef, useState } from "react";
import { ConnectionParamDef, CredentialFieldDef, Integration } from "@/lib/api";
import Button from "./Button";
import { CheckCircleIcon, CloseIcon } from "./icons";

type ModalView = "default" | "disconnect" | "instance" | "token";
type AuthType = "oauth" | "manual";
type ConnectionTarget = {
  instance?: string;
  connection?: string;
};

type AuthTarget = {
  key: string;
  connection?: string;
  label: string;
  authTypes: AuthType[];
  credentialFields?: CredentialFieldDef[];
};

type AuthAction = {
  key: string;
  authType: AuthType;
  connection?: string;
  label: string;
  variant?: "primary" | "secondary";
};

type PendingAuthAction = ConnectionTarget & {
  authType: AuthType;
};

interface IntegrationSettingsModalProps {
  integration: Integration;
  onClose: () => void;
  onStartOAuth: (instance?: string, connection?: string) => void;
  onSubmitToken: (credential: string | Record<string, string>, connectionParams?: Record<string, string>, instance?: string, connection?: string) => void;
  onDisconnect: (instance?: string, connection?: string) => void;
  reconnecting: boolean;
  disconnecting: boolean;
  submitting: boolean;
  error: string | null;
}

function normalizeAuthTypes(authTypes?: AuthType[], fallbackToOAuth = true): AuthType[] {
  const normalized: AuthType[] = [];
  if (authTypes?.includes("oauth")) {
    normalized.push("oauth");
  }
  if (authTypes?.includes("manual")) {
    normalized.push("manual");
  }
  if (normalized.length === 0 && fallbackToOAuth) {
    normalized.push("oauth");
  }
  return normalized;
}

function resolveAuthTargets(integration: Integration): AuthTarget[] {
  const defaultCredentialFields = integration.credential_fields;
  if (integration.connections?.length) {
    return integration.connections
      .map((connection) => ({
        key: connection.name,
        connection: connection.name,
        label: connection.name,
        authTypes: normalizeAuthTypes(connection.auth_types, false),
        credentialFields: connection.credential_fields?.length
          ? connection.credential_fields
          : defaultCredentialFields,
      }))
      .filter((target) => target.authTypes.length > 0);
  }

  return [{
    key: integration.name,
    label: integration.display_name || integration.name,
    authTypes: normalizeAuthTypes(integration.auth_types),
    credentialFields: defaultCredentialFields,
  }];
}

function buildAuthActionLabel(
  target: AuthTarget,
  authType: AuthType,
  connected: boolean,
  showTargetNames: boolean,
): string {
  const dualAuth = target.authTypes.includes("oauth") && target.authTypes.includes("manual");
  if (connected) {
    if (authType === "manual" && dualAuth) {
      return showTargetNames ? `Add ${target.label} with API Token` : "Add with API Token";
    }
    return showTargetNames ? `Add ${target.label}` : "Add Connection";
  }

  if (authType === "manual") {
    if (dualAuth) {
      return showTargetNames ? `Use API Token for ${target.label}` : "Use API Token";
    }
    return showTargetNames ? `Connect with ${target.label}` : "Connect";
  }

  return showTargetNames ? `Connect with ${target.label}` : dualAuth ? "Connect with OAuth" : "Connect";
}

function buildAuthActions(targets: AuthTarget[], connected: boolean): AuthAction[] {
  const showTargetNames = targets.length > 1;
  const actions: AuthAction[] = [];

  for (const target of targets) {
    if (target.authTypes.includes("oauth")) {
      actions.push({
        key: `${target.key}:oauth`,
        authType: "oauth",
        connection: target.connection,
        label: buildAuthActionLabel(target, "oauth", connected, showTargetNames),
      });
    }
    if (target.authTypes.includes("manual")) {
      actions.push({
        key: `${target.key}:manual`,
        authType: "manual",
        connection: target.connection,
        label: buildAuthActionLabel(target, "manual", connected, showTargetNames),
        variant: target.authTypes.includes("oauth") ? "secondary" : "primary",
      });
    }
  }

  return actions;
}

export default function IntegrationSettingsModal({
  integration,
  onClose,
  onStartOAuth,
  onSubmitToken,
  onDisconnect,
  reconnecting,
  disconnecting,
  submitting,
  error,
}: IntegrationSettingsModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [view, setView] = useState<ModalView>("default");
  const [disconnectTarget, setDisconnectTarget] = useState<ConnectionTarget>({});
  const [pendingAction, setPendingAction] = useState<PendingAuthAction | undefined>();

  useEffect(() => {
    dialogRef.current?.showModal();
  }, []);

  const displayName = integration.display_name || integration.name;
  const headingId = `settings-modal-heading-${integration.name}`;
  const authTargets = resolveAuthTargets(integration);
  const defaultTarget = authTargets.length === 1 ? authTargets[0] : undefined;
  const defaultConnection = defaultTarget?.connection;
  const connectionActions = buildAuthActions(authTargets, !!integration.connected);
  const needsParams = integration.connection_params && Object.keys(integration.connection_params).length > 0;

  function handleCancel(e: React.SyntheticEvent<HTMLDialogElement>) {
    if (disconnecting || submitting) {
      e.preventDefault();
    }
  }

  function handleBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === e.currentTarget && !disconnecting && !submitting) {
      e.currentTarget.close();
    }
  }

  function closeDialog() {
    dialogRef.current?.close();
  }

  function startAddConnection(authType: "oauth" | "manual", connection = defaultConnection) {
    const action = { authType, connection };
    setPendingAction(action);
    if (integration.connected) {
      setView("instance");
    } else if (authType === "manual") {
      setView("token");
    } else {
      onStartOAuth(undefined, action.connection);
    }
  }

  function handleInstanceSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const name = (new FormData(e.currentTarget).get("instance_name") as string)?.trim();
    if (!name) return;
    const action = pendingAction ? { ...pendingAction, instance: name } : undefined;
    setPendingAction(action);
    if (action?.authType === "manual") {
      setView("token");
    } else {
      onStartOAuth(action?.instance, action?.connection);
    }
  }

  function resolveCredentialFields(): CredentialFieldDef[] | undefined {
    const target = pendingAction?.connection
      ? authTargets.find((authTarget) => authTarget.connection === pendingAction.connection)
      : defaultTarget;
    return target?.credentialFields ?? integration.credential_fields;
  }

  function isPendingAction(action: AuthAction): boolean {
    return reconnecting
      && pendingAction?.authType === action.authType
      && pendingAction?.connection === action.connection;
  }

  function handleTokenSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const fields = resolveCredentialFields();

    if (!fields?.length) return;

    let credential: string | Record<string, string>;
    if (fields.length === 1) {
      const val = (fd.get(`cred_${fields[0].name}`) as string)?.trim();
      if (!val) return;
      credential = val;
    } else {
      const creds: Record<string, string> = {};
      for (const field of fields) {
        const val = (fd.get(`cred_${field.name}`) as string)?.trim();
        if (!val) return;
        creds[field.name] = val;
      }
      credential = creds;
    }

    let params: Record<string, string> | undefined;
    if (integration.connection_params) {
      const collected: Record<string, string> = {};
      for (const name of Object.keys(integration.connection_params)) {
        const val = (fd.get(`cp_${name}`) as string)?.trim();
        if (val) collected[name] = val;
      }
      if (Object.keys(collected).length > 0) params = collected;
    }
    onSubmitToken(credential, params, pendingAction?.instance, pendingAction?.connection);
  }

  function renderConnectionButtons() {
    return (
      <div className="mt-6 flex flex-col gap-2">
        {connectionActions.map((action) => (
          <Button
            key={action.key}
            variant={action.variant}
            className="w-full"
            onClick={() => startAddConnection(action.authType, action.connection)}
            disabled={reconnecting || submitting}
          >
            {isPendingAction(action) ? "Connecting..." : action.label}
          </Button>
        ))}
      </div>
    );
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
        {view === "disconnect" ? (
          <>
            <h2
              id={headingId}
              className="text-lg font-heading font-semibold text-stone-900 dark:text-stone-100"
            >
              Disconnect {displayName}?
            </h2>
            <p className="mt-2 text-sm text-stone-500 dark:text-stone-400">
              This will remove your {displayName} integration. You can reconnect
              at any time.
            </p>
            {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}
            <div className="mt-6 flex gap-3">
              <Button
                variant="secondary"
                className="flex-1"
                onClick={() => {
                  setView("default");
                  setDisconnectTarget({});
                }}
                disabled={disconnecting}
              >
                Cancel
              </Button>
              <Button
                variant="danger"
                className="flex-1"
                onClick={() => onDisconnect(disconnectTarget.instance, disconnectTarget.connection)}
                disabled={disconnecting}
              >
                {disconnecting ? "Disconnecting..." : "Disconnect"}
              </Button>
            </div>
          </>
        ) : view === "instance" ? (
          <form onSubmit={handleInstanceSubmit}>
            <h2
              id={headingId}
              className="text-lg font-heading font-semibold text-stone-900 dark:text-stone-100"
            >
              Add Connection
            </h2>
            {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}
            <label
              htmlFor={`instance-name-${integration.name}`}
              className="mt-4 block text-sm font-medium text-stone-700 dark:text-stone-300"
            >
              Connection name
            </label>
            <input
              id={`instance-name-${integration.name}`}
              name="instance_name"
              type="text"
              required
              placeholder="e.g. my-store, acme-workspace"
              autoFocus
              className="mt-1 w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25 dark:text-stone-100 dark:placeholder:text-stone-500 dark:focus:border-timber-500 dark:focus:ring-timber-500/25"
            />
            <div className="mt-6 flex gap-3">
              <Button
                type="button"
                variant="secondary"
                className="flex-1"
                onClick={() => setView("default")}
              >
                Cancel
              </Button>
              <Button type="submit" className="flex-1">
                Continue
              </Button>
            </div>
          </form>
        ) : view === "token" ? (
          <TokenForm
            integrationName={integration.name}
            headingId={headingId}
            credentialFields={resolveCredentialFields()}
            connectionParams={needsParams ? integration.connection_params : undefined}
            error={error}
            submitting={submitting}
            onSubmit={handleTokenSubmit}
            onCancel={() => setView(integration.connected ? "instance" : "default")}
          />
        ) : (
          <>
            <div className="flex items-start justify-between">
              <h2
                id={headingId}
                className="text-lg font-heading font-semibold text-stone-900 dark:text-stone-100"
              >
                {displayName}
              </h2>
              <button
                onClick={closeDialog}
                className="rounded p-1 text-stone-400 hover:text-stone-600 transition-colors dark:text-stone-500 dark:hover:text-stone-300"
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
                          <div>
                            <div className="text-sm text-stone-700 dark:text-stone-300">{inst.name}</div>
                            {inst.connection && (
                              <div className="text-xs text-stone-500 dark:text-stone-400">{inst.connection}</div>
                            )}
                          </div>
                        </div>
                        <button
                          onClick={() => {
                            setDisconnectTarget({ instance: inst.name, connection: inst.connection });
                            setView("disconnect");
                          }}
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
                {renderConnectionButtons()}
              </>
            ) : (
              <>
                <p className="mt-4 text-sm text-stone-500 dark:text-stone-400">Not connected</p>
                {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}
                {renderConnectionButtons()}
              </>
            )}
          </>
        )}
      </div>
    </dialog>
  );
}

const LINK_RE = /(\[[^\]]+\]\(https?:\/\/[^)]+\))/;
const LINK_MATCH_RE = /^\[([^\]]+)\]\((https?:\/\/[^)]+)\)$/;

function renderLinkedText(text: string): (string | JSX.Element)[] {
  return text.split(LINK_RE).map((seg, i) => {
    const m = seg.match(LINK_MATCH_RE);
    if (!m) return seg;
    return <a key={i} href={m[2]} target="_blank" rel="noopener noreferrer" className="text-timber-500 hover:underline dark:text-timber-400">{m[1]}</a>;
  });
}

function TokenForm({
  integrationName,
  headingId,
  credentialFields,
  connectionParams,
  error,
  submitting,
  onSubmit,
  onCancel,
}: {
  integrationName: string;
  headingId: string;
  credentialFields: CredentialFieldDef[] | undefined;
  connectionParams: Record<string, ConnectionParamDef> | undefined;
  error: string | null;
  submitting: boolean;
  onSubmit: (e: React.FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
}) {
  const fields = credentialFields ?? [];
  const heading = fields.length === 1 ? (fields[0].label || fields[0].name) : "Enter Credentials";

  return (
    <form onSubmit={onSubmit}>
      <h2
        id={headingId}
        className="text-lg font-heading font-semibold text-stone-900 dark:text-stone-100"
      >
        {heading}
      </h2>
      {connectionParams && Object.entries(connectionParams).map(([name, def]) => (
        <div key={name} className="mt-2">
          <label
            htmlFor={`cp_${name}-${integrationName}`}
            className="block text-sm font-medium text-stone-700 dark:text-stone-300"
          >
            {def.description || name}
          </label>
          <input
            id={`cp_${name}-${integrationName}`}
            name={`cp_${name}`}
            type="text"
            required={def.required}
            defaultValue={def.default}
            placeholder={name}
            className="mt-1 w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25 dark:text-stone-100 dark:placeholder:text-stone-500 dark:focus:border-timber-500 dark:focus:ring-timber-500/25"
          />
        </div>
      ))}
      {fields.map((field, idx) => (
        <div key={field.name} className="mt-4">
          <label
            htmlFor={`cred_${field.name}-${integrationName}`}
            className="block text-sm font-medium text-stone-700 dark:text-stone-300"
          >
            {field.label || field.name}
            {field.help_url && (
              <a
                href={field.help_url}
                target="_blank"
                rel="noopener noreferrer"
                className="ml-1 text-xs text-timber-500 hover:underline dark:text-timber-400"
              >
                (where to find this)
              </a>
            )}
          </label>
          {field.description && (
            <p className="mt-0.5 text-xs text-stone-400 dark:text-stone-500">{renderLinkedText(field.description)}</p>
          )}
          <input
            id={`cred_${field.name}-${integrationName}`}
            name={`cred_${field.name}`}
            type="password"
            required
            placeholder={field.label || field.name}
            autoFocus={idx === 0}
            className="mt-1 w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25 dark:text-stone-100 dark:placeholder:text-stone-500 dark:focus:border-timber-500 dark:focus:ring-timber-500/25"
          />
        </div>
      ))}
      {error && <p className="mt-3 text-sm text-ember-500">{error}</p>}
      <div className="mt-6 flex gap-3">
        <Button
          type="button"
          variant="secondary"
          className="flex-1"
          onClick={onCancel}
          disabled={submitting}
        >
          Cancel
        </Button>
        <Button type="submit" className="flex-1" disabled={submitting}>
          {submitting ? "Connecting..." : "Submit"}
        </Button>
      </div>
    </form>
  );
}
