import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

/**
 * Identity payload returned by an auth provider after a successful login.
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
 * Input passed to an auth provider's `beginLogin` handler.
 */
export interface BeginLoginRequest {
  callbackUrl: string;
  hostState: string;
  scopes: string[];
  options: Record<string, string>;
}

/**
 * Response returned by an auth provider's `beginLogin` handler.
 */
export interface BeginLoginResponse {
  authorizationUrl: string;
  providerState?: Uint8Array;
}

/**
 * Callback payload passed to an auth provider's `completeLogin` handler.
 */
export interface CompleteLoginRequest {
  query: Record<string, string>;
  providerState: Uint8Array;
  callbackUrl: string;
}

/**
 * Session TTL hints exposed by an auth provider.
 */
export interface AuthSessionSettings {
  sessionTtlSeconds: number | bigint;
}

/**
 * Runtime hooks required to implement a Gestalt auth provider.
 */
export interface AuthProviderOptions extends RuntimeProviderOptions {
  beginLogin: (
    request: BeginLoginRequest,
  ) => MaybePromise<BeginLoginResponse>;
  completeLogin: (
    request: CompleteLoginRequest,
  ) => MaybePromise<AuthenticatedUser>;
  validateExternalToken?: (
    token: string,
  ) => MaybePromise<AuthenticatedUser | null | undefined>;
  sessionSettings?: () => MaybePromise<AuthSessionSettings>;
}

/**
 * Auth provider implementation consumed by the Gestalt runtime.
 */
export class AuthProvider extends RuntimeProvider {
  readonly kind = "auth" as const;

  private readonly beginLoginHandler: AuthProviderOptions["beginLogin"];
  private readonly completeLoginHandler: AuthProviderOptions["completeLogin"];
  private readonly validateExternalTokenHandler: AuthProviderOptions["validateExternalToken"];
  private readonly sessionSettingsHandler: AuthProviderOptions["sessionSettings"];

  constructor(options: AuthProviderOptions) {
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

  async sessionSettings(): Promise<AuthSessionSettings | undefined> {
    return await this.sessionSettingsHandler?.();
  }
}

/**
 * Creates an auth provider with the standard Gestalt runtime contract.
 *
 * @example
 * ```ts
 * import { defineAuthProvider } from "@valon-technologies/gestalt";
 *
 * export const auth = defineAuthProvider({
 *   displayName: "Example Auth",
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
export function defineAuthProvider(options: AuthProviderOptions): AuthProvider {
  return new AuthProvider(options);
}

/**
 * Runtime type guard for auth providers loaded from user modules.
 */
export function isAuthProvider(value: unknown): value is AuthProvider {
  return (
    value instanceof AuthProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "auth" &&
      "beginLogin" in value &&
      "completeLogin" in value)
  );
}
