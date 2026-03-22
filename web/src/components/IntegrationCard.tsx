"use client";

import { Integration, startIntegrationOAuth, connectManualIntegration, disconnectIntegration } from "@/lib/api";
import Button from "./Button";
import { useMemo, useState } from "react";

const DANGEROUS_ELEMENTS = [
  "script", "foreignObject", "iframe", "embed", "object",
  "style", "animate", "set",
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

function DefaultIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="3" width="7" height="7" rx="1" />
      <rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" />
      <rect x="14" y="14" width="7" height="7" rx="1" />
    </svg>
  );
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
  const [showTokenForm, setShowTokenForm] = useState(false);
  const [credential, setCredential] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const safeIconSVG = useMemo(
    () => (integration.icon_svg ? sanitizeSVG(integration.icon_svg) : ""),
    [integration.icon_svg],
  );

  const isManual = integration.auth_type === "manual";

  async function handleConnect() {
    if (isManual) {
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

  async function handleSubmitManual(e: React.FormEvent) {
    e.preventDefault();
    if (!credential.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      await connectManualIntegration(integration.name, credential.trim());
      setShowTokenForm(false);
      setCredential("");
      onConnected?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to connect");
    } finally {
      setSubmitting(false);
    }
  }

  function handleCancelManual() {
    setShowTokenForm(false);
    setCredential("");
    setError(null);
  }

  async function handleDisconnect() {
    setDisconnecting(true);
    setError(null);
    try {
      await disconnectIntegration(integration.name);
      onDisconnected?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to disconnect");
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-5 shadow-warm">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-stone-100 text-stone-500 [&>svg]:h-5 [&>svg]:w-5">
            {safeIconSVG ? (
              <div dangerouslySetInnerHTML={{ __html: safeIconSVG }} className="flex items-center justify-center [&>svg]:h-5 [&>svg]:w-5" />
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
          <span className="inline-block rounded-full border border-grove-200 bg-grove-50 px-2 py-0.5 text-xs font-medium text-grove-600">
            Connected
          </span>
        )}
      </div>
      {error && <p className="mt-2 text-sm text-ember-500">{error}</p>}
      {showTokenForm && (
        <form onSubmit={handleSubmitManual} className="mt-3">
          <label htmlFor={`credential-${integration.name}`} className="block text-sm font-medium text-stone-700">
            API Token
          </label>
          <input
            id={`credential-${integration.name}`}
            type="password"
            value={credential}
            onChange={(e) => setCredential(e.target.value)}
            placeholder="Paste your API token"
            autoFocus
            className="mt-1 w-full rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25"
          />
          <div className="mt-2 flex gap-2">
            <Button type="submit" disabled={submitting || !credential.trim()}>
              {submitting ? "Connecting..." : "Submit"}
            </Button>
            <Button type="button" variant="secondary" onClick={handleCancelManual} disabled={submitting}>
              Cancel
            </Button>
          </div>
        </form>
      )}
      {!showTokenForm && (
        integration.connected ? (
          <div className="mt-4 flex gap-2">
            <Button onClick={handleConnect} disabled={loading}>
              {loading ? "Connecting..." : "Reconnect"}
            </Button>
            <Button variant="danger" onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting ? "Disconnecting..." : "Disconnect"}
            </Button>
          </div>
        ) : (
          <div className="mt-4">
            <Button onClick={handleConnect} disabled={loading}>
              {loading ? "Connecting..." : "Connect"}
            </Button>
          </div>
        )
      )}
    </div>
  );
}
