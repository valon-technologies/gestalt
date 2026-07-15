import { ProviderBase, type ProviderBaseOptions } from "../provider.ts";
import type { MaybePromise } from "../api.ts";

/** gRPC metadata key for the caller bearer token on grant-management RPCs. */
export const CALLER_BEARER_TOKEN_METADATA_KEY =
  "x-gestalt-caller-bearer-token";

/**
 * Call context passed to grant-management handlers.
 */
export interface IdentityCallContext {
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
  /**
   * Gestalt request-side extension for the desired access-token lifetime in
   * seconds. The provider MAY clamp or default it; expiresIn in TokenResponse
   * remains authoritative per RFC 6749 §5.1. Undefined/0 means use the grant
   * default.
   */
  expiresIn?: bigint | number;
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
 * Runtime hooks required to implement a Gestalt identity provider.
 */
export interface IdentityProviderOptions extends ProviderBaseOptions {
  authorize: (request: AuthorizeRequest) => MaybePromise<AuthorizeResponse>;
  token: (request: TokenRequest) => MaybePromise<TokenResponse>;
  introspect: (request: IntrospectRequest) => MaybePromise<IntrospectResponse>;
  userInfo: (
    request: Record<string, never>,
    call: IdentityCallContext,
  ) => MaybePromise<{
    subjectId: string;
    email?: string;
    name?: string;
  }>;
  listGrants: (
    request: Record<string, never>,
    call: IdentityCallContext,
  ) => MaybePromise<{ grantIds: string[] }>;
  getGrant: (
    request: { grantId: string },
    call: IdentityCallContext,
  ) => MaybePromise<GrantDetails>;
  revokeGrant: (
    request: { grantId: string },
    call: IdentityCallContext,
  ) => MaybePromise<void>;
}

/**
 * Identity provider implementation consumed by the Gestalt runtime.
 */
export class IdentityProvider extends ProviderBase {
  readonly kind = "identity" as const;

  private readonly authorizeHandler: IdentityProviderOptions["authorize"];
  private readonly tokenHandler: IdentityProviderOptions["token"];
  private readonly introspectHandler: IdentityProviderOptions["introspect"];
  private readonly userInfoHandler: IdentityProviderOptions["userInfo"];
  private readonly listGrantsHandler: IdentityProviderOptions["listGrants"];
  private readonly getGrantHandler: IdentityProviderOptions["getGrant"];
  private readonly revokeGrantHandler: IdentityProviderOptions["revokeGrant"];

  constructor(options: IdentityProviderOptions) {
    super(options, "identity");
    this.authorizeHandler = options.authorize;
    this.tokenHandler = options.token;
    this.introspectHandler = options.introspect;
    this.userInfoHandler = options.userInfo;
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

  async userInfo(
    request: Record<string, never>,
    call: IdentityCallContext,
  ): Promise<{ subjectId: string; email?: string; name?: string }> {
    return await this.userInfoHandler(request, call);
  }

  async listGrants(
    request: Record<string, never>,
    call: IdentityCallContext,
  ): Promise<{ grantIds: string[] }> {
    return await this.listGrantsHandler(request, call);
  }

  async getGrant(
    request: { grantId: string },
    call: IdentityCallContext,
  ): Promise<GrantDetails> {
    return await this.getGrantHandler(request, call);
  }

  async revokeGrant(
    request: { grantId: string },
    call: IdentityCallContext,
  ): Promise<void> {
    await this.revokeGrantHandler(request, call);
  }
}

/**
 * Creates an identity provider with the standard Gestalt runtime contract.
 */
export function defineIdentityProvider(
  options: IdentityProviderOptions,
): IdentityProvider {
  return new IdentityProvider(options);
}

/**
 * Runtime type guard for identity providers loaded from user modules.
 */
export function isIdentityProvider(value: unknown): value is IdentityProvider {
  return (
    value instanceof IdentityProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      String((value as { kind?: unknown }).kind ?? "") === "identity" &&
      "authorize" in value &&
      "token" in value &&
      "introspect" in value &&
      "listGrants" in value &&
      "getGrant" in value &&
      "revokeGrant" in value)
  );
}
