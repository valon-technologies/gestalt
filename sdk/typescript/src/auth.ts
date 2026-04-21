import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
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
 * Input passed to an authentication provider's `beginAuthentication` handler.
 */
export interface BeginAuthenticationRequest {
  callbackUrl: string;
  hostState: string;
  scopes: string[];
  options: Record<string, string>;
}

/**
 * Response returned by an authentication provider's `beginAuthentication`
 * handler.
 */
export interface BeginAuthenticationResponse {
  authorizationUrl: string;
  providerState?: Uint8Array;
}

/**
 * Callback payload passed to an authentication provider's
 * `completeAuthentication` handler.
 */
export interface CompleteAuthenticationRequest {
  query: Record<string, string>;
  providerState: Uint8Array;
  callbackUrl: string;
}

export interface TokenAuthInput {
  token: string;
}

export interface HTTPRequestAuthInput {
  method: string;
  url: string;
  headers: Record<string, string>;
  query: Record<string, string>;
}

export interface AuthenticateRequest {
  token?: TokenAuthInput;
  http?: HTTPRequestAuthInput;
  options: Record<string, string>;
}

/** @deprecated Use BeginAuthenticationRequest. */
export type BeginLoginRequest = BeginAuthenticationRequest;

/** @deprecated Use BeginAuthenticationResponse. */
export type BeginLoginResponse = BeginAuthenticationResponse;

/** @deprecated Use CompleteAuthenticationRequest. */
export type CompleteLoginRequest = CompleteAuthenticationRequest;

/**
 * Session TTL hints exposed by an authentication provider.
 */
export interface AuthenticationSessionSettings {
  sessionTtlSeconds: number | bigint;
}

/**
 * Runtime hooks required to implement a Gestalt authentication provider.
 */
export interface AuthenticationProviderOptions extends RuntimeProviderOptions {
  beginAuthentication?: (
    request: BeginAuthenticationRequest,
  ) => MaybePromise<BeginAuthenticationResponse>;
  completeAuthentication?: (
    request: CompleteAuthenticationRequest,
  ) => MaybePromise<AuthenticatedUser>;
  authenticate?: (
    request: AuthenticateRequest,
  ) => MaybePromise<AuthenticatedUser | null | undefined>;
  beginLogin?: (
    request: BeginLoginRequest,
  ) => MaybePromise<BeginLoginResponse>;
  completeLogin?: (
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
export class AuthenticationProvider extends RuntimeProvider {
  readonly kind = "authentication" as const;

  private readonly beginAuthenticationHandler: AuthenticationProviderOptions["beginAuthentication"];
  private readonly completeAuthenticationHandler: AuthenticationProviderOptions["completeAuthentication"];
  private readonly authenticateHandler: AuthenticationProviderOptions["authenticate"];
  private readonly beginLoginHandler: AuthenticationProviderOptions["beginLogin"];
  private readonly completeLoginHandler: AuthenticationProviderOptions["completeLogin"];
  private readonly validateExternalTokenHandler: AuthenticationProviderOptions["validateExternalToken"];
  private readonly sessionSettingsHandler: AuthenticationProviderOptions["sessionSettings"];

  constructor(options: AuthenticationProviderOptions) {
    super(options);
    this.beginAuthenticationHandler = options.beginAuthentication;
    this.completeAuthenticationHandler = options.completeAuthentication;
    this.authenticateHandler = options.authenticate;
    this.beginLoginHandler = options.beginLogin;
    this.completeLoginHandler = options.completeLogin;
    this.validateExternalTokenHandler = options.validateExternalToken;
    this.sessionSettingsHandler = options.sessionSettings;
  }

  async beginAuthentication(
    request: BeginAuthenticationRequest,
  ): Promise<BeginAuthenticationResponse> {
    if (this.beginAuthenticationHandler) {
      return await this.beginAuthenticationHandler(request);
    }
    if (this.beginLoginHandler) {
      return await this.beginLoginHandler(request);
    }
    throw new Error("authentication provider does not implement beginAuthentication");
  }

  async completeAuthentication(
    request: CompleteAuthenticationRequest,
  ): Promise<AuthenticatedUser> {
    if (this.completeAuthenticationHandler) {
      return await this.completeAuthenticationHandler(request);
    }
    if (this.completeLoginHandler) {
      return await this.completeLoginHandler(request);
    }
    throw new Error(
      "authentication provider does not implement completeAuthentication",
    );
  }

  async authenticate(
    request: AuthenticateRequest,
  ): Promise<AuthenticatedUser | null | undefined> {
    if (this.authenticateHandler) {
      return await this.authenticateHandler(request);
    }
    if (request.token && this.validateExternalTokenHandler) {
      return await this.validateExternalTokenHandler(request.token.token);
    }
    return undefined;
  }

  async beginLogin(request: BeginLoginRequest): Promise<BeginLoginResponse> {
    return await this.beginAuthentication(request);
  }

  async completeLogin(request: CompleteLoginRequest): Promise<AuthenticatedUser> {
    return await this.completeAuthentication(request);
  }

  supportsExternalTokenValidation(): boolean {
    return (
      this.authenticateHandler !== undefined ||
      this.validateExternalTokenHandler !== undefined
    );
  }

  supportsAuthenticateRequest(request: AuthenticateRequest): boolean {
    if (this.authenticateHandler !== undefined) {
      return true;
    }
    return (
      request.token !== undefined &&
      this.validateExternalTokenHandler !== undefined
    );
  }

  async validateExternalToken(token: string): Promise<AuthenticatedUser | null | undefined> {
    if (this.validateExternalTokenHandler) {
      return await this.validateExternalTokenHandler(token);
    }
    return await this.authenticate({ token: { token }, options: {} });
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
 *   async beginAuthentication(request) {
 *     return {
 *       authorizationUrl: new URL("/login", request.callbackUrl).toString(),
 *     };
 *   },
 *   async completeAuthentication() {
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
      (("beginAuthentication" in value && "completeAuthentication" in value) ||
        ("beginLogin" in value && "completeLogin" in value)))
  );
}
