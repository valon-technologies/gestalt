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
  const [toast, setToast] = useState<string | null>(() => {
    if (typeof window === "undefined") {
      return null;
    }
    const connected = new URLSearchParams(window.location.search).get("connected");
    return connected ? `${connected} connected successfully.` : null;
  });

  useEffect(() => {
    if (toast) {
      window.history.replaceState(null, "", "/integrations");
    }
  }, [toast]);

  function loadIntegrations() {
    getIntegrations()
      .then(setIntegrations)
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load");
      })
      .finally(() => setLoading(false));
  }

  useEffect(() => { loadIntegrations(); }, []);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-12">
          {toast && (
            <div className="mb-8 flex items-center justify-between rounded-lg border border-grove-200 bg-grove-50 px-5 py-3.5 text-sm text-grove-700 dark:border-grove-600 dark:bg-grove-700/20 dark:text-grove-200">
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

          <div className="animate-fade-in-up">
            <span className="label-text">Catalog</span>
            <h1 className="mt-2 text-2xl font-heading font-bold text-primary">
              Integrations
            </h1>
            <p className="mt-2 text-sm text-muted">
              Browse and connect third-party services.
            </p>
          </div>

          {loading && (
            <p className="mt-10 text-sm text-faint">Loading...</p>
          )}

          {error && <p className="mt-10 text-sm text-ember-500">{error}</p>}

          {!loading && !error && integrations.length === 0 && (
            <p className="mt-10 text-sm text-faint">
              No integrations registered.
            </p>
          )}

          {!loading && !error && integrations.length > 0 && (
            <div className="mt-10 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 animate-fade-in-up [animation-delay:60ms]">
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
