import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

/**
 * Fakeable secret lookup contract shared by providers and tests.
 */
export interface SecretsClient {
  getSecret(name: string): MaybePromise<string>;
}

/**
 * Runtime hooks required to implement a Gestalt secrets provider.
 */
export interface SecretsProviderOptions extends ProviderBaseOptions, SecretsClient {}

/**
 * Secrets provider implementation consumed by the Gestalt runtime.
 */
export class SecretsProvider extends ProviderBase implements SecretsClient {
  readonly kind = "secrets" as const;

  private readonly getSecretHandler: SecretsProviderOptions["getSecret"];

  constructor(options: SecretsProviderOptions) {
    super(options);
    this.getSecretHandler = options.getSecret;
  }

  async getSecret(name: string): Promise<string> {
    return await this.getSecretHandler(name);
  }
}

/**
 * Creates a secrets provider from a simple `getSecret` implementation.
 */
export function defineSecretsProvider(options: SecretsProviderOptions): SecretsProvider {
  return new SecretsProvider(options);
}

/**
 * Runtime type guard for secrets providers loaded from user modules.
 */
export function isSecretsProvider(value: unknown): value is SecretsProvider {
  return (
    value instanceof SecretsProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "secrets" &&
      "getSecret" in value)
  );
}
