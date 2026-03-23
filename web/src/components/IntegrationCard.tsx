"use client";

import { useState } from "react";
import {
  Integration,
  startIntegrationOAuth,
  connectManualIntegration,
  disconnectIntegration,
} from "@/lib/api";
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
  const [showTokenForm, setShowTokenForm] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const safeIconSVG = integration.icon_svg
    ? sanitizeSVG(integration.icon_svg)
    : "";
  const isManual = integration.auth_type === "manual";

  async function handleConnect() {
    if (isManual) {
      setSettingsOpen(false);
      setShowTokenForm(true);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const { url } = await startIntegrationOAuth(integration.name);
      window.location.href = url;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start OAuth");
    } finally {
      setLoading(false);
    }
  }

  async function handleSubmitManual(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const credential = (
      new FormData(e.currentTarget).get("credential") as string
    )?.trim();
    if (!credential) return;
    setSubmitting(true);
    setError(null);
    try {
      await connectManualIntegration(integration.name, credential);
      setShowTokenForm(false);
      onConnected?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to connect");
    } finally {
      setSubmitting(false);
    }
  }

  function handleCancelManual() {
    setShowTokenForm(false);
    setError(null);
  }

  async function handleDisconnect() {
    setDisconnecting(true);
    setError(null);
    try {
      await disconnectIntegration(integration.name);
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

  return (
    <div className="rounded-lg border border-border bg-surface p-5 shadow-warm">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-stone-100 text-stone-500 [&>svg]:h-5 [&>svg]:w-5">
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
            <h3 className="text-base font-heading font-semibold text-stone-900">
              {integration.display_name || integration.name}
            </h3>
            {integration.description && (
              <p className="mt-1 line-clamp-2 text-sm text-stone-500">
                {integration.description}
              </p>
            )}
          </div>
        </div>
        {integration.connected && (
          <div className="flex items-center gap-1">
            <CheckCircleIcon className="h-5 w-5 text-grove-500" />
            <button
              onClick={() => setSettingsOpen(true)}
              className="flex h-8 w-8 items-center justify-center rounded text-stone-400 transition-colors hover:bg-stone-100 hover:text-stone-600"
              aria-label={`${integration.display_name || integration.name} settings`}
            >
              <GearIcon className="h-4 w-4" />
            </button>
          </div>
        )}
      </div>
      {error && !settingsOpen && (
        <p className="mt-2 text-sm text-ember-500">{error}</p>
      )}
      {showTokenForm && (
        <form onSubmit={handleSubmitManual} className="mt-3">
          <label
            htmlFor={`credential-${integration.name}`}
            className="block text-sm font-medium text-stone-700"
          >
            API Token
          </label>
          <input
            id={`credential-${integration.name}`}
            name="credential"
            type="password"
            required
            placeholder="Paste your API token"
            autoFocus
            className="mt-1 w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25"
          />
          <div className="mt-2 flex gap-2">
            <Button type="submit" disabled={submitting}>
              {submitting ? "Connecting..." : "Submit"}
            </Button>
            <Button
              type="button"
              variant="secondary"
              onClick={handleCancelManual}
              disabled={submitting}
            >
              Cancel
            </Button>
          </div>
        </form>
      )}
      {!showTokenForm && !integration.connected && (
        <div className="mt-4">
          <Button onClick={handleConnect} disabled={loading}>
            {loading ? "Connecting..." : "Connect"}
          </Button>
        </div>
      )}
      {settingsOpen && (
        <IntegrationSettingsModal
          integration={integration}
          onClose={handleSettingsClose}
          onReconnect={handleConnect}
          onDisconnect={handleDisconnect}
          reconnecting={loading}
          disconnecting={disconnecting}
          error={error}
        />
      )}
    </div>
  );
}
