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
      <p className="py-8 text-center text-sm text-gray-500">
        No API tokens yet.
      </p>
    );
  }

  return (
    <div>
      {error && <p className="mb-4 text-sm text-red-600">{error}</p>}
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-200 text-left text-gray-500">
            <th className="pb-2 font-medium">Name</th>
            <th className="pb-2 font-medium">Scopes</th>
            <th className="pb-2 font-medium">Created</th>
            <th className="pb-2 font-medium">Expires</th>
            <th className="pb-2 font-medium"></th>
          </tr>
        </thead>
        <tbody>
          {tokens.map((token) => (
            <tr key={token.id} className="border-b border-gray-100">
              <td className="py-3 text-gray-900">{token.name}</td>
              <td className="py-3 text-gray-500">{token.scopes || "all"}</td>
              <td className="py-3 text-gray-500">
                {new Date(token.created_at).toLocaleDateString()}
              </td>
              <td className="py-3 text-gray-500">
                {token.expires_at
                  ? new Date(token.expires_at).toLocaleDateString()
                  : "Never"}
              </td>
              <td className="py-3">
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
