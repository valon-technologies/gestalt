import type { MaybePromise } from "./api.ts";

/**
 * Credential record stored by the host.
 */
export interface ExternalCredential {
  id?: string;
  subjectId?: string;
  instance?: string;
  accessToken?: string;
  refreshToken?: string;
  scopes?: string;
  expiresAt?: Date;
  lastRefreshedAt?: Date;
  refreshErrorCount?: number;
  metadataJson?: string;
  createdAt?: Date;
  updatedAt?: Date;
  connectionId?: string;
}

/**
 * Selects a host-managed external credential.
 */
export interface ExternalCredentialLookup {
  subjectId?: string;
  instance?: string;
  connectionId?: string;
}

export interface UpsertExternalCredentialRequest {
  credential?: ExternalCredential;
  preserveTimestamps?: boolean;
}

export interface GetExternalCredentialRequest {
  lookup?: ExternalCredentialLookup;
}

export interface ListExternalCredentialsRequest {
  subjectId?: string;
  instance?: string;
  connectionId?: string;
}

export interface ListExternalCredentialsResponse {
  credentials: ExternalCredential[];
}

export interface DeleteExternalCredentialRequest {
  id: string;
}

export interface ExternalCredentialTokenExchangeDriver {
  type?: string;
  targetPrincipal?: string;
  scopes?: string[];
  lifetimeSeconds?: number;
  endpoint?: string;
  params?: Record<string, string>;
}

export interface ExternalCredentialAuthConfig {
  type?: string;
  token?: string;
  tokenPrefix?: string;
  grantType?: string;
  tokenUrl?: string;
  clientId?: string;
  clientSecret?: string;
  clientAuth?: string;
  tokenExchange?: string;
  scopes?: string[];
  scopeParam?: string;
  scopeSeparator?: string;
  tokenParams?: Record<string, string>;
  refreshParams?: Record<string, string>;
  acceptHeader?: string;
  accessTokenPath?: string;
  tokenExchangeDrivers?: ExternalCredentialTokenExchangeDriver[];
  refreshToken?: string;
}

export interface ValidateExternalCredentialConfigRequest {
  provider?: string;
  connection?: string;
  connectionId?: string;
  mode?: string;
  auth?: ExternalCredentialAuthConfig;
  connectionParams?: Record<string, string>;
}

export interface ResolveExternalCredentialRequest {
  provider?: string;
  connection?: string;
  connectionId?: string;
  mode?: string;
  credentialSubjectId?: string;
  actorSubjectId?: string;
  instance?: string;
  auth?: ExternalCredentialAuthConfig;
  connectionParams?: Record<string, string>;
}

export interface ResolveExternalCredentialResponse {
  token?: string;
  expiresAt?: Date;
  metadataJson?: string;
  params?: Record<string, string>;
  credential?: ExternalCredential;
}

export interface ExternalCredentialTokenResponse {
  accessToken?: string;
  refreshToken?: string;
  expiresIn?: number;
  tokenType?: string;
  extraJson?: string;
  refreshSource?: string;
}

export interface ExchangeExternalCredentialRequest {
  provider?: string;
  connection?: string;
  connectionId?: string;
  credentialSubjectId?: string;
  actorSubjectId?: string;
  instance?: string;
  auth?: ExternalCredentialAuthConfig;
  credentialJson?: string;
  connectionParams?: Record<string, string>;
}

export interface ExchangeExternalCredentialResponse {
  tokenResponse?: ExternalCredentialTokenResponse;
}

/**
 * Fakeable external-credential client contract.
 */
export interface ExternalCredentials {
  upsertCredential(
    request: UpsertExternalCredentialRequest,
  ): MaybePromise<ExternalCredential>;
  getCredential(
    request: GetExternalCredentialRequest,
  ): MaybePromise<ExternalCredential>;
  listCredentials(
    request: ListExternalCredentialsRequest,
  ): MaybePromise<ListExternalCredentialsResponse>;
  deleteCredential(request: DeleteExternalCredentialRequest): MaybePromise<void>;
  validateCredentialConfig(
    request: ValidateExternalCredentialConfigRequest,
  ): MaybePromise<void>;
  resolveCredential(
    request: ResolveExternalCredentialRequest,
  ): MaybePromise<ResolveExternalCredentialResponse>;
  exchangeCredential(
    request: ExchangeExternalCredentialRequest,
  ): MaybePromise<ExchangeExternalCredentialResponse>;
}
