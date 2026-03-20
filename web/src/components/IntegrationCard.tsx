"use client";

import { Integration, startIntegrationOAuth } from "@/lib/api";
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
}: {
  integration: Integration;
}) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const safeIconSVG = useMemo(
    () => (integration.icon_svg ? sanitizeSVG(integration.icon_svg) : ""),
    [integration.icon_svg],
  );

  async function handleConnect() {
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
              <p className="mt-1 text-sm text-stone-500">
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
      {!integration.connected && (
        <div className="mt-4">
          <Button onClick={handleConnect} disabled={loading}>
            {loading ? "Connecting..." : "Connect"}
          </Button>
        </div>
      )}
    </div>
  );
}
