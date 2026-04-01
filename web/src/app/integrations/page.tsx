"use client";

import { useEffect, useState } from "react";
import { getIntegrations, Integration } from "@/lib/api";
import Nav from "@/components/Nav";
import IntegrationCard from "@/components/IntegrationCard";
import AuthGuard from "@/components/AuthGuard";

export default function IntegrationsPage() {
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    const connected = new URLSearchParams(window.location.search).get("connected");
    if (connected) {
      setToast(`${connected} connected successfully.`);
      window.history.replaceState(null, "", "/integrations");
    }
  }, []);

  function loadIntegrations() {
    getIntegrations()
      .then(setIntegrations)
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load");
      })
      .finally(() => setLoading(false));
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { loadIntegrations(); }, []);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-8">
          {toast && (
            <div className="mb-6 flex items-center justify-between rounded-lg border border-grove-200 bg-grove-50 px-4 py-3 text-sm text-grove-700 dark:border-grove-600 dark:bg-grove-700/20 dark:text-grove-200">
              <span>{toast}</span>
              <button
                onClick={() => setToast(null)}
                className="ml-4 text-grove-400 hover:text-grove-600 dark:text-grove-500 dark:hover:text-grove-200"
                aria-label="Dismiss"
              >
                &times;
              </button>
            </div>
          )}

          <h1 className="text-2xl font-heading font-bold text-stone-900 dark:text-stone-100">
            Integrations
          </h1>
          <p className="mt-1 text-sm text-stone-500 dark:text-stone-400">
            Browse and connect third-party services.
          </p>

          {loading && (
            <p className="mt-8 text-sm text-stone-400">Loading...</p>
          )}

          {error && <p className="mt-8 text-sm text-ember-500">{error}</p>}

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
                  onConnected={loadIntegrations}
                  onDisconnected={loadIntegrations}
                />
              ))}
            </div>
          )}
        </main>
      </div>
    </AuthGuard>
  );
}
