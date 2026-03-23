"use client";

import { useState } from "react";
import { createToken } from "@/lib/api";
import Button from "./Button";

interface TokenCreateFormProps {
  onCreated: () => void;
}

export default function TokenCreateForm({ onCreated }: TokenCreateFormProps) {
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [plaintext, setPlaintext] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const name = (new FormData(form).get("name") as string)?.trim();
    if (!name) return;

    setCreating(true);
    setError(null);
    setPlaintext(null);

    try {
      const result = await createToken(name);
      setPlaintext(result.token);
      form.reset();
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
            className="block text-sm font-medium text-stone-700"
          >
            Token name
          </label>
          <input
            id="token-name"
            name="name"
            type="text"
            required
            placeholder="e.g. ci-pipeline"
            className="mt-1 rounded-md border border-border bg-surface px-3 py-2 text-sm text-stone-900 placeholder:text-stone-400 focus:border-timber-400 focus:outline-none focus:ring-2 focus:ring-timber-400/25"
          />
        </div>
        <Button type="submit" disabled={creating}>
          {creating ? "Creating..." : "Create Token"}
        </Button>
      </form>

      {plaintext && (
        <div className="mt-4 rounded-lg border border-harvest-300 bg-harvest-50 p-4">
          <p className="text-sm font-medium text-harvest-700">
            Copy this token now. It will not be shown again.
          </p>
          <code className="mt-2 block break-all rounded-md bg-stone-100 p-2 font-mono text-sm text-stone-900">
            {plaintext}
          </code>
        </div>
      )}

      {error && <p className="mt-4 text-sm text-ember-500">{error}</p>}
    </>
  );
}
