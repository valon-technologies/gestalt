import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

/** gRPC metadata key for the caller bearer token on grant-management RPCs. */
export const CALLER_BEARER_TOKEN_METADATA_KEY =
  "x-gestalt-caller-bearer-token";

/**
 * Call context passed to grant-management handlers.
 */
export interface AuthCallContext {
  callerBearerToken: string;
}

/**
 * RFC 6749 authorization request parameters.
 */
export interface AuthorizeRequest {
  responseType: string;
  clientId: string;
  redirectUri: string;
  scope: string;
  state: string;
}

/**
 * RFC 6749 authorization redirect response.
 */
export interface AuthorizeResponse {
  redirectUri: string;
}

/**
 * RFC 6749 token endpoint request parameters.
 */
export interface TokenRequest {
  grantType: string;
  code: string;
  redirectUri: string;
  clientId: string;
  state: string;
  scope: string;
  subjectToken: string;
  subjectTokenType: string;
}

/**
 * RFC 6749 token endpoint response fields.
 */
export interface TokenResponse {
  accessToken: string;
  tokenType: string;
  expiresIn: number | bigint;
  refreshToken?: string;
  scope?: string;
  grantId?: string;
}

/**
 * RFC 7662 token introspection request parameters.
 */
export interface IntrospectRequest {
  token: string;
  tokenTypeHint?: string;
}

/**
 * RFC 7662 token introspection response fields.
 */
export interface IntrospectResponse {
  active: boolean;
  subject?: string;
  scope?: string;
  clientId?: string;
  audience?: string[];
}

/**
 * Grant scope entry returned by grant-management RPCs.
 */
export interface GrantScope {
  scope: string;
  resource?: string[];
}

/**
 * OIDF-shaped grant details.
 */
export interface GrantDetails {
  scopes: GrantScope[];
  createdAt: number | bigint;
  expiresAt: number | bigint;
}

/**
 * Runtime hooks required to implement a Gestalt authentication provider.
 */
export interface AuthenticationProviderOptions extends ProviderBaseOptions {
  authorize: (request: AuthorizeRequest) => MaybePromise<AuthorizeResponse>;
  token: (request: TokenRequest) => MaybePromise<TokenResponse>;
  introspect: (request: IntrospectRequest) => MaybePromise<IntrospectResponse>;
  listGrants: (
    request: Record<string, never>,
    call: AuthCallContext,
  ) => MaybePromise<{ grantIds: string[] }>;
  getGrant: (
    request: { grantId: string },
    call: AuthCallContext,
  ) => MaybePromise<GrantDetails>;
  revokeGrant: (
    request: { grantId: string },
    call: AuthCallContext,
  ) => MaybePromise<void>;
}

/**
 * Authentication provider implementation consumed by the Gestalt runtime.
 */
export class AuthenticationProvider extends ProviderBase {
  readonly kind = "authentication" as const;

  private readonly authorizeHandler: AuthenticationProviderOptions["authorize"];
  private readonly tokenHandler: AuthenticationProviderOptions["token"];
  private readonly introspectHandler: AuthenticationProviderOptions["introspect"];
  private readonly listGrantsHandler: AuthenticationProviderOptions["listGrants"];
  private readonly getGrantHandler: AuthenticationProviderOptions["getGrant"];
  private readonly revokeGrantHandler: AuthenticationProviderOptions["revokeGrant"];

  constructor(options: AuthenticationProviderOptions) {
    super(options);
    this.authorizeHandler = options.authorize;
    this.tokenHandler = options.token;
    this.introspectHandler = options.introspect;
    this.listGrantsHandler = options.listGrants;
    this.getGrantHandler = options.getGrant;
    this.revokeGrantHandler = options.revokeGrant;
  }

  async authorize(request: AuthorizeRequest): Promise<AuthorizeResponse> {
    return await this.authorizeHandler(request);
  }

  async token(request: TokenRequest): Promise<TokenResponse> {
    return await this.tokenHandler(request);
  }

  async introspect(request: IntrospectRequest): Promise<IntrospectResponse> {
    return await this.introspectHandler(request);
  }

  async listGrants(
    request: Record<string, never>,
    call: AuthCallContext,
  ): Promise<{ grantIds: string[] }> {
    return await this.listGrantsHandler(request, call);
  }

  async getGrant(
    request: { grantId: string },
    call: AuthCallContext,
  ): Promise<GrantDetails> {
    return await this.getGrantHandler(request, call);
  }

  async revokeGrant(
    request: { grantId: string },
    call: AuthCallContext,
  ): Promise<void> {
    await this.revokeGrantHandler(request, call);
  }
}

/**
 * Creates an authentication provider with the standard Gestalt runtime
 * contract.
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
      "authorize" in value &&
      "token" in value &&
      "introspect" in value &&
      "listGrants" in value &&
      "getGrant" in value &&
      "revokeGrant" in value)
  );
}
