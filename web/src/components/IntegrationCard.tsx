"use client";

import { Integration, startIntegrationOAuth } from "@/lib/api";
import Button from "./Button";
import { useState } from "react";

export default function IntegrationCard({
  integration,
}: {
  integration: Integration;
}) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
