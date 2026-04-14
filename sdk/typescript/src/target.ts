import { readFileSync } from "node:fs";
import { isAbsolute, normalize, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { slugName, type ProviderKind } from "./provider.ts";

export type ModuleTarget = {
  modulePath: string;
  exportName?: string;
};

export type ProviderTarget = ModuleTarget & {
  kind: ProviderKind;
};

export type PackageConfig = {
  name?: string;
  providerTarget?: ProviderTarget;
};

type PackageProviderConfig =
  | string
  | {
      kind?: string;
      target?: string;
    };

const EXTERNAL_PROVIDER_KIND_TOKENS = new Set<string>([
  "plugin",
  "integration",
  "auth",
  "fileapi",
  "secrets",
  "telemetry",
]);

export function parseModuleTarget(target: string, label = "gestalt provider target"): ModuleTarget {
  const [modulePathRaw, exportNameRaw] = target.split("#", 2);
  const modulePath = modulePathRaw?.trim() ?? "";
  const exportName = exportNameRaw?.trim() || undefined;

  if (!modulePath) {
    throw new Error(`${label} must include a relative module path`);
  }
  if (!modulePath.startsWith("./") && !modulePath.startsWith("../")) {
    throw new Error(`${label} module path must be relative`);
  }
  if (exportName !== undefined && !/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(exportName)) {
    throw new Error(`${label} export must be a valid JavaScript identifier`);
  }

  const parsed: ModuleTarget = {
    modulePath,
  };
  if (exportName !== undefined) {
    parsed.exportName = exportName;
  }
  return parsed;
}

export function parseProviderTarget(target: string | PackageProviderConfig): ProviderTarget {
  if (typeof target === "string") {
    const prefixed = parseKindPrefixedTarget(target);
    if (prefixed) {
      return prefixed;
    }
    return {
      kind: "integration",
      ...parseModuleTarget(target, "gestalt.provider"),
    };
  }

  const kind = parseProviderKind(target.kind ?? "integration");
  if (!target.target || typeof target.target !== "string") {
    throw new Error("gestalt.provider.target is required");
  }
  return {
    kind,
    ...parseModuleTarget(target.target, "gestalt.provider.target"),
  };
}

export const parsePluginTarget = parseModuleTarget;

export function readPackageConfig(root: string): PackageConfig {
  const packagePath = resolve(root, "package.json");
  const raw = JSON.parse(readFileSync(packagePath, "utf8")) as Record<string, unknown>;
  const gestalt = (raw.gestalt ?? {}) as Record<string, unknown>;
  const name = typeof raw.name === "string" ? raw.name.trim() : undefined;

  let providerTarget: ProviderTarget | undefined;
  if (typeof gestalt.provider === "string") {
    providerTarget = parseProviderTarget(gestalt.provider);
  } else if (isProviderConfigObject(gestalt.provider)) {
    providerTarget = parseProviderTarget(gestalt.provider);
  } else if (typeof gestalt.plugin === "string") {
    providerTarget = {
      kind: "integration",
      ...parseModuleTarget(gestalt.plugin, "gestalt.plugin"),
    };
  }

  const config: PackageConfig = {};
  if (name !== undefined) {
    config.name = name;
  }
  if (providerTarget !== undefined) {
    config.providerTarget = providerTarget;
  }
  return config;
}

export function readPackageProviderTarget(root: string): ProviderTarget {
  const config = readPackageConfig(root);
  if (!config.providerTarget) {
    throw new Error("package.json gestalt.provider or gestalt.plugin is required");
  }
  return config.providerTarget;
}

export function readPackagePluginTarget(root: string): string {
  const target = readPackageProviderTarget(root);
  if (target.kind !== "integration") {
    throw new Error(`package.json provider kind ${JSON.stringify(target.kind)} is not an integration provider`);
  }
  return formatModuleTarget(target);
}

export function defaultProviderName(root: string): string {
  const config = readPackageConfig(root);
  return slugName(config.name ?? "");
}

export const defaultPluginName = defaultProviderName;

export function resolveProviderModulePath(root: string, target: ProviderTarget | ModuleTarget): string {
  const absolute = resolve(root, target.modulePath);
  if (!isAbsolute(absolute)) {
    throw new Error("provider module path did not resolve to an absolute path");
  }
  return normalize(absolute);
}

export const resolvePluginModulePath = resolveProviderModulePath;

export function resolveProviderImportUrl(root: string, target: ProviderTarget | ModuleTarget): string {
  return pathToFileURL(resolveProviderModulePath(root, target)).href;
}

export const resolvePluginImportUrl = resolveProviderImportUrl;

export function formatProviderTarget(target: ProviderTarget): string {
  return `${formatProviderKind(target.kind)}:${formatModuleTarget(target)}`;
}

export function formatModuleTarget(target: ModuleTarget): string {
  return `${target.modulePath}${target.exportName ? `#${target.exportName}` : ""}`;
}

function parseKindPrefixedTarget(target: string): ProviderTarget | undefined {
  const match = target.match(
    /^(plugin|integration|auth|fileapi|secrets|telemetry):(.*)$/,
  );
  if (!match) {
    return undefined;
  }
  return {
    kind: parseProviderKind(match[1]!),
    ...parseModuleTarget(match[2]!, "provider target"),
  };
}

function parseProviderKind(value: string): ProviderKind {
  const normalized = value.trim().toLowerCase();
  if (!EXTERNAL_PROVIDER_KIND_TOKENS.has(normalized)) {
    throw new Error(`unsupported provider kind ${JSON.stringify(value)}`);
  }
  if (normalized === "plugin") {
    return "integration";
  }
  return normalized as ProviderKind;
}

function formatProviderKind(kind: ProviderKind): string {
  if (kind === "integration") {
    return "plugin";
  }
  return kind;
}

function isProviderConfigObject(value: unknown): value is { kind?: string; target?: string } {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
