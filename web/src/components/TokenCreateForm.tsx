"use client";

import { useState } from "react";
import { createToken } from "@/lib/api";
import Button from "./Button";

interface TokenCreateFormProps {
  onCreated: () => void;
}

export default function TokenCreateForm({ onCreated }: TokenCreateFormProps) {
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [plaintext, setPlaintext] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;

    setCreating(true);
    setError(null);
    setPlaintext(null);

    try {
      const result = await createToken(name.trim());
      setPlaintext(result.token);
      setName("");
      onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create token");
    } finally {
      setCreating(false);
    }
  }

  return (
    <>
      <form onSubmit={handleSubmit} className="mt-6 flex items-end gap-3">
        <div>
          <label
            htmlFor="token-name"
            className="block text-sm font-medium text-gray-700"
          >
            Token name
          </label>
          <input
            id="token-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. ci-pipeline"
            className="mt-1 rounded border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>
        <Button type="submit" disabled={creating || !name.trim()}>
          {creating ? "Creating..." : "Create Token"}
        </Button>
      </form>

      {plaintext && (
        <div className="mt-4 rounded border border-yellow-300 bg-yellow-50 p-4">
          <p className="text-sm font-medium text-yellow-800">
            Copy this token now. It will not be shown again.
          </p>
          <code className="mt-2 block break-all rounded bg-white p-2 text-sm text-gray-900">
            {plaintext}
          </code>
        </div>
      )}

      {error && <p className="mt-4 text-sm text-red-600">{error}</p>}
    </>
  );
}
