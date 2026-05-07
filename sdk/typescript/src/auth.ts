import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

/**
 * Identity payload returned by an authentication provider after a successful
 * login.
 */
export interface AuthenticatedUser {
  subject: string;
  email?: string;
  emailVerified?: boolean;
  displayName?: string;
  avatarUrl?: string;
  claims?: Record<string, string>;
}

/**
 * Input passed to an authentication provider's `beginLogin` handler.
 */
export interface BeginLoginRequest {
  callbackUrl: string;
  hostState: string;
  scopes: string[];
  options: Record<string, string>;
}

/**
 * Response returned by an authentication provider's `beginLogin` handler.
 */
export interface BeginLoginResponse {
  authorizationUrl: string;
  providerState?: Uint8Array;
}

/**
 * Callback payload passed to an authentication provider's `completeLogin`
 * handler.
 */
export interface CompleteLoginRequest {
  query: Record<string, string>;
  providerState: Uint8Array;
  callbackUrl: string;
}

/**
 * Session TTL hints exposed by an authentication provider.
 */
export interface AuthenticationSessionSettings {
  sessionTtlSeconds: number | bigint;
}

/**
 * Runtime hooks required to implement a Gestalt authentication provider.
 */
export interface AuthenticationProviderOptions extends ProviderBaseOptions {
  beginLogin: (
    request: BeginLoginRequest,
  ) => MaybePromise<BeginLoginResponse>;
  completeLogin: (
    request: CompleteLoginRequest,
  ) => MaybePromise<AuthenticatedUser>;
  validateExternalToken?: (
    token: string,
  ) => MaybePromise<AuthenticatedUser | null | undefined>;
  sessionSettings?: () => MaybePromise<AuthenticationSessionSettings>;
}

/**
 * Authentication provider implementation consumed by the Gestalt runtime.
 */
export class AuthenticationProvider extends ProviderBase {
  readonly kind = "authentication" as const;

  private readonly beginLoginHandler: AuthenticationProviderOptions["beginLogin"];
  private readonly completeLoginHandler: AuthenticationProviderOptions["completeLogin"];
  private readonly validateExternalTokenHandler: AuthenticationProviderOptions["validateExternalToken"];
  private readonly sessionSettingsHandler: AuthenticationProviderOptions["sessionSettings"];

  constructor(options: AuthenticationProviderOptions) {
    super(options);
    this.beginLoginHandler = options.beginLogin;
    this.completeLoginHandler = options.completeLogin;
    this.validateExternalTokenHandler = options.validateExternalToken;
    this.sessionSettingsHandler = options.sessionSettings;
  }

  async beginLogin(request: BeginLoginRequest): Promise<BeginLoginResponse> {
    return await this.beginLoginHandler(request);
  }

  async completeLogin(request: CompleteLoginRequest): Promise<AuthenticatedUser> {
    return await this.completeLoginHandler(request);
  }

  supportsExternalTokenValidation(): boolean {
    return this.validateExternalTokenHandler !== undefined;
  }

  async validateExternalToken(token: string): Promise<AuthenticatedUser | null | undefined> {
    return await this.validateExternalTokenHandler?.(token);
  }

  supportsSessionSettings(): boolean {
    return this.sessionSettingsHandler !== undefined;
  }

  async sessionSettings(): Promise<AuthenticationSessionSettings | undefined> {
    return await this.sessionSettingsHandler?.();
  }
}

/**
 * Creates an authentication provider with the standard Gestalt runtime
 * contract.
 *
 * @example
 * ```ts
 * import { defineAuthenticationProvider } from "@valon-technologies/gestalt";
 *
 * export const authentication = defineAuthenticationProvider({
 *   displayName: "Example Authentication",
 *   async beginLogin(request) {
 *     return {
 *       authorizationUrl: new URL("/login", request.callbackUrl).toString(),
 *     };
 *   },
 *   async completeLogin() {
 *     return { subject: "usr_123", email: "user@example.com" };
 *   },
 * });
 * ```
 */
export function defineAuthenticationProvider(
  options: AuthenticationProviderOptions,
): AuthenticationProvider {
  return new AuthenticationProvider(options);
}

/**
 * Runtime type guard for authentication providers loaded from user modules.
 */
export function isAuthenticationProvider(
  value: unknown,
): value is AuthenticationProvider {
  return (
    value instanceof AuthenticationProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      String((value as { kind?: unknown }).kind ?? "") === "authentication" &&
      "beginLogin" in value &&
      "completeLogin" in value)
  );
}
