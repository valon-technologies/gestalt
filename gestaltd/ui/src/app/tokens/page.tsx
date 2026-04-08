"use client";

import { useEffect, useRef, useState } from "react";
import { getTokens, APIToken } from "@/lib/api";
import Nav from "@/components/Nav";
import TokenTable from "@/components/TokenTable";
import TokenCreateForm from "@/components/TokenCreateForm";
import AuthGuard from "@/components/AuthGuard";

export default function TokensPage() {
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const loadRequestIdRef = useRef(0);

  async function loadTokens() {
    const requestID = loadRequestIdRef.current + 1;
    loadRequestIdRef.current = requestID;

    try {
      const nextTokens = await getTokens();
      if (loadRequestIdRef.current !== requestID) return;
      setTokens(nextTokens);
      setError(null);
    } catch (err) {
      if (loadRequestIdRef.current !== requestID) return;
      setError(err instanceof Error ? err.message : "Failed to load tokens");
    } finally {
      if (loadRequestIdRef.current === requestID) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void loadTokens();
  }, []);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-12">
          <div className="animate-fade-in-up">
            <span className="label-text">Security</span>
            <h1 className="mt-2 text-2xl font-heading font-bold text-primary">
              API Tokens
            </h1>
            <p className="mt-2 text-sm text-muted">
              Manage tokens for programmatic access to the Gestalt API.
            </p>
          </div>

          <div className="animate-fade-in-up [animation-delay:60ms]">
            <TokenCreateForm onCreated={loadTokens} />
          </div>

          {error && <p className="mt-4 text-sm text-ember-500">{error}</p>}

          {loading ? (
            <p className="mt-10 text-sm text-faint">Loading...</p>
          ) : !error ? (
            <div className="mt-8 animate-fade-in-up [animation-delay:120ms]">
              <TokenTable tokens={tokens} onRevoked={loadTokens} />
            </div>
          ) : null}
        </main>
      </div>
    </AuthGuard>
  );
}
