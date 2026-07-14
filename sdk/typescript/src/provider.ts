import type { MaybePromise } from "./api.ts";
import { IndexedDB } from "./providers/indexeddb.ts";
import {
  type MigrationRunOptions,
  type Revision,
  resolveMigrationDbBinding,
  runMigrations,
} from "./providers/migrations.ts";

/**
 * Migrations declared on a provider: a bare ordered list of revisions, a list
 * plus binding/ledger overrides, or a callback resolved at configure time.
 */
export type MigrationsOption =
  | Revision[]
  | MigrationRunOptions
  | ((
      name: string,
      config: Record<string, unknown>,
    ) => MigrationRunOptions | undefined);

/**
 * Provider kinds supported by the TypeScript SDK runtime.
 */
export type ProviderKind =
  | "integration"
  | "authorization"
  | "identity"
  | "cache"
  | "secrets"
  | "s3"
  | "runtime"
  | "workflow"
  | "agent"
  | "telemetry";

/**
 * Runtime metadata reported to the Gestalt host during startup.
 */
export type ProviderMetadata = {
  kind?: ProviderKind;
  name?: string;
  displayName?: string;
  description?: string;
  version?: string;
};

/**
 * Optional configuration hook invoked after the host starts the provider.
 */
export type ConfigureHandler = (
  name: string,
  config: Record<string, unknown>,
) => MaybePromise<void>;

/**
 * Optional readiness probe invoked by the Gestalt host.
 */
export type HealthCheckHandler = () => MaybePromise<void>;

/**
 * Optional callback that returns non-fatal runtime warnings.
 */
export type WarningsHandler = () => MaybePromise<string[]>;

/**
 * Optional hook invoked after configuration when the host is ready for
 * provider-owned background work to begin.
 */
export type StartHandler = () => MaybePromise<void>;

/**
 * Optional shutdown hook invoked when the provider process exits.
 */
export type CloseHandler = () => MaybePromise<void>;

/**
 * Shared provider metadata and lifecycle hooks for authored providers.
 */
export interface ProviderBaseOptions {
  name?: string;
  displayName?: string;
  description?: string;
  version?: string;
  /**
   * Revisions the SDK discovers and runs during provider startup, before the
   * author's own configure handler. The author never calls a runner.
   */
  migrations?: MigrationsOption;
  configure?: ConfigureHandler;
  healthCheck?: HealthCheckHandler;
  warnings?: string[] | WarningsHandler;
  start?: StartHandler;
  close?: CloseHandler;
}

/**
 * Base class shared by all TypeScript SDK provider implementations.
 */
export abstract class ProviderBase {
  abstract readonly kind: ProviderKind;

  name: string;
  readonly displayName: string;
  readonly description: string;
  readonly version: string;

  private readonly migrationsSource: MigrationsOption | undefined;
  private readonly configureHandler: ConfigureHandler | undefined;
  private readonly healthCheckHandler: HealthCheckHandler | undefined;
  private readonly warningsSource: string[] | WarningsHandler | undefined;
  private readonly startHandler: StartHandler | undefined;
  private readonly closeHandler: CloseHandler | undefined;

  protected constructor(options: ProviderBaseOptions) {
    this.name = slugName(options.name ?? "");
    this.displayName = options.displayName?.trim() ?? "";
    this.description = options.description?.trim() ?? "";
    this.version = options.version?.trim() ?? "";
    this.migrationsSource = options.migrations;
    this.configureHandler = options.configure;
    this.healthCheckHandler = options.healthCheck;
    this.warningsSource = Array.isArray(options.warnings)
      ? [...options.warnings]
      : options.warnings;
    this.startHandler = options.start;
    this.closeHandler = options.close;
  }

  resolveName(fallback: string): void {
    if (!this.name) {
      this.name = slugName(fallback);
    }
  }

  providerMetadata(): ProviderMetadata {
    const metadata: ProviderMetadata = {
      kind: this.kind,
    };
    if (this.name) {
      metadata.name = this.name;
    }
    if (this.displayName) {
      metadata.displayName = this.displayName;
    }
    if (this.description) {
      metadata.description = this.description;
    }
    if (this.version) {
      metadata.version = this.version;
    }
    return metadata;
  }

  async configureProvider(
    name: string,
    config: Record<string, unknown>,
  ): Promise<void> {
    const options = resolveMigrations(this.migrationsSource, name, config);
    if (options) {
      const dbBinding = resolveMigrationDbBinding(options, config);
      const db = new IndexedDB(dbBinding);
      try {
        await runMigrations(db, { ...options, appName: name });
      } finally {
        db.close();
      }
    }
    await this.configureHandler?.(name, config);
  }

  supportsHealthCheck(): boolean {
    return this.healthCheckHandler !== undefined;
  }

  async healthCheck(): Promise<void> {
    await this.healthCheckHandler?.();
  }

  async startProvider(): Promise<void> {
    await this.startHandler?.();
  }

  async warnings(): Promise<string[]> {
    if (!this.warningsSource) {
      return [];
    }
    if (Array.isArray(this.warningsSource)) {
      return [...this.warningsSource];
    }
    return [...(await this.warningsSource())];
  }

  async closeProvider(): Promise<void> {
    await this.closeHandler?.();
  }
}

/**
 * Type guard for values that implement the provider base contract.
 */
export function isProviderBase(value: unknown): value is ProviderBase {
  return (
    value instanceof ProviderBase ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      "resolveName" in value &&
      typeof (value as { resolveName?: unknown }).resolveName === "function" &&
      "configureProvider" in value &&
      typeof (value as { configureProvider?: unknown }).configureProvider ===
        "function" &&
      "supportsHealthCheck" in value &&
      typeof (value as { supportsHealthCheck?: unknown })
        .supportsHealthCheck === "function" &&
      "healthCheck" in value &&
      typeof (value as { healthCheck?: unknown }).healthCheck === "function" &&
      "startProvider" in value &&
      typeof (value as { startProvider?: unknown }).startProvider ===
        "function" &&
      "warnings" in value &&
      typeof (value as { warnings?: unknown }).warnings === "function" &&
      "closeProvider" in value &&
      typeof (value as { closeProvider?: unknown }).closeProvider ===
        "function")
  );
}

/**
 * Normalizes package and provider names into Gestalt's slug format.
 */
export function slugName(value: string): string {
  const normalized = value.trim().replace(/^@[^/]+\//, "");
  return normalized.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
}

function resolveMigrations(
  source: MigrationsOption | undefined,
  name: string,
  config: Record<string, unknown>,
): MigrationRunOptions | undefined {
  if (source === undefined) {
    return undefined;
  }
  if (typeof source === "function") {
    const resolved = source(name, config);
    return resolved === undefined ? undefined : normalizeMigrationRunOptions(resolved);
  }
  return normalizeMigrationRunOptions(source);
}

function normalizeMigrationRunOptions(
  input: Revision[] | MigrationRunOptions,
): MigrationRunOptions | undefined {
  if (Array.isArray(input)) {
    return input.length > 0 ? { revisions: input } : undefined;
  }
  return input.revisions.length > 0 ? input : undefined;
}
