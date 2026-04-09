import { existsSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http2";
import { dirname, resolve } from "node:path";

import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { EmptySchema, TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";

import {
  AuthProvider as AuthProviderService,
  AuthSessionSettingsSchema,
  AuthenticatedUserSchema,
  BeginLoginResponseSchema,
  type CompleteLoginRequest as AuthCompleteLoginRequest,
  type ValidateExternalTokenRequest,
} from "../gen/v1/auth_pb.ts";
import {
  DatastoreProvider as DatastoreProviderService,
  ListAPITokensResponseSchema,
  ListStoredIntegrationTokensResponseSchema,
  OAuthRegistrationSchema,
  RevokeAllAPITokensResponseSchema,
  StoredAPITokenSchema,
  StoredIntegrationTokenSchema,
  StoredUserSchema,
  type DeleteStoredIntegrationTokenRequest,
  type FindOrCreateUserRequest,
  type GetAPITokenByHashRequest,
  type GetStoredIntegrationTokenRequest,
  type GetUserRequest,
  type ListAPITokensRequest,
  type ListStoredIntegrationTokensRequest,
  type RevokeAPITokenRequest,
  type RevokeAllAPITokensRequest,
} from "../gen/v1/datastore_pb.ts";
import {
  SecretsProvider as SecretsProviderService,
  GetSecretResponseSchema,
  type GetSecretRequest,
} from "../gen/v1/secrets_pb.ts";
import {
  CatalogOperationSchema as ProtoCatalogOperationSchema,
  CatalogParameterSchema as ProtoCatalogParameterSchema,
  CatalogSchema as ProtoCatalogSchema,
  ConnectionMode as ProviderConnectionMode,
  GetSessionCatalogResponseSchema,
  OperationResultSchema,
  ProviderMetadataSchema,
  IntegrationProvider as IntegrationProviderService,
  StartProviderResponseSchema,
  type ExecuteRequest,
  type GetSessionCatalogRequest,
  type StartProviderRequest,
} from "../gen/v1/plugin_pb.ts";
import {
  ConfigureProviderResponseSchema,
  HealthCheckResponseSchema,
  ProviderIdentitySchema,
  ProviderKind as ProtoProviderKind,
  ProviderLifecycle,
  type ConfigureProviderRequest,
} from "../gen/v1/runtime_pb.ts";
import { errorMessage, type Request } from "./api.ts";
import { AuthProvider, isAuthProvider, type AuthenticatedUser } from "./auth.ts";
import {
  DatastoreProvider,
  isDatastoreProvider,
  type OAuthRegistration,
  type StoredAPIToken,
  type StoredIntegrationToken,
  type StoredUser,
} from "./datastore.ts";
import { SecretsProvider, isSecretsProvider } from "./secrets.ts";
import { catalogToYaml, type Catalog } from "./catalog.ts";
import {
  IntegrationProvider,
  connectionModeToProtoValue,
  connectionParamToProto,
  isIntegrationProvider,
} from "./plugin.ts";
import { type ProviderKind, slugName } from "./provider.ts";
import {
  defaultProviderName,
  formatProviderTarget,
  parseProviderTarget,
  readPackageConfig,
  readPackageProviderTarget,
  resolveProviderImportUrl,
} from "./target.ts";

export const ENV_PROVIDER_SOCKET = "GESTALT_PLUGIN_SOCKET";
export const ENV_PROVIDER_PARENT_PID = "GESTALT_PLUGIN_PARENT_PID";
export const ENV_WRITE_CATALOG = "GESTALT_PLUGIN_WRITE_CATALOG";
export const CURRENT_PROTOCOL_VERSION = 2;
export const USAGE = "usage: bun run runtime.ts ROOT PROVIDER_TARGET";

export type RuntimeArgs = {
  root: string;
  target: string;
};

export type LoadedProvider = IntegrationProvider | AuthProvider | DatastoreProvider | SecretsProvider;

export async function main(argv: string[] = process.argv.slice(2)): Promise<number> {
  const args = parseRuntimeArgs(argv);
  if (!args) {
    console.error(USAGE);
    return 2;
  }
  const provider = await loadProviderFromTarget(args.root, args.target);
  await runLoadedProvider(provider, {
    root: args.root,
  });
  return 0;
}

export function parseRuntimeArgs(argv: string[]): RuntimeArgs | undefined {
  if (argv.length !== 2) {
    return undefined;
  }
  return {
    root: argv[0]!,
    target: argv[1]!,
  };
}

export async function loadProviderFromTarget(
  root: string,
  rawTarget?: string,
): Promise<LoadedProvider> {
  const config = readPackageConfig(root);
  const targetValue =
    rawTarget?.trim() || formatProviderTarget(config.providerTarget ?? readPackageProviderTarget(root));
  const target = parseProviderTarget(targetValue);
  const module = await import(resolveProviderImportUrl(root, target));
  const candidate =
    (target.exportName ? module[target.exportName] : undefined) ??
    module.provider ??
    module.plugin ??
    module.default;

  const defaultName =
    slugName(config.name ?? "") || slugName(dirname(resolve(root, target.modulePath)));
  switch (target.kind) {
    case "integration": {
      if (!isIntegrationProvider(candidate)) {
        throw new Error(`${targetValue} did not resolve to a Gestalt integration provider`);
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "auth": {
      if (!isAuthProvider(candidate)) {
        throw new Error(`${targetValue} did not resolve to a Gestalt auth provider`);
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "datastore": {
      if (!isDatastoreProvider(candidate)) {
        throw new Error(`${targetValue} did not resolve to a Gestalt datastore provider`);
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "secrets": {
      if (!isSecretsProvider(candidate)) {
        throw new Error(`${targetValue} did not resolve to a Gestalt secrets provider`);
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    default:
      throw new Error(`TypeScript SDK does not yet support provider kind ${JSON.stringify(target.kind)}`);
  }
}

export async function loadPluginFromTarget(
  root: string,
  rawTarget?: string,
): Promise<IntegrationProvider> {
  const provider = await loadProviderFromTarget(root, rawTarget);
  if (!isIntegrationProvider(provider)) {
    throw new Error("target did not resolve to an integration provider");
  }
  return provider;
}

export async function runLoadedProvider(
  provider: LoadedProvider,
  options: {
    root?: string;
    providerName?: string;
  } = {},
): Promise<void> {
  if (options.providerName) {
    provider.name = slugName(options.providerName);
  } else if (!provider.name && options.root) {
    provider.resolveName(defaultProviderName(options.root));
  }

  const catalogPath = process.env[ENV_WRITE_CATALOG];
  if (catalogPath) {
    if (!isIntegrationProvider(provider)) {
      throw new Error("static catalog generation is only supported for integration providers");
    }
    writeFileSync(catalogPath, pluginCatalogYaml(provider), "utf8");
    return;
  }

  await serve(provider);
}

export async function runLoadedPlugin(
  plugin: IntegrationProvider,
  options: {
    root?: string;
    pluginName?: string;
  } = {},
): Promise<void> {
  const runtimeOptions: {
    root?: string;
    providerName?: string;
  } = {};
  if (options.root !== undefined) {
    runtimeOptions.root = options.root;
  }
  if (options.pluginName !== undefined) {
    runtimeOptions.providerName = options.pluginName;
  }
  await runLoadedProvider(plugin, runtimeOptions);
}

export async function runBundledProvider(
  provider: unknown,
  kind: ProviderKind,
  providerName: string,
): Promise<void> {
  let loaded: LoadedProvider;
  switch (kind) {
    case "integration":
      if (!isIntegrationProvider(provider)) {
        throw new Error("bundled target did not resolve to a Gestalt integration provider");
      }
      loaded = provider;
      break;
    case "auth":
      if (!isAuthProvider(provider)) {
        throw new Error("bundled target did not resolve to a Gestalt auth provider");
      }
      loaded = provider;
      break;
    case "datastore":
      if (!isDatastoreProvider(provider)) {
        throw new Error("bundled target did not resolve to a Gestalt datastore provider");
      }
      loaded = provider;
      break;
    case "secrets":
      if (!isSecretsProvider(provider)) {
        throw new Error("bundled target did not resolve to a Gestalt secrets provider");
      }
      loaded = provider;
      break;
    default:
      throw new Error(`TypeScript SDK does not yet support provider kind ${JSON.stringify(kind)}`);
  }
  loaded.name = slugName(providerName);
  await runLoadedProvider(loaded, {
    providerName,
  });
}

export async function runBundledPlugin(plugin: unknown, pluginName: string): Promise<void> {
  await runBundledProvider(plugin, "integration", pluginName);
}

export async function serve(provider: LoadedProvider): Promise<void> {
  const socketPath = process.env[ENV_PROVIDER_SOCKET];
  if (!socketPath) {
    throw new Error(`${ENV_PROVIDER_SOCKET} is required`);
  }
  if (existsSync(socketPath)) {
    rmSync(socketPath);
  }

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(ProviderLifecycle, createRuntimeService(provider));
      if (isIntegrationProvider(provider)) {
        router.service(IntegrationProviderService, createProviderService(provider));
      } else if (isAuthProvider(provider)) {
        router.service(AuthProviderService, createAuthService(provider));
      } else if (isDatastoreProvider(provider)) {
        router.service(DatastoreProviderService, createDatastoreService(provider));
      } else if (isSecretsProvider(provider)) {
        router.service(SecretsProviderService, createSecretsService(provider));
      }
    },
  });

  const server = createServer(handler);
  let shutdownError: unknown;
  let closePromise: Promise<void> | undefined;
  const close = () => {
    closePromise ??= (async () => {
      try {
        await provider.closeProvider();
      } catch (error) {
        shutdownError = error;
      } finally {
        try {
          await new Promise<void>((resolveClose) => {
            server.close(() => resolveClose());
          });
        } finally {
          if (existsSync(socketPath)) {
            rmSync(socketPath);
          }
        }
      }
    })();
    return closePromise;
  };

  await new Promise<void>((resolveListen, rejectListen) => {
    server.once("error", rejectListen);
    server.listen(socketPath, () => {
      server.off("error", rejectListen);
      resolveListen();
    });
  });

  const shutdown = () => {
    void close();
  };
  process.once("SIGINT", shutdown);
  process.once("SIGTERM", shutdown);

  await new Promise<void>((resolveClose, rejectClose) => {
    server.once("close", resolveClose);
    server.once("error", rejectClose);
  });
  if (shutdownError) {
    throw shutdownError;
  }
}

export function createRuntimeService(
  provider: LoadedProvider,
): Partial<ServiceImpl<typeof ProviderLifecycle>> {
  return {
    async getProviderIdentity() {
      return create(ProviderIdentitySchema, {
        kind: providerKindToProto(provider.kind),
        name: provider.name,
        displayName: provider.displayName,
        description: provider.description,
        version: provider.version,
        warnings: await provider.warnings(),
        minProtocolVersion: CURRENT_PROTOCOL_VERSION,
        maxProtocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
    async configureProvider(request: ConfigureProviderRequest) {
      if (request.protocolVersion !== CURRENT_PROTOCOL_VERSION) {
        throw new ConnectError(
          `host requested protocol version ${request.protocolVersion}, provider requires ${CURRENT_PROTOCOL_VERSION}`,
          Code.FailedPrecondition,
        );
      }
      try {
        await provider.configureProvider(request.name, objectFromUnknown(request.config));
      } catch (error) {
        throw new ConnectError(`configure provider: ${errorMessage(error)}`, Code.Unknown);
      }
      return create(ConfigureProviderResponseSchema, {
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
    async healthCheck() {
      if (!provider.supportsHealthCheck()) {
        if (provider.kind === "datastore") {
          return create(HealthCheckResponseSchema, {
            ready: false,
            message: "datastore provider must implement HealthChecker",
          });
        }
        return create(HealthCheckResponseSchema, {
          ready: true,
        });
      }
      try {
        await provider.healthCheck();
        return create(HealthCheckResponseSchema, {
          ready: true,
        });
      } catch (error) {
        return create(HealthCheckResponseSchema, {
          ready: false,
          message: errorMessage(error),
        });
      }
    },
  };
}

export function createProviderService(
  provider: IntegrationProvider,
): Partial<ServiceImpl<typeof IntegrationProviderService>> {
  return {
    getMetadata() {
      return create(ProviderMetadataSchema, {
        name: provider.name,
        displayName: provider.displayName,
        description: provider.description,
        connectionMode: connectionModeToProtoValue(provider.connectionMode) as ProviderConnectionMode,
        authTypes: [...provider.authTypes],
        connectionParams: Object.fromEntries(
          Object.entries(provider.connectionParams).map(([key, value]) => [
            key,
            connectionParamToProto(value),
          ]),
        ),
        staticCatalog: catalogToProto(provider.staticCatalog()),
        supportsSessionCatalog: provider.supportsSessionCatalog(),
        supportsPostConnect: false,
        minProtocolVersion: CURRENT_PROTOCOL_VERSION,
        maxProtocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
    async startProvider(request: StartProviderRequest) {
      try {
        await provider.configureProvider(request.name, objectFromUnknown(request.config));
      } catch (error) {
        throw new ConnectError(`configure provider: ${errorMessage(error)}`, Code.Unknown);
      }
      return create(StartProviderResponseSchema, {
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
    async execute(request: ExecuteRequest) {
      return create(
        OperationResultSchema,
        await provider.execute(
          request.operation,
          objectFromUnknown(request.params),
          providerRequest(request.token, request.connectionParams),
        ),
      );
    },
    async getSessionCatalog(request: GetSessionCatalogRequest) {
      let catalog: Catalog | Record<string, unknown> | null | undefined;
      try {
        catalog = await provider.catalogForRequest(
          providerRequest(request.token, request.connectionParams),
        );
      } catch (error) {
        throw new ConnectError(`session catalog: ${errorMessage(error)}`, Code.Unknown);
      }
      if (!catalog) {
        throw new ConnectError("provider does not support session catalogs", Code.Unimplemented);
      }
      return create(GetSessionCatalogResponseSchema, {
        catalog: catalogToProto(catalog),
      });
    },
    async postConnect() {
      throw new ConnectError("provider does not support post connect", Code.Unimplemented);
    },
  };
}

export function createAuthService(
  provider: AuthProvider,
): Partial<ServiceImpl<typeof AuthProviderService>> {
  return {
    async beginLogin(request) {
      const response = await provider.beginLogin({
        callbackUrl: request.callbackUrl,
        hostState: request.hostState,
        scopes: [...request.scopes],
        options: {
          ...request.options,
        },
      });
      if (!response) {
        throw new ConnectError("auth provider returned nil response", Code.Internal);
      }
      return create(BeginLoginResponseSchema, {
        authorizationUrl: response.authorizationUrl,
        providerState: response.providerState ?? new Uint8Array(),
      });
    },
    async completeLogin(request: AuthCompleteLoginRequest) {
      const user = await provider.completeLogin({
        query: {
          ...request.query,
        },
        providerState: cloneUint8Array(request.providerState),
        callbackUrl: request.callbackUrl,
      });
      if (!user) {
        throw new ConnectError("auth provider returned nil user", Code.Internal);
      }
      return authenticatedUserToProto(user);
    },
    async validateExternalToken(request: ValidateExternalTokenRequest) {
      if (!provider.supportsExternalTokenValidation()) {
        throw new ConnectError(
          "auth provider does not support external token validation",
          Code.Unimplemented,
        );
      }
      const user = await provider.validateExternalToken(request.token);
      if (!user) {
        throw new ConnectError("token not recognized", Code.NotFound);
      }
      return authenticatedUserToProto(user);
    },
    async getSessionSettings() {
      if (!provider.supportsSessionSettings()) {
        throw new ConnectError(
          "auth provider does not expose session settings",
          Code.Unimplemented,
        );
      }
      const settings = await provider.sessionSettings();
      return create(AuthSessionSettingsSchema, {
        sessionTtlSeconds: normalizeBigInt(settings?.sessionTtlSeconds ?? 0),
      });
    },
  };
}

export function createDatastoreService(
  provider: DatastoreProvider,
): Partial<ServiceImpl<typeof DatastoreProviderService>> {
  return {
    async migrate() {
      await provider.migrate();
      return create(EmptySchema, {});
    },
    async getUser(request: GetUserRequest) {
      const user = await provider.getUser(request.id);
      if (!user) {
        throw new ConnectError("user not found", Code.NotFound);
      }
      return storedUserToProto(user);
    },
    async findOrCreateUser(request: FindOrCreateUserRequest) {
      const user = await provider.findOrCreateUser(request.email);
      if (!user) {
        throw new ConnectError("datastore provider returned nil user", Code.Internal);
      }
      return storedUserToProto(user);
    },
    async putStoredIntegrationToken(request) {
      await provider.putIntegrationToken(storedIntegrationTokenFromProto(request));
      return create(EmptySchema, {});
    },
    async getStoredIntegrationToken(request: GetStoredIntegrationTokenRequest) {
      const token = await provider.getIntegrationToken(
        request.userId,
        request.integration,
        request.connection,
        request.instance,
      );
      if (!token) {
        throw new ConnectError("integration token not found", Code.NotFound);
      }
      return storedIntegrationTokenToProto(token);
    },
    async listStoredIntegrationTokens(request: ListStoredIntegrationTokensRequest) {
      const tokens = await provider.listIntegrationTokens(
        request.userId,
        request.integration,
        request.connection,
      );
      return create(ListStoredIntegrationTokensResponseSchema, {
        tokens: tokens.map(storedIntegrationTokenToProto),
      });
    },
    async deleteStoredIntegrationToken(request: DeleteStoredIntegrationTokenRequest) {
      await provider.deleteIntegrationToken(request.id);
      return create(EmptySchema, {});
    },
    async putAPIToken(request) {
      await provider.putApiToken(storedApiTokenFromProto(request));
      return create(EmptySchema, {});
    },
    async getAPITokenByHash(request: GetAPITokenByHashRequest) {
      const token = await provider.getApiTokenByHash(request.hashedToken);
      if (!token) {
        throw new ConnectError("api token not found", Code.NotFound);
      }
      return storedApiTokenToProto(token);
    },
    async listAPITokens(request: ListAPITokensRequest) {
      const tokens = await provider.listApiTokens(request.userId);
      return create(ListAPITokensResponseSchema, {
        tokens: tokens.map(storedApiTokenToProto),
      });
    },
    async revokeAPIToken(request: RevokeAPITokenRequest) {
      await provider.revokeApiToken(request.userId, request.id);
      return create(EmptySchema, {});
    },
    async revokeAllAPITokens(request: RevokeAllAPITokensRequest) {
      return create(RevokeAllAPITokensResponseSchema, {
        revoked: normalizeBigInt(await provider.revokeAllApiTokens(request.userId)),
      });
    },
    async getOAuthRegistration(request) {
      if (!provider.supportsOAuthRegistrations()) {
        throw new ConnectError(
          "datastore provider does not support oauth registrations",
          Code.Unimplemented,
        );
      }
      const registration = await provider.getOAuthRegistration(
        request.authServerUrl,
        request.redirectUri,
      );
      if (!registration) {
        throw new ConnectError("oauth registration not found", Code.NotFound);
      }
      return oauthRegistrationToProto(registration);
    },
    async putOAuthRegistration(request) {
      if (!provider.supportsOAuthRegistrations()) {
        throw new ConnectError(
          "datastore provider does not support oauth registrations",
          Code.Unimplemented,
        );
      }
      await provider.putOAuthRegistration(oauthRegistrationFromProto(request));
      return create(EmptySchema, {});
    },
    async deleteOAuthRegistration(request) {
      if (!provider.supportsOAuthRegistrations()) {
        throw new ConnectError(
          "datastore provider does not support oauth registrations",
          Code.Unimplemented,
        );
      }
      await provider.deleteOAuthRegistration(request.authServerUrl, request.redirectUri);
      return create(EmptySchema, {});
    },
  };
}

export function createSecretsService(
  provider: SecretsProvider,
): Partial<ServiceImpl<typeof SecretsProviderService>> {
  return {
    async getSecret(request: GetSecretRequest) {
      const value = await provider.getSecret(request.name);
      return create(GetSecretResponseSchema, {
        value,
      });
    },
  };
}

export function pluginCatalogYaml(plugin: IntegrationProvider): string {
  return catalogToYaml(plugin.staticCatalog());
}

function providerRequest(token: string, connectionParams: Record<string, string>): Request {
  return {
    token,
    connectionParams: {
      ...connectionParams,
    },
  };
}

function providerKindToProto(kind: ProviderKind): ProtoProviderKind {
  switch (kind) {
    case "integration":
      return ProtoProviderKind.INTEGRATION;
    case "auth":
      return ProtoProviderKind.AUTH;
    case "datastore":
      return ProtoProviderKind.DATASTORE;
    case "secrets":
      return ProtoProviderKind.SECRETS;
    case "telemetry":
      return ProtoProviderKind.TELEMETRY;
    default:
      return ProtoProviderKind.UNSPECIFIED;
  }
}

function objectFromUnknown(value: unknown): Record<string, unknown> {
  if (typeof value === "object" && value !== null && !Array.isArray(value)) {
    return {
      ...(value as Record<string, unknown>),
    };
  }
  return {};
}

function catalogToProto(catalog: Catalog | Record<string, unknown>) {
  const typed = catalog as Catalog;
  return create(ProtoCatalogSchema, {
    name: typed.name ?? "",
    displayName: typed.displayName ?? "",
    description: typed.description ?? "",
    iconSvg: typed.iconSvg ?? "",
    operations: (typed.operations ?? []).map((op) => {
      const protoOp = create(ProtoCatalogOperationSchema, {
        id: op.id,
        method: op.method,
        title: op.title ?? "",
        description: op.description ?? "",
        tags: op.tags ?? [],
        readOnly: op.readOnly ?? false,
        parameters: (op.parameters ?? []).map((p) =>
          create(ProtoCatalogParameterSchema, {
            name: p.name,
            type: p.type,
            description: p.description ?? "",
            required: p.required ?? false,
          }),
        ),
      });
      if (op.visible !== undefined) {
        protoOp.visible = op.visible;
      }
      return protoOp;
    }),
  });
}

function authenticatedUserToProto(user: AuthenticatedUser) {
  return create(AuthenticatedUserSchema, {
    subject: user.subject,
    email: user.email ?? "",
    emailVerified: user.emailVerified ?? false,
    displayName: user.displayName ?? "",
    avatarUrl: user.avatarUrl ?? "",
    claims: {
      ...(user.claims ?? {}),
    },
  });
}

function storedUserToProto(user: StoredUser) {
  return create(StoredUserSchema, {
    id: user.id,
    email: user.email,
    displayName: user.displayName ?? "",
    ...(user.createdAt ? { createdAt: timestampFromDate(user.createdAt)! } : {}),
    ...(user.updatedAt ? { updatedAt: timestampFromDate(user.updatedAt)! } : {}),
  });
}

function storedIntegrationTokenToProto(token: StoredIntegrationToken) {
  return create(StoredIntegrationTokenSchema, {
    id: token.id,
    userId: token.userId,
    integration: token.integration,
    connection: token.connection,
    instance: token.instance,
    accessTokenSealed: cloneUint8Array(token.accessTokenSealed),
    refreshTokenSealed: cloneUint8Array(token.refreshTokenSealed),
    scopes: token.scopes,
    refreshErrorCount: token.refreshErrorCount ?? 0,
    connectionParams: {
      ...(token.connectionParams ?? {}),
    },
    ...(token.expiresAt ? { expiresAt: timestampFromDate(token.expiresAt)! } : {}),
    ...(token.lastRefreshedAt ? { lastRefreshedAt: timestampFromDate(token.lastRefreshedAt)! } : {}),
    ...(token.createdAt ? { createdAt: timestampFromDate(token.createdAt)! } : {}),
    ...(token.updatedAt ? { updatedAt: timestampFromDate(token.updatedAt)! } : {}),
  });
}

function storedIntegrationTokenFromProto(
  token: ReturnType<typeof create<typeof StoredIntegrationTokenSchema>>,
): StoredIntegrationToken {
  const result: StoredIntegrationToken = {
    id: token.id,
    userId: token.userId,
    integration: token.integration,
    connection: token.connection,
    instance: token.instance,
    accessTokenSealed: cloneUint8Array(token.accessTokenSealed),
    refreshTokenSealed: cloneUint8Array(token.refreshTokenSealed),
    scopes: token.scopes,
    refreshErrorCount: token.refreshErrorCount,
    connectionParams: {
      ...token.connectionParams,
    },
  };
  const expiresAt = dateFromTimestamp(token.expiresAt);
  if (expiresAt) {
    result.expiresAt = expiresAt;
  }
  const lastRefreshedAt = dateFromTimestamp(token.lastRefreshedAt);
  if (lastRefreshedAt) {
    result.lastRefreshedAt = lastRefreshedAt;
  }
  const createdAt = dateFromTimestamp(token.createdAt);
  if (createdAt) {
    result.createdAt = createdAt;
  }
  const updatedAt = dateFromTimestamp(token.updatedAt);
  if (updatedAt) {
    result.updatedAt = updatedAt;
  }
  return result;
}

function storedApiTokenToProto(token: StoredAPIToken) {
  return create(StoredAPITokenSchema, {
    id: token.id,
    userId: token.userId,
    name: token.name,
    hashedToken: token.hashedToken,
    scopes: token.scopes,
    ...(token.expiresAt ? { expiresAt: timestampFromDate(token.expiresAt)! } : {}),
    ...(token.createdAt ? { createdAt: timestampFromDate(token.createdAt)! } : {}),
    ...(token.updatedAt ? { updatedAt: timestampFromDate(token.updatedAt)! } : {}),
  });
}

function storedApiTokenFromProto(
  token: ReturnType<typeof create<typeof StoredAPITokenSchema>>,
): StoredAPIToken {
  const result: StoredAPIToken = {
    id: token.id,
    userId: token.userId,
    name: token.name,
    hashedToken: token.hashedToken,
    scopes: token.scopes,
  };
  const expiresAt = dateFromTimestamp(token.expiresAt);
  if (expiresAt) {
    result.expiresAt = expiresAt;
  }
  const createdAt = dateFromTimestamp(token.createdAt);
  if (createdAt) {
    result.createdAt = createdAt;
  }
  const updatedAt = dateFromTimestamp(token.updatedAt);
  if (updatedAt) {
    result.updatedAt = updatedAt;
  }
  return result;
}

function oauthRegistrationToProto(registration: OAuthRegistration) {
  return create(OAuthRegistrationSchema, {
    authServerUrl: registration.authServerUrl,
    redirectUri: registration.redirectUri,
    clientId: registration.clientId,
    clientSecretSealed: cloneUint8Array(registration.clientSecretSealed),
    authorizationEndpoint: registration.authorizationEndpoint ?? "",
    tokenEndpoint: registration.tokenEndpoint ?? "",
    scopesSupported: registration.scopesSupported ?? "",
    ...(registration.expiresAt ? { expiresAt: timestampFromDate(registration.expiresAt)! } : {}),
    ...(registration.discoveredAt
      ? { discoveredAt: timestampFromDate(registration.discoveredAt)! }
      : {}),
  });
}

function oauthRegistrationFromProto(
  registration: ReturnType<typeof create<typeof OAuthRegistrationSchema>>,
): OAuthRegistration {
  const result: OAuthRegistration = {
    authServerUrl: registration.authServerUrl,
    redirectUri: registration.redirectUri,
    clientId: registration.clientId,
    clientSecretSealed: cloneUint8Array(registration.clientSecretSealed),
  };
  if (registration.authorizationEndpoint) {
    result.authorizationEndpoint = registration.authorizationEndpoint;
  }
  if (registration.tokenEndpoint) {
    result.tokenEndpoint = registration.tokenEndpoint;
  }
  if (registration.scopesSupported) {
    result.scopesSupported = registration.scopesSupported;
  }
  const expiresAt = dateFromTimestamp(registration.expiresAt);
  if (expiresAt) {
    result.expiresAt = expiresAt;
  }
  const discoveredAt = dateFromTimestamp(registration.discoveredAt);
  if (discoveredAt) {
    result.discoveredAt = discoveredAt;
  }
  return result;
}

function timestampFromDate(value: Date | undefined): Timestamp | undefined {
  if (!value) {
    return undefined;
  }
  const millis = value.getTime();
  const seconds = Math.floor(millis / 1000);
  const nanos = Math.trunc((millis - seconds * 1000) * 1_000_000);
  return create(TimestampSchema, {
    seconds: BigInt(seconds),
    nanos,
  });
}

function dateFromTimestamp(value: Timestamp | undefined): Date | undefined {
  if (!value) {
    return undefined;
  }
  return new Date(Number(value.seconds) * 1000 + Math.trunc(value.nanos / 1_000_000));
}

function normalizeBigInt(value: number | bigint): bigint {
  if (typeof value === "bigint") {
    return value < 0n ? 0n : value;
  }
  if (!Number.isFinite(value)) {
    return 0n;
  }
  return BigInt(Math.max(0, Math.trunc(value)));
}

function cloneUint8Array(value: Uint8Array | undefined): Uint8Array {
  if (!value) {
    return new Uint8Array();
  }
  return new Uint8Array(value);
}

if (import.meta.main) {
  void main().then(
    (code) => {
      process.exitCode = code;
    },
    (error: unknown) => {
      console.error(error instanceof Error ? error.stack ?? error.message : String(error));
      process.exitCode = 1;
    },
  );
}
