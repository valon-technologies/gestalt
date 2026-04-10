"use client";

import { useEffect, useRef, useState } from "react";
import {
  Integration,
  PENDING_CONNECTION_PATH,
  resolveAPIPath,
  startIntegrationOAuth,
  connectManualIntegration,
  disconnectIntegration,
} from "@/lib/api";
import { INPUT_CLASSES } from "@/lib/constants";
import Button from "./Button";
import { CheckCircleIcon, GearIcon, DefaultIcon } from "./icons";
import IntegrationSettingsModal from "./IntegrationSettingsModal";

const DANGEROUS_ELEMENTS = [
  "script",
  "foreignObject",
  "iframe",
  "embed",
  "object",
  "style",
  "animate",
  "set",
];

function stripDangerousAttrs(el: Element) {
  for (const { name, value } of Array.from(el.attributes)) {
    if (name.startsWith("on")) {
      el.removeAttribute(name);
    } else if (
      (name === "href" || name === "xlink:href") &&
      value.replace(/\s/g, "").toLowerCase().startsWith("javascript:")
    ) {
      el.removeAttribute(name);
    }
  }
}

function sanitizeSVG(raw: string): string {
  const doc = new DOMParser().parseFromString(raw, "image/svg+xml");
  const svg = doc.documentElement;
  if (svg.nodeName !== "svg") return "";
  for (const tag of DANGEROUS_ELEMENTS) {
    svg.querySelectorAll(tag).forEach((el) => el.remove());
  }
  stripDangerousAttrs(svg);
  svg.querySelectorAll("*").forEach((el) => stripDangerousAttrs(el));
  return svg.outerHTML;
}

function hasConnectionParams(integration: Integration): boolean {
  return (
    !!integration.connectionParams &&
    Object.keys(integration.connectionParams).length > 0
  );
}

type ConnectionTarget = {
  instance?: string;
  connection?: string;
};

type PendingSelection = {
  action: string;
  pendingToken: string;
};

export default function IntegrationCard({
  integration,
  onConnected,
  onDisconnected,
}: {
  integration: Integration;
  onConnected?: () => void;
  onDisconnected?: () => void;
}) {
  const [loading, setLoading] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [showParamForm, setShowParamForm] = useState(false);
  const [pendingOAuthTarget, setPendingOAuthTarget] = useState<ConnectionTarget>({});
  const [pendingSelection, setPendingSelection] = useState<PendingSelection | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const pendingSelectionFormRef = useRef<HTMLFormElement>(null);

  const safeIconSVG = integration.iconSvg
    ? sanitizeSVG(integration.iconSvg)
    : "";
  const needsParams = hasConnectionParams(integration);

  useEffect(() => {
    if (!pendingSelection) return;
    pendingSelectionFormRef.current?.submit();
  }, [pendingSelection]);

  function collectConnectionParams(
    form: HTMLFormElement,
  ): Record<string, string> {
    const params: Record<string, string> = {};
    if (!integration.connectionParams) return params;
    for (const name of Object.keys(integration.connectionParams)) {
      const val = (new FormData(form).get(`cp_${name}`) as string)?.trim();
      if (val) params[name] = val;
    }
    return params;
  }

  async function beginOAuth(connectionParams?: Record<string, string>, target: ConnectionTarget = pendingOAuthTarget) {
    setLoading(true);
    setError(null);
    try {
      const { url } = await startIntegrationOAuth(
        integration.name,
        undefined,
        connectionParams,
        target.instance,
        target.connection,
      );
      window.location.href = url;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start OAuth");
      setLoading(false);
    }
  }

  async function handleStartOAuth(instance?: string, connection?: string) {
    const target = { instance, connection };
    setPendingOAuthTarget(target);
    if (needsParams && !showParamForm) {
      setSettingsOpen(false);
      setShowParamForm(true);
      setError(null);
      return;
    }
    await beginOAuth(undefined, target);
  }

  async function handleSubmitToken(credential: string | Record<string, string>, connectionParams?: Record<string, string>, instance?: string, connection?: string) {
    setSubmitting(true);
    setError(null);
    try {
      const result = await connectManualIntegration(
        integration.name, credential, connectionParams, instance, connection,
      );
      if (result.status === "selection_required") {
        if (!result.pendingToken) {
          throw new Error("Connection requires selection, but the server did not return a pending token.");
        }
        setSettingsOpen(false);
        setPendingSelection({
          action: resolveAPIPath(result.selectionUrl || PENDING_CONNECTION_PATH),
          pendingToken: result.pendingToken,
        });
      } else {
        setSettingsOpen(false);
        onConnected?.();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to connect");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleParamSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const params = collectConnectionParams(e.currentTarget);
    await beginOAuth(params);
  }

  function handleCancelForm() {
    setShowParamForm(false);
    setPendingOAuthTarget({});
    setError(null);
  }

  async function handleDisconnect(instance?: string, connection?: string) {
    setDisconnecting(true);
    setError(null);
    try {
      await disconnectIntegration(integration.name, instance, connection);
      onDisconnected?.();
      setSettingsOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to disconnect");
    } finally {
      setDisconnecting(false);
    }
  }

  function handleSettingsClose() {
    setSettingsOpen(false);
    setError(null);
  }

  function renderConnectionParamFields() {
    if (!integration.connectionParams) return null;
    return Object.entries(integration.connectionParams).map(([name, def]) => (
      <div key={name} className="mt-3">
        <label
          htmlFor={`cp_${name}-${integration.name}`}
          className="label-text block"
        >
          {def.description || name}
        </label>
        <input
          id={`cp_${name}-${integration.name}`}
          name={`cp_${name}`}
          type="text"
          required={def.required}
          defaultValue={def.default}
          placeholder={name}
          className={`mt-1.5 w-full ${INPUT_CLASSES}`}
        />
      </div>
    ));
  }

  return (
    <div className="rounded-lg border border-alpha bg-base-white p-6 transition-all duration-150 hover:border-alpha-strong hover:shadow-card dark:bg-surface">
      {pendingSelection && (
        <form
          ref={pendingSelectionFormRef}
          method="post"
          action={pendingSelection.action}
          className="hidden"
        >
          <input
            type="hidden"
            name="pending_token"
            value={pendingSelection.pendingToken}
          />
        </form>
      )}
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-base-100 text-muted [&>svg]:h-5 [&>svg]:w-5 dark:bg-surface-raised">
            {safeIconSVG ? (
              <div
                dangerouslySetInnerHTML={{ __html: safeIconSVG }}
                className="flex items-center justify-center [&>svg]:h-5 [&>svg]:w-5"
              />
            ) : (
              <DefaultIcon />
            )}
          </div>
          <div>
            <h3 className="text-base font-heading font-semibold text-primary">
              {integration.displayName || integration.name}
            </h3>
            {integration.description && (
              <p className="mt-1 line-clamp-2 text-sm text-muted">
                {integration.description}
              </p>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1">
          {integration.connected && (
            <CheckCircleIcon className="h-5 w-5 text-grove-500" />
          )}
          <button
            onClick={() => setSettingsOpen(true)}
            className="flex h-8 w-8 items-center justify-center rounded-md text-faint transition-all duration-150 hover:bg-alpha-5 hover:text-muted"
            aria-label={`${integration.displayName || integration.name} settings`}
          >
            <GearIcon className="h-4 w-4" />
          </button>
        </div>
      </div>
      {error && !settingsOpen && (
        <p className="mt-3 text-sm text-ember-500">{error}</p>
      )}
      {showParamForm && (
        <form onSubmit={handleParamSubmit} className="mt-4">
          {renderConnectionParamFields()}
          <div className="mt-4 flex gap-2">
            <Button type="submit" disabled={loading}>
              {loading ? "Connecting..." : "Connect"}
            </Button>
            <Button
              type="button"
              variant="secondary"
              onClick={handleCancelForm}
              disabled={loading}
            >
              Cancel
            </Button>
          </div>
        </form>
      )}
      {settingsOpen && (
        <IntegrationSettingsModal
          integration={integration}
          onClose={handleSettingsClose}
          onStartOAuth={handleStartOAuth}
          onSubmitToken={handleSubmitToken}
          onDisconnect={handleDisconnect}
          reconnecting={loading}
          disconnecting={disconnecting}
          submitting={submitting}
          error={error}
        />
      )}
    </div>
  );
}
