/**
 * Source manifest reader for the zero-arg build derivation.
 *
 * The build entrypoint derives its positional args from `manifest.yaml` plus
 * the language package manifest when invoked with no arguments. This module
 * reads just the fields the derivation needs (kind, source, entrypoint artifact
 * path), dispatching on the manifest file extension.
 *
 * @module manifest
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import YAML from "yaml";

export type SourceManifest = {
  kind?: string | undefined;
  source?: string | undefined;
  entrypoint?: {
    artifactPath?: string | undefined;
  } | undefined;
};

const MANIFEST_BASENAMES = ["manifest.yaml", "manifest.yml", "manifest.json"] as const;

/**
 * Reads the source manifest from a provider root, returning the fields the
 * build derivation consumes. Throws if no manifest is found.
 */
export function readSourceManifest(root: string): SourceManifest {
  for (const name of MANIFEST_BASENAMES) {
    const path = resolve(root, name);
    let raw: string;
    try {
      raw = readFileSync(path, "utf8");
    } catch {
      continue;
    }
    return parseSourceManifest(raw, name);
  }
  throw new Error(`no manifest file found in ${root} (tried ${MANIFEST_BASENAMES.join(", ")})`);
}

function parseSourceManifest(raw: string, name: string): SourceManifest {
  const data = name.endsWith(".json") ? (JSON.parse(raw) as Record<string, unknown>) : YAML.parse(raw);
  if (data === null || typeof data !== "object" || Array.isArray(data)) {
    throw new Error(`${name} must be a mapping`);
  }
  const record = data as Record<string, unknown>;
  const entrypoint = record.entrypoint;
  const manifest: SourceManifest = {
    kind: typeof record.kind === "string" ? record.kind : undefined,
    source: typeof record.source === "string" ? record.source : undefined,
  };
  if (entrypoint !== null && typeof entrypoint === "object" && !Array.isArray(entrypoint)) {
    const artifactPath = (entrypoint as Record<string, unknown>).artifactPath;
    if (typeof artifactPath === "string") {
      manifest.entrypoint = { artifactPath };
    }
  }
  return manifest;
}
