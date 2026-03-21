"use client";

import { useEffect, useState, useCallback } from "react";
import { getTokens, APIToken } from "@/lib/api";
import Nav from "@/components/Nav";
import TokenTable from "@/components/TokenTable";
import TokenCreateForm from "@/components/TokenCreateForm";
import AuthGuard from "@/components/AuthGuard";

export default function TokensPage() {
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadTokens = useCallback(async () => {
    try {
      const data = await getTokens();
      setTokens(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load tokens");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadTokens();
  }, [loadTokens]);

  return (
    <AuthGuard>
      <div className="min-h-screen">
        <Nav />
        <main className="mx-auto max-w-5xl px-6 py-8">
          <h1 className="text-2xl font-heading font-bold text-stone-900">API Tokens</h1>
          <p className="mt-1 text-sm text-stone-500">
            Manage tokens for programmatic access to the Toolshed API.
          </p>

          <TokenCreateForm onCreated={loadTokens} />

          {error && <p className="mt-4 text-sm text-ember-500">{error}</p>}

          {loading ? (
            <p className="mt-8 text-sm text-stone-400">Loading...</p>
          ) : !error ? (
            <div className="mt-6">
              <TokenTable tokens={tokens} onRevoked={loadTokens} />
            </div>
          ) : null}
        </main>
      </div>
    </AuthGuard>
  );
}
