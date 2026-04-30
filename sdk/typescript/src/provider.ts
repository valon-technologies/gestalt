import type { MaybePromise } from "./api.ts";

/**
 * Provider kinds supported by the TypeScript SDK runtime.
 */
export type ProviderKind =
  | "integration"
  | "authentication"
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
 * Shared runtime metadata and lifecycle hooks for authored providers.
 */
export interface RuntimeProviderOptions {
  name?: string;
  displayName?: string;
  description?: string;
  version?: string;
  configure?: ConfigureHandler;
  healthCheck?: HealthCheckHandler;
  warnings?: string[] | WarningsHandler;
  start?: StartHandler;
  close?: CloseHandler;
}

/**
 * Base class shared by all TypeScript SDK provider implementations.
 */
export abstract class RuntimeProvider {
  abstract readonly kind: ProviderKind;

  name: string;
  readonly displayName: string;
  readonly description: string;
  readonly version: string;

  private readonly configureHandler: ConfigureHandler | undefined;
  private readonly healthCheckHandler: HealthCheckHandler | undefined;
  private readonly warningsSource: string[] | WarningsHandler | undefined;
  private readonly startHandler: StartHandler | undefined;
  private readonly closeHandler: CloseHandler | undefined;

  protected constructor(options: RuntimeProviderOptions) {
    this.name = slugName(options.name ?? "");
    this.displayName = options.displayName?.trim() ?? "";
    this.description = options.description?.trim() ?? "";
    this.version = options.version?.trim() ?? "";
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

  runtimeMetadata(): ProviderMetadata {
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

  async configureProvider(name: string, config: Record<string, unknown>): Promise<void> {
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
 * Runtime type guard for values that implement the provider base contract.
 */
export function isRuntimeProvider(value: unknown): value is RuntimeProvider {
  return (
    value instanceof RuntimeProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      "resolveName" in value &&
      typeof (value as { resolveName?: unknown }).resolveName === "function" &&
      "configureProvider" in value &&
      typeof (value as { configureProvider?: unknown }).configureProvider === "function" &&
      "supportsHealthCheck" in value &&
      typeof (value as { supportsHealthCheck?: unknown }).supportsHealthCheck === "function" &&
      "healthCheck" in value &&
      typeof (value as { healthCheck?: unknown }).healthCheck === "function" &&
      "startProvider" in value &&
      typeof (value as { startProvider?: unknown }).startProvider === "function" &&
      "warnings" in value &&
      typeof (value as { warnings?: unknown }).warnings === "function" &&
      "closeProvider" in value &&
      typeof (value as { closeProvider?: unknown }).closeProvider === "function")
  );
}

/**
 * Normalizes package and provider names into Gestalt's slug format.
 */
export function slugName(value: string): string {
  const normalized = value.trim().replace(/^@[^/]+\//, "");
  return normalized.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
}
