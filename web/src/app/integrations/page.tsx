"use client";

import { useEffect, useState, useCallback } from "react";
import { getIntegrations, Integration } from "@/lib/api";
import Nav from "@/components/Nav";
import IntegrationCard from "@/components/IntegrationCard";
import AuthGuard from "@/components/AuthGuard";

export default function IntegrationsPage() {
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadIntegrations = useCallback(() => {
    getIntegrations()
      .then(setIntegrations)
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load");
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    loadIntegrations();
  }, [loadIntegrations]);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-8">
          <h1 className="text-2xl font-heading font-bold text-stone-900">Integrations</h1>
          <p className="mt-1 text-sm text-stone-500">
            Browse and connect third-party services.
          </p>

          {loading && (
            <p className="mt-8 text-sm text-stone-400">Loading...</p>
          )}

          {error && (
            <p className="mt-8 text-sm text-ember-500">{error}</p>
          )}

          {!loading && !error && integrations.length === 0 && (
            <p className="mt-8 text-sm text-stone-400">
              No integrations registered.
            </p>
          )}

          {!loading && !error && integrations.length > 0 && (
            <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {integrations.map((integration) => (
                <IntegrationCard
                  key={integration.name}
                  integration={integration}
                />
              ))}
            </div>
          )}
        </main>
      </div>
    </AuthGuard>
  );
}
