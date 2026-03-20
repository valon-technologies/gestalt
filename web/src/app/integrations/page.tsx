"use client";

import { Suspense, useEffect, useState, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { getIntegrations, Integration } from "@/lib/api";
import Nav from "@/components/Nav";
import IntegrationCard from "@/components/IntegrationCard";
import AuthGuard from "@/components/AuthGuard";

function OAuthFlashBanner() {
  const [flash, setFlash] = useState<{ type: "success" | "error"; message: string } | null>(null);
  const searchParams = useSearchParams();
  const router = useRouter();

  useEffect(() => {
    const connected = searchParams.get("connected");
    const oauthError = searchParams.get("error");
    if (connected) {
      setFlash({ type: "success", message: `${connected} connected successfully.` });
      router.replace("/integrations", { scroll: false });
    } else if (oauthError) {
      setFlash({ type: "error", message: oauthError });
      router.replace("/integrations", { scroll: false });
    }
  }, [searchParams, router]);

  if (!flash) return null;

  return (
    <div
      className={`mt-4 rounded-lg border px-4 py-3 text-sm ${
        flash.type === "success"
          ? "border-grove-200 bg-grove-50 text-grove-700"
          : "border-ember-200 bg-ember-50 text-ember-700"
      }`}
    >
      <div className="flex items-center justify-between">
        <span>{flash.message}</span>
        <button
          onClick={() => setFlash(null)}
          className="ml-4 text-current opacity-50 hover:opacity-100"
        >
          &times;
        </button>
      </div>
    </div>
  );
}

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

          <Suspense>
            <OAuthFlashBanner />
          </Suspense>

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
