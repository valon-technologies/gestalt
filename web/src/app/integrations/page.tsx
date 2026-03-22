"use client";

import { Suspense, useEffect, useState, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { getIntegrations, Integration } from "@/lib/api";
import Nav from "@/components/Nav";
import IntegrationCard from "@/components/IntegrationCard";
import AuthGuard from "@/components/AuthGuard";

function IntegrationsContent() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    const connected = searchParams.get("connected");
    if (connected) {
      setToast(`${connected} connected successfully.`);
      router.replace("/integrations");
    }
  }, [searchParams, router]);

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
    <div className="min-h-screen">
      <Nav />
      <main className="mx-auto max-w-5xl px-6 py-8">
        {toast && (
          <div className="mb-6 flex items-center justify-between rounded-lg border border-grove-200 bg-grove-50 px-4 py-3 text-sm text-grove-700">
            <span>{toast}</span>
            <button
              onClick={() => setToast(null)}
              className="ml-4 text-grove-400 hover:text-grove-600"
              aria-label="Dismiss"
            >
              &times;
            </button>
          </div>
        )}

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
                onConnected={loadIntegrations}
                onDisconnected={loadIntegrations}
              />
            ))}
          </div>
        )}
      </main>
    </div>
  );
}

export default function IntegrationsPage() {
  return (
    <AuthGuard>
      <Suspense fallback={<div className="min-h-screen"><Nav /><main className="mx-auto max-w-5xl px-6 py-8"><p className="text-sm text-stone-400">Loading...</p></main></div>}>
        <IntegrationsContent />
      </Suspense>
    </AuthGuard>
  );
}
