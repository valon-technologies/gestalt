import { createHash } from "node:crypto";

export const FORMAT_VERSION = 1 as const;
export const EXTRACTOR_VERSION = "typed-app-contracts/0.1";

export type Scalar = string | number | boolean | null;

export type WireSchema =
  | { kind: "string" }
  | { kind: "number" }
  | { kind: "boolean" }
  | { kind: "null" }
  | { kind: "literal"; value: Exclude<Scalar, null> }
  | { kind: "array"; items: WireSchema }
  | { kind: "tuple"; items: WireSchema[] }
  | {
      kind: "object";
      additionalProperties: false;
      properties: Record<string, { optional: boolean; schema: WireSchema }>;
    }
  | { kind: "union"; variants: WireSchema[] };

export interface ToolContract {
  description: string;
  input: WireSchema;
  output: WireSchema;
  digest: string;
}

export interface AppContract {
  formatVersion: typeof FORMAT_VERSION;
  app: { name: string; version: string };
  compiler: { extractor: typeof EXTRACTOR_VERSION; typescript: string };
  tools: Record<string, ToolContract>;
}

export interface LockedDependency {
  app: string;
  version: string;
  contractDigest: string;
}

export interface AppManifest {
  name: string;
  version: string;
  dependencies: Record<string, LockedDependency>;
}

export interface PublishedRelease {
  formatVersion: typeof FORMAT_VERSION;
  build: { extractor: string; typescript: string; bun: string; digest: string };
  manifest: AppManifest;
  manifestDigest: string;
  contract: AppContract;
  contractDigest: string;
  clientTemplate: string;
  clientDigest: string;
  artifactFile: "provider.js";
  artifactDigest: string;
  sourceDigest: string;
}

export class ExperimentError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(`${code}: ${message}`);
    this.name = "ExperimentError";
  }
}

export function stableStringify(value: unknown): string {
  return JSON.stringify(sortValue(value), null, 2) + "\n";
}

function sortValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sortValue);
  }
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, entry]) => [key, sortValue(entry)]),
    );
  }
  return value;
}

export function digest(value: string | Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

export function validateManifest(manifest: AppManifest): void {
  const coordinate = /^[a-z][a-z0-9-]*(?:\/[a-z][a-z0-9-]*)?$/;
  const alias = /^[a-z][a-zA-Z0-9]*$/;
  if (!coordinate.test(manifest.name)) {
    throw new ExperimentError("INVALID_MANIFEST", `invalid app name ${manifest.name}`);
  }
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(manifest.version)) {
    throw new ExperimentError("INVALID_MANIFEST", `version ${manifest.version} is not exact semver`);
  }
  for (const [dependencyAlias, dependency] of Object.entries(manifest.dependencies)) {
    if (!alias.test(dependencyAlias)) {
      throw new ExperimentError("INVALID_MANIFEST", `invalid dependency alias ${dependencyAlias}`);
    }
    if (!coordinate.test(dependency.app)) {
      throw new ExperimentError("INVALID_MANIFEST", `invalid dependency app ${dependency.app}`);
    }
    if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(dependency.version)) {
      throw new ExperimentError(
        "DEPENDENCY_NOT_EXACT",
        `${dependency.app} uses non-exact version ${dependency.version}`,
      );
    }
    if (!/^[a-f0-9]{64}$/.test(dependency.contractDigest)) {
      throw new ExperimentError(
        "INVALID_MANIFEST",
        `${dependency.app}@${dependency.version} has an invalid contract digest`,
      );
    }
  }
}
