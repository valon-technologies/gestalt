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
  }>({
    integrations: null,
    tokens: null,
    error: null,
  });

  useEffect(() => {
    let active = true;

    Promise.allSettled([getIntegrations(), getTokens()]).then(
      ([integrationsResult, tokensResult]) => {
        if (!active) return;

        const error =
          integrationsResult.status === "rejected"
            ? errorMessage(integrationsResult.reason)
            : tokensResult.status === "rejected"
              ? errorMessage(tokensResult.reason)
              : null;

        setData({
          integrations:
            integrationsResult.status === "fulfilled"
              ? integrationsResult.value.length
              : null,
          tokens:
            tokensResult.status === "fulfilled"
              ? tokensResult.value.length
              : null,
          error,
        });
      },
    );

    return () => {
      active = false;
    };
  }, []);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-12">
          <div className="animate-fade-in-up">
            <span className="label-text">Overview</span>
            <h1 className="mt-2 text-2xl font-heading font-bold text-primary">
              Dashboard
            </h1>
            <p className="mt-2 text-sm text-muted">
              Manage the client-facing integration workspace from one place.
            </p>
          </div>

          {data.error && (
            <p className="mt-8 text-sm text-ember-500">{data.error}</p>
          )}

          <div className="mt-10 grid grid-cols-1 gap-5 sm:grid-cols-2 animate-fade-in-up [animation-delay:60ms]">
            <Link
              href="/integrations"
              className="group rounded-lg border border-alpha bg-base-100 p-8 transition-all duration-150 hover:border-alpha-strong hover:shadow-card dark:bg-surface"
            >
              <span className="label-text">Integrations</span>
              <p className="mt-3 text-3xl font-heading font-bold text-primary">
                {data.integrations ?? "--"}
              </p>
              <p className="mt-3 text-sm text-muted group-hover:text-primary transition-colors duration-150">
                Manage integrations
                <span className="inline-block ml-1 transition-transform duration-150 group-hover:translate-x-0.5">
                  &rarr;
                </span>
              </p>
            </Link>
            <Link
              href="/tokens"
              className="group rounded-lg border border-alpha bg-base-100 p-8 transition-all duration-150 hover:border-alpha-strong hover:shadow-card dark:bg-surface"
            >
              <span className="label-text">API Tokens</span>
              <p className="mt-3 text-3xl font-heading font-bold text-primary">
                {data.tokens ?? "--"}
              </p>
              <p className="mt-3 text-sm text-muted group-hover:text-primary transition-colors duration-150">
                Manage tokens
                <span className="inline-block ml-1 transition-transform duration-150 group-hover:translate-x-0.5">
                  &rarr;
                </span>
              </p>
            </Link>
          </div>
        </main>
      </div>
    </AuthGuard>
  );
}

function errorMessage(reason: unknown): string {
  if (reason instanceof Error) {
    return reason.message;
  }
  if (typeof reason === "string") {
    return reason;
  }
  return "Failed to load";
}
