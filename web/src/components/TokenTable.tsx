"use client";

import { APIToken, revokeToken } from "@/lib/api";
import Button from "./Button";
import { useState } from "react";

interface TokenTableProps {
  tokens: APIToken[];
  onRevoked: () => void;
}

export default function TokenTable({ tokens, onRevoked }: TokenTableProps) {
  const [revoking, setRevoking] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleRevoke(id: string) {
    setRevoking(id);
    setError(null);
    try {
      await revokeToken(id);
      onRevoked();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to revoke token");
    } finally {
      setRevoking(null);
    }
  }

  if (tokens.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-stone-400 dark:text-stone-500">
        No API tokens yet.
      </p>
    );
  }

  return (
    <div className="rounded-lg border border-border bg-surface overflow-x-auto">
      {error && <p className="mb-4 px-4 pt-3 text-sm text-ember-500">{error}</p>}
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border bg-surface-raised text-left text-stone-500 dark:text-stone-400">
            <th className="px-4 pb-3 pt-3 text-xs font-medium uppercase tracking-wide">Name</th>
            <th className="px-4 pb-3 pt-3 text-xs font-medium uppercase tracking-wide">Scopes</th>
            <th className="px-4 pb-3 pt-3 text-xs font-medium uppercase tracking-wide">Created</th>
            <th className="px-4 pb-3 pt-3 text-xs font-medium uppercase tracking-wide">Expires</th>
            <th className="px-4 pb-3 pt-3 text-xs font-medium uppercase tracking-wide"></th>
          </tr>
        </thead>
        <tbody>
          {tokens.map((token) => (
            <tr key={token.id} className="border-b border-stone-200 last:border-b-0 dark:border-stone-700">
              <td className="px-4 py-3 text-stone-900 dark:text-stone-100">{token.name}</td>
              <td className="px-4 py-3 text-stone-500 dark:text-stone-400">{token.scopes || "all"}</td>
              <td className="px-4 py-3 text-stone-500 dark:text-stone-400">
                {new Date(token.created_at).toLocaleDateString()}
              </td>
              <td className="px-4 py-3 text-stone-500 dark:text-stone-400">
                {token.expires_at
                  ? new Date(token.expires_at).toLocaleDateString()
                  : "Never"}
              </td>
              <td className="px-4 py-3">
                <Button
                  variant="danger"
                  onClick={() => handleRevoke(token.id)}
                  disabled={revoking === token.id}
                >
                  {revoking === token.id ? "Revoking..." : "Revoke"}
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
