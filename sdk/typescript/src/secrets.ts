import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

export interface SecretsProviderOptions extends RuntimeProviderOptions {
  getSecret: (name: string) => MaybePromise<string>;
}

export class SecretsProvider extends RuntimeProvider {
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

export function defineSecretsProvider(options: SecretsProviderOptions): SecretsProvider {
  return new SecretsProvider(options);
}

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
