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
      <div className="surface-card py-12 text-center text-sm text-faint">
        No API tokens yet.
      </div>
    );
  }

  return (
    <div className="table-surface overflow-x-auto">
      {error && <p className="px-6 pt-5 text-sm text-ember-500">{error}</p>}
      <table className="w-full text-sm">
        <thead className="table-head">
          <tr className="border-b border-alpha text-left">
            <th className="px-5 py-3.5 label-text">Name</th>
            <th className="px-5 py-3.5 label-text">Scopes</th>
            <th className="px-5 py-3.5 label-text">Created</th>
            <th className="px-5 py-3.5 label-text">Expires</th>
            <th className="px-5 py-3.5 label-text"></th>
          </tr>
        </thead>
        <tbody>
          {tokens.map((token) => (
            <tr key={token.id} className="table-row border-b border-alpha last:border-b-0">
              <td className="px-5 py-4 text-primary">{token.name}</td>
              <td className="px-5 py-4 text-muted">{token.scopes || "all"}</td>
              <td className="px-5 py-4 text-muted font-mono text-xs">
                {new Date(token.created_at).toLocaleDateString()}
              </td>
              <td className="px-5 py-4 text-muted font-mono text-xs">
                {token.expires_at
                  ? new Date(token.expires_at).toLocaleDateString()
                  : "Never"}
              </td>
              <td className="px-5 py-4">
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
