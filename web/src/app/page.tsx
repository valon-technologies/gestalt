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
      <div className="page-shell">
        <Nav />
        <main className="page-main">
          <div className="page-hero animate-fade-in-up">
            <span className="label-text">Overview</span>
            <h1 className="page-title mt-4">Dashboard</h1>
            <p className="page-subtitle mt-4">
              A warm, readable control surface for integrations, tokens, and
              the operational details that matter most.
            </p>
          </div>

          {data.error && (
            <p className="mt-8 text-sm text-ember-500">{data.error}</p>
          )}

          <div className="mt-8 grid grid-cols-1 gap-6 md:mt-10 md:grid-cols-2 animate-fade-in-up [animation-delay:60ms]">
            <Link
              href="/integrations"
              className="surface-card group p-6 md:p-8"
            >
              <span className="label-text">Integrations</span>
              <p className="metric-value mt-5">
                {data.integrations ?? "--"}
              </p>
              <p className="mt-5 max-w-xs text-sm leading-6 text-muted group-hover:text-secondary">
                Browse providers, establish new connections, and keep the
                catalog in good shape.
              </p>
              <p className="mt-6 inline-flex items-center gap-2 text-sm font-bold text-primary">
                Manage integrations
                <span className="inline-block transition-transform duration-150 group-hover:translate-x-0.5">
                  &rarr;
                </span>
              </p>
            </Link>
            <Link
              href="/tokens"
              className="surface-card group p-6 md:p-8"
            >
              <span className="label-text">API Tokens</span>
              <p className="metric-value mt-5">
                {data.tokens ?? "--"}
              </p>
              <p className="mt-5 max-w-xs text-sm leading-6 text-muted group-hover:text-secondary">
                Create, rotate, and retire credentials without losing visual
                clarity in the process.
              </p>
              <p className="mt-6 inline-flex items-center gap-2 text-sm font-bold text-primary">
                Manage tokens
                <span className="inline-block transition-transform duration-150 group-hover:translate-x-0.5">
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
