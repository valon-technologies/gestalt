import type { MaybePromise } from "./api.ts";

export type ProviderKind =
  | "integration"
  | "auth"
  | "cache"
  | "secrets"
  | "telemetry";

export type ProviderMetadata = {
  kind?: ProviderKind;
  name?: string;
  displayName?: string;
  description?: string;
  version?: string;
};

export type ConfigureHandler = (
  name: string,
  config: Record<string, unknown>,
) => MaybePromise<void>;

export type HealthCheckHandler = () => MaybePromise<void>;

export type WarningsHandler = () => MaybePromise<string[]>;

export type CloseHandler = () => MaybePromise<void>;

export interface RuntimeProviderOptions {
  name?: string;
  displayName?: string;
  description?: string;
  version?: string;
  configure?: ConfigureHandler;
  healthCheck?: HealthCheckHandler;
  warnings?: string[] | WarningsHandler;
  close?: CloseHandler;
}

export abstract class RuntimeProvider {
  abstract readonly kind: ProviderKind;

  name: string;
  readonly displayName: string;
  readonly description: string;
  readonly version: string;

  private readonly configureHandler: ConfigureHandler | undefined;
  private readonly healthCheckHandler: HealthCheckHandler | undefined;
  private readonly warningsSource: string[] | WarningsHandler | undefined;
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

export function isRuntimeProvider(value: unknown): value is RuntimeProvider {
  return (
    value instanceof RuntimeProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      "resolveName" in value &&
      "configureProvider" in value)
  );
}

export function slugName(value: string): string {
  const normalized = value.trim().replace(/^@[^/]+\//, "");
  return normalized.replace(/[^A-Za-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
}
