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
      <div className="page-shell">
        <Nav />
        <main className="page-main">
          {toast && (
            <div className="mb-8 flex items-center justify-between rounded-lg border border-grove-200 bg-grove-50 px-5 py-4 text-sm text-grove-700 dark:border-grove-600 dark:bg-grove-700/20 dark:text-grove-200">
              <span>{toast}</span>
              <button
                onClick={() => setToast(null)}
                className="ml-4 text-grove-400 hover:text-grove-600 dark:text-grove-500 dark:hover:text-grove-200 transition-colors duration-150"
                aria-label="Dismiss"
              >
                &times;
              </button>
            </div>
          )}

          <div className="page-hero animate-fade-in-up">
            <span className="label-text">Catalog</span>
            <h1 className="page-title mt-4">Integrations</h1>
            <p className="page-subtitle mt-4">
              Browse and connect third-party services with a warmer, quieter
              surface that lets the provider states do the talking.
            </p>
          </div>

          {loading && (
            <p className="mt-10 text-sm text-faint">Loading integrations...</p>
          )}

          {error && <p className="mt-10 text-sm text-ember-500">{error}</p>}

          {!loading && !error && integrations.length === 0 && (
            <div className="surface-card mt-10 p-6 text-sm text-muted">
              No integrations registered.
            </div>
          )}

          {!loading && !error && integrations.length > 0 && (
            <div className="mt-10 grid grid-cols-1 gap-6 md:grid-cols-2 xl:grid-cols-3 animate-fade-in-up [animation-delay:60ms]">
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
