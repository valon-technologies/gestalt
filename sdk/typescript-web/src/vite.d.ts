import type { Plugin } from "vite";

export interface GestaltViteOptions {
  /** Override process.env for hermetic tests. */
  env?: Record<string, string | undefined>;
}

/** Normalize a mount path to a leading and trailing slash, or "/" when empty. */
export function normalizeBasePath(value: string | undefined | null): string;

/** Vite preset encoding the Gestalt frontend env-var contract. */
export function gestalt(options?: GestaltViteOptions): Plugin;
