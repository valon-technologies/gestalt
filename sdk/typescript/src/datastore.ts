import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

export interface StoredUser {
  id: string;
  email: string;
  displayName?: string;
  createdAt?: Date;
  updatedAt?: Date;
}

export interface StoredIntegrationToken {
  id: string;
  userId: string;
  integration: string;
  connection: string;
  instance: string;
  accessTokenSealed: Uint8Array;
  refreshTokenSealed: Uint8Array;
  scopes: string;
  expiresAt?: Date;
  lastRefreshedAt?: Date;
  refreshErrorCount?: number;
  connectionParams?: Record<string, string>;
  createdAt?: Date;
  updatedAt?: Date;
}

export interface StoredAPIToken {
  id: string;
  userId: string;
  name: string;
  hashedToken: string;
  scopes: string;
  expiresAt?: Date;
  createdAt?: Date;
  updatedAt?: Date;
}

export interface OAuthRegistration {
  authServerUrl: string;
  redirectUri: string;
  clientId: string;
  clientSecretSealed: Uint8Array;
  expiresAt?: Date;
  authorizationEndpoint?: string;
  tokenEndpoint?: string;
  scopesSupported?: string;
  discoveredAt?: Date;
}

export interface DatastoreProviderOptions extends RuntimeProviderOptions {
  migrate: () => MaybePromise<void>;
  getUser: (id: string) => MaybePromise<StoredUser | null | undefined>;
  findOrCreateUser: (email: string) => MaybePromise<StoredUser>;
  putIntegrationToken: (token: StoredIntegrationToken) => MaybePromise<void>;
  getIntegrationToken: (
    userId: string,
    integration: string,
    connection: string,
    instance: string,
  ) => MaybePromise<StoredIntegrationToken | null | undefined>;
  listIntegrationTokens: (
    userId: string,
    integration: string,
    connection: string,
  ) => MaybePromise<StoredIntegrationToken[]>;
  deleteIntegrationToken: (id: string) => MaybePromise<void>;
  putApiToken: (token: StoredAPIToken) => MaybePromise<void>;
  getApiTokenByHash: (hashedToken: string) => MaybePromise<StoredAPIToken | null | undefined>;
  listApiTokens: (userId: string) => MaybePromise<StoredAPIToken[]>;
  revokeApiToken: (userId: string, id: string) => MaybePromise<void>;
  revokeAllApiTokens: (userId: string) => MaybePromise<number | bigint>;
  getOAuthRegistration?: (
    authServerUrl: string,
    redirectUri: string,
  ) => MaybePromise<OAuthRegistration | null | undefined>;
  putOAuthRegistration?: (registration: OAuthRegistration) => MaybePromise<void>;
  deleteOAuthRegistration?: (
    authServerUrl: string,
    redirectUri: string,
  ) => MaybePromise<void>;
}

export class DatastoreProvider extends RuntimeProvider {
  readonly kind = "datastore" as const;

  private readonly migrateHandler: DatastoreProviderOptions["migrate"];
  private readonly getUserHandler: DatastoreProviderOptions["getUser"];
  private readonly findOrCreateUserHandler: DatastoreProviderOptions["findOrCreateUser"];
  private readonly putIntegrationTokenHandler: DatastoreProviderOptions["putIntegrationToken"];
  private readonly getIntegrationTokenHandler: DatastoreProviderOptions["getIntegrationToken"];
  private readonly listIntegrationTokensHandler: DatastoreProviderOptions["listIntegrationTokens"];
  private readonly deleteIntegrationTokenHandler: DatastoreProviderOptions["deleteIntegrationToken"];
  private readonly putApiTokenHandler: DatastoreProviderOptions["putApiToken"];
  private readonly getApiTokenByHashHandler: DatastoreProviderOptions["getApiTokenByHash"];
  private readonly listApiTokensHandler: DatastoreProviderOptions["listApiTokens"];
  private readonly revokeApiTokenHandler: DatastoreProviderOptions["revokeApiToken"];
  private readonly revokeAllApiTokensHandler: DatastoreProviderOptions["revokeAllApiTokens"];
  private readonly getOAuthRegistrationHandler: DatastoreProviderOptions["getOAuthRegistration"];
  private readonly putOAuthRegistrationHandler: DatastoreProviderOptions["putOAuthRegistration"];
  private readonly deleteOAuthRegistrationHandler: DatastoreProviderOptions["deleteOAuthRegistration"];

  constructor(options: DatastoreProviderOptions) {
    super(options);
    this.migrateHandler = options.migrate;
    this.getUserHandler = options.getUser;
    this.findOrCreateUserHandler = options.findOrCreateUser;
    this.putIntegrationTokenHandler = options.putIntegrationToken;
    this.getIntegrationTokenHandler = options.getIntegrationToken;
    this.listIntegrationTokensHandler = options.listIntegrationTokens;
    this.deleteIntegrationTokenHandler = options.deleteIntegrationToken;
    this.putApiTokenHandler = options.putApiToken;
    this.getApiTokenByHashHandler = options.getApiTokenByHash;
    this.listApiTokensHandler = options.listApiTokens;
    this.revokeApiTokenHandler = options.revokeApiToken;
    this.revokeAllApiTokensHandler = options.revokeAllApiTokens;
    this.getOAuthRegistrationHandler = options.getOAuthRegistration;
    this.putOAuthRegistrationHandler = options.putOAuthRegistration;
    this.deleteOAuthRegistrationHandler = options.deleteOAuthRegistration;
  }

  async migrate(): Promise<void> {
    await this.migrateHandler();
  }

  async getUser(id: string): Promise<StoredUser | null | undefined> {
    return await this.getUserHandler(id);
  }

  async findOrCreateUser(email: string): Promise<StoredUser> {
    return await this.findOrCreateUserHandler(email);
  }

  async putIntegrationToken(token: StoredIntegrationToken): Promise<void> {
    await this.putIntegrationTokenHandler(token);
  }

  async getIntegrationToken(
    userId: string,
    integration: string,
    connection: string,
    instance: string,
  ): Promise<StoredIntegrationToken | null | undefined> {
    return await this.getIntegrationTokenHandler(userId, integration, connection, instance);
  }

  async listIntegrationTokens(
    userId: string,
    integration: string,
    connection: string,
  ): Promise<StoredIntegrationToken[]> {
    return await this.listIntegrationTokensHandler(userId, integration, connection);
  }

  async deleteIntegrationToken(id: string): Promise<void> {
    await this.deleteIntegrationTokenHandler(id);
  }

  async putApiToken(token: StoredAPIToken): Promise<void> {
    await this.putApiTokenHandler(token);
  }

  async getApiTokenByHash(hashedToken: string): Promise<StoredAPIToken | null | undefined> {
    return await this.getApiTokenByHashHandler(hashedToken);
  }

  async listApiTokens(userId: string): Promise<StoredAPIToken[]> {
    return await this.listApiTokensHandler(userId);
  }

  async revokeApiToken(userId: string, id: string): Promise<void> {
    await this.revokeApiTokenHandler(userId, id);
  }

  async revokeAllApiTokens(userId: string): Promise<number | bigint> {
    return await this.revokeAllApiTokensHandler(userId);
  }

  supportsOAuthRegistrations(): boolean {
    return (
      this.getOAuthRegistrationHandler !== undefined &&
      this.putOAuthRegistrationHandler !== undefined &&
      this.deleteOAuthRegistrationHandler !== undefined
    );
  }

  async getOAuthRegistration(
    authServerUrl: string,
    redirectUri: string,
  ): Promise<OAuthRegistration | null | undefined> {
    return await this.getOAuthRegistrationHandler?.(authServerUrl, redirectUri);
  }

  async putOAuthRegistration(registration: OAuthRegistration): Promise<void> {
    await this.putOAuthRegistrationHandler?.(registration);
  }

  async deleteOAuthRegistration(authServerUrl: string, redirectUri: string): Promise<void> {
    await this.deleteOAuthRegistrationHandler?.(authServerUrl, redirectUri);
  }
}

export function defineDatastoreProvider(options: DatastoreProviderOptions): DatastoreProvider {
  return new DatastoreProvider(options);
}

export function isDatastoreProvider(value: unknown): value is DatastoreProvider {
  return (
    value instanceof DatastoreProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "datastore" &&
      "migrate" in value &&
      "getUser" in value &&
      "findOrCreateUser" in value)
  );
}
