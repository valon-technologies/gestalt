import type { ProviderKind } from "./provider.ts";

type ProviderKindDefinition = {
  readonly tokens: readonly string[];
  readonly formatToken: string;
  readonly defaultExportNames: readonly string[];
  readonly label: string;
};

const PROVIDER_KIND_DEFINITIONS = {
  integration: {
    tokens: ["plugin"],
    formatToken: "plugin",
    defaultExportNames: ["provider", "plugin"],
    label: "plugin provider",
  },
  authentication: {
    tokens: ["authentication"],
    formatToken: "authentication",
    defaultExportNames: ["authentication", "provider"],
    label: "authentication provider",
  },
  cache: {
    tokens: ["cache"],
    formatToken: "cache",
    defaultExportNames: ["cache", "provider"],
    label: "cache provider",
  },
  secrets: {
    tokens: ["secrets"],
    formatToken: "secrets",
    defaultExportNames: ["secrets", "provider"],
    label: "secrets provider",
  },
  s3: {
    tokens: ["s3"],
    formatToken: "s3",
    defaultExportNames: ["s3", "provider"],
    label: "s3 provider",
  },
  workflow: {
    tokens: ["workflow"],
    formatToken: "workflow",
    defaultExportNames: ["workflow", "provider"],
    label: "workflow provider",
  },
  agent: {
    tokens: ["agent"],
    formatToken: "agent",
    defaultExportNames: ["agent", "provider"],
    label: "agent provider",
  },
  telemetry: {
    tokens: ["telemetry"],
    formatToken: "telemetry",
    defaultExportNames: ["telemetry", "provider"],
    label: "telemetry provider",
  },
} satisfies Record<ProviderKind, ProviderKindDefinition>;

const EXTERNAL_PROVIDER_KIND_TOKEN_SET = new Set<string>(
  Object.values(PROVIDER_KIND_DEFINITIONS).flatMap(
    (definition) => definition.tokens,
  ),
);

const EXTERNAL_PROVIDER_KIND_MAP = new Map<string, ProviderKind>(
  Object.entries(PROVIDER_KIND_DEFINITIONS).flatMap(([kind, definition]) =>
    definition.tokens.map(
      (token) => [token, kind as ProviderKind] as const,
    ),
  ),
);

export function isExternalProviderKindToken(value: string): boolean {
  return EXTERNAL_PROVIDER_KIND_TOKEN_SET.has(value.trim().toLowerCase());
}

export function parseExternalProviderKind(value: string): ProviderKind {
  const normalized = value.trim().toLowerCase();
  const kind = EXTERNAL_PROVIDER_KIND_MAP.get(normalized);
  if (!kind) {
    throw new Error(`unsupported provider kind ${JSON.stringify(value)}`);
  }
  return kind;
}

export function formatExternalProviderKind(kind: ProviderKind): string {
  return PROVIDER_KIND_DEFINITIONS[kind].formatToken;
}

export function providerKindLabel(kind: ProviderKind): string {
  return PROVIDER_KIND_DEFINITIONS[kind].label;
}

export function defaultProviderExportNames(
  kind: ProviderKind,
): readonly string[] {
  return PROVIDER_KIND_DEFINITIONS[kind].defaultExportNames;
}

export function resolveDefaultProviderExport(
  module: Record<string, unknown>,
  kind: ProviderKind,
): unknown {
  for (const exportName of defaultProviderExportNames(kind)) {
    const candidate = Reflect.get(module, exportName);
    if (candidate !== undefined && candidate !== null) {
      return candidate;
    }
  }
  return Reflect.get(module, "default");
}
