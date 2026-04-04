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

          <section className="mt-10 rounded-lg border border-alpha bg-base-100 p-8 shadow-card dark:bg-surface animate-fade-in-up [animation-delay:120ms]">
            <span className="label-text">Admin</span>
            <h2 className="mt-2 text-xl font-heading font-bold text-primary">
              Metrics moved to the admin UI
            </h2>
            <p className="mt-3 max-w-2xl text-sm text-muted">
              Prometheus metrics now live at
              <span className="font-mono text-primary"> /admin</span> so this
              client workspace stays focused on integrations and tokens.
            </p>
            <div className="mt-5">
              <button
                type="button"
                onClick={(event) => {
                  event.preventDefault();
                  window.location.assign("/admin/");
                }}
                className="inline-flex items-center gap-2 rounded-md border border-alpha px-4 py-2 text-sm font-medium text-primary transition-colors duration-150 hover:border-alpha-strong hover:bg-alpha-5"
              >
                Open admin UI
                <span aria-hidden="true">&rarr;</span>
              </button>
            </div>
          </section>
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
