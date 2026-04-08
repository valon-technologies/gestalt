"use client";

import { useState } from "react";
import { createToken } from "@/lib/api";
import { INPUT_CLASSES } from "@/lib/constants";
import Button from "./Button";

interface TokenCreateFormProps {
  onCreated: () => void | Promise<void>;
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
      await onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create token");
    } finally {
      setCreating(false);
    }
  }

  return (
    <>
      <form onSubmit={handleSubmit} className="mt-8 flex items-end gap-3">
        <div>
          <label
            htmlFor="token-name"
            className="label-text block"
          >
            Token name
          </label>
          <input
            id="token-name"
            name="name"
            type="text"
            required
            placeholder="e.g. ci-pipeline"
            className={`mt-2 ${INPUT_CLASSES}`}
          />
        </div>
        <Button type="submit" disabled={creating}>
          {creating ? "Creating..." : "Create Token"}
        </Button>
      </form>

      {plaintext && (
        <div className="mt-6 rounded-lg border border-gold-300 bg-gold-50 p-5 dark:border-gold-700 dark:bg-gold-950/30">
          <p className="text-sm font-medium text-gold-800 dark:text-gold-300">
            Copy this token now. It will not be shown again.
          </p>
          <code className="mt-3 block break-all rounded-sm bg-base-white p-3 font-mono text-sm text-primary border border-alpha dark:bg-surface">
            {plaintext}
          </code>
        </div>
      )}

      {error && <p className="mt-4 text-sm text-ember-500">{error}</p>}
    </>
  );
}
