"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { getIntegrations, getTokens } from "@/lib/api";
import Nav from "@/components/Nav";
import AuthGuard from "@/components/AuthGuard";

export default function DashboardPage() {
  const [data, setData] = useState<{
    integrations: number | null;
    tokens: number | null;
    error: string | null;
  }>({ integrations: null, tokens: null, error: null });

  useEffect(() => {
    Promise.all([getIntegrations(), getTokens()])
      .then(([integrations, tokens]) => {
        setData({
          integrations: integrations.length,
          tokens: tokens.length,
          error: null,
        });
      })
      .catch((err) => {
        setData((prev) => ({
          ...prev,
          error: err instanceof Error ? err.message : "Failed to load",
        }));
      });
  }, []);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-8">
          <h1 className="text-2xl font-heading font-bold text-stone-900">
            Dashboard
          </h1>
          <p className="mt-1 text-sm text-stone-500">
            Your Gestalt overview at a glance.
          </p>

          {data.error && (
            <p className="mt-8 text-sm text-ember-500">{data.error}</p>
          )}

          <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Link
              href="/integrations"
              className="rounded-lg border border-border bg-surface p-6 shadow-warm transition-all hover:shadow-md hover:border-timber-300"
            >
              <p className="text-sm font-medium text-stone-500">
                Integrations
              </p>
              <p className="mt-2 text-3xl font-heading font-bold text-stone-900">
                {data.integrations ?? "--"}
              </p>
              <p className="mt-1 text-sm font-medium text-timber-600">
                Manage integrations &rarr;
              </p>
            </Link>
            <Link
              href="/tokens"
              className="rounded-lg border border-border bg-surface p-6 shadow-warm transition-all hover:shadow-md hover:border-timber-300"
            >
              <p className="text-sm font-medium text-stone-500">API Tokens</p>
              <p className="mt-2 text-3xl font-heading font-bold text-stone-900">
                {data.tokens ?? "--"}
              </p>
              <p className="mt-1 text-sm font-medium text-timber-600">
                Manage tokens &rarr;
              </p>
            </Link>
          </div>
        </main>
      </div>
    </AuthGuard>
  );
}
