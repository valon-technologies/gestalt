import { existsSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http2";
import { dirname, resolve } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
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
  Cache as CacheService,
  CacheDeleteManyResponseSchema,
  CacheDeleteResponseSchema,
  CacheGetManyResponseSchema,
  CacheGetResponseSchema,
  CacheResultSchema,
  CacheTouchResponseSchema,
} from "../gen/v1/cache_pb.ts";
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
  type RequestContext as ProtoRequestContext,
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
import { S3 as S3Service } from "../gen/v1/s3_pb.ts";
import { errorMessage, type Request } from "./api.ts";
import {
  AuthProvider,
  isAuthProvider,
  type AuthenticatedUser,
} from "./auth.ts";
import { CacheProvider, isCacheProvider } from "./cache.ts";
import { SecretsProvider, isSecretsProvider } from "./secrets.ts";
import { catalogToYaml, type Catalog } from "./catalog.ts";
import {
  IntegrationProvider,
  connectionModeToProtoValue,
  connectionParamToProto,
  isIntegrationProvider,
} from "./plugin.ts";
import { type ProviderKind, slugName } from "./provider.ts";
import { S3Provider, createS3Service, isS3Provider } from "./s3.ts";
import {
  defaultProviderName,
  formatProviderTarget,
  parseProviderTarget,
  readPackageConfig,
  readPackageProviderTarget,
  resolveProviderImportUrl,
} from "./target.ts";

/**
 * Environment variable containing the Unix socket path for a running provider.
 */
export const ENV_PROVIDER_SOCKET = "GESTALT_PLUGIN_SOCKET";
/**
 * Environment variable containing the parent process ID supplied by the host.
 */
export const ENV_PROVIDER_PARENT_PID = "GESTALT_PLUGIN_PARENT_PID";
/**
 * Environment variable used to request static catalog generation.
 */
export const ENV_WRITE_CATALOG = "GESTALT_PLUGIN_WRITE_CATALOG";
/**
 * Protocol version currently implemented by the TypeScript runtime.
 */
export const CURRENT_PROTOCOL_VERSION = 2;
/**
 * Command-line usage for the runtime entrypoint.
 */
export const USAGE = "usage: bun run runtime.ts ROOT PROVIDER_TARGET";

/**
 * Parsed arguments for the runtime entrypoint.
 */
export type RuntimeArgs = {
  root: string;
  target: string;
};

/**
 * Provider implementations supported by the runtime host.
 */
export type LoadedProvider =
  | IntegrationProvider
  | AuthProvider
  | CacheProvider
  | SecretsProvider
  | S3Provider;

function assertProtocolVersion(protocolVersion: number): void {
  if (protocolVersion === CURRENT_PROTOCOL_VERSION) {
    return;
  }
  throw new ConnectError(
    `host requested protocol version ${protocolVersion}, provider requires ${CURRENT_PROTOCOL_VERSION}`,
    Code.FailedPrecondition,
  );
}

/**
 * CLI entrypoint that loads a provider from source and starts serving it.
 */
export async function main(
  argv: string[] = process.argv.slice(2),
): Promise<number> {
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

/**
 * Parses `gestalt-ts-runtime` CLI arguments.
 */
export function parseRuntimeArgs(argv: string[]): RuntimeArgs | undefined {
  if (argv.length !== 2) {
    return undefined;
  }
  return {
    root: argv[0]!,
    target: argv[1]!,
  };
}

/**
 * Loads any supported provider kind from a package root and optional target.
 */
export async function loadProviderFromTarget(
  root: string,
  rawTarget?: string,
): Promise<LoadedProvider> {
  const config = readPackageConfig(root);
  const targetValue =
    rawTarget?.trim() ||
    formatProviderTarget(
      config.providerTarget ?? readPackageProviderTarget(root),
    );
  const target = parseProviderTarget(targetValue);
  const module = await import(resolveProviderImportUrl(root, target));
  const candidate =
    (target.exportName ? module[target.exportName] : undefined) ??
    defaultProviderExport(module, target.kind);

  const defaultName =
    slugName(config.name ?? "") ||
    slugName(dirname(resolve(root, target.modulePath)));
  switch (target.kind) {
    case "integration": {
      if (!isIntegrationProvider(candidate)) {
        throw new Error(
          `${targetValue} did not resolve to a Gestalt integration provider`,
        );
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "auth": {
      if (!isAuthProvider(candidate)) {
        throw new Error(
          `${targetValue} did not resolve to a Gestalt auth provider`,
        );
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "cache": {
      if (!isCacheProvider(candidate)) {
        throw new Error(
          `${targetValue} did not resolve to a Gestalt cache provider`,
        );
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "secrets": {
      if (!isSecretsProvider(candidate)) {
        throw new Error(
          `${targetValue} did not resolve to a Gestalt secrets provider`,
        );
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    case "s3": {
      if (!isS3Provider(candidate)) {
        throw new Error(`${targetValue} did not resolve to a Gestalt s3 provider`);
      }
      candidate.resolveName(defaultName);
      return candidate;
    }
    default:
      throw new Error(
        `TypeScript SDK does not yet support provider kind ${JSON.stringify(target.kind)}`,
      );
  }
}

/**
 * Loads an integration provider from a package root and optional target.
 */
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

/**
 * Runs a provider that has already been loaded into memory.
 */
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
      throw new Error(
        "static catalog generation is only supported for integration providers",
      );
    }
    writeFileSync(catalogPath, pluginCatalogYaml(provider), "utf8");
    return;
  }

  await serve(provider);
}

/**
 * Runs an integration provider that has already been loaded into memory.
 */
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

/**
 * Runs a bundled provider export after validating its provider kind.
 */
export async function runBundledProvider(
  provider: unknown,
  kind: ProviderKind,
  providerName: string,
): Promise<void> {
  let loaded: LoadedProvider;
  switch (kind) {
    case "integration":
      if (!isIntegrationProvider(provider)) {
        throw new Error(
          "bundled target did not resolve to a Gestalt integration provider",
        );
      }
      loaded = provider;
      break;
    case "auth":
      if (!isAuthProvider(provider)) {
        throw new Error(
          "bundled target did not resolve to a Gestalt auth provider",
        );
      }
      loaded = provider;
      break;
    case "cache":
      if (!isCacheProvider(provider)) {
        throw new Error(
          "bundled target did not resolve to a Gestalt cache provider",
        );
      }
      loaded = provider;
      break;
    case "secrets":
      if (!isSecretsProvider(provider)) {
        throw new Error(
          "bundled target did not resolve to a Gestalt secrets provider",
        );
      }
      loaded = provider;
      break;
    case "s3":
      if (!isS3Provider(provider)) {
        throw new Error("bundled target did not resolve to a Gestalt s3 provider");
      }
      loaded = provider;
      break;
    default:
      throw new Error(
        `TypeScript SDK does not yet support provider kind ${JSON.stringify(kind)}`,
      );
  }
  loaded.name = slugName(providerName);
  await runLoadedProvider(loaded, {
    providerName,
  });
}

/**
 * Runs a bundled integration provider export.
 */
export async function runBundledPlugin(
  plugin: unknown,
  pluginName: string,
): Promise<void> {
  await runBundledProvider(plugin, "integration", pluginName);
}

/**
 * Starts serving a provider over the Gestalt Unix socket transport.
 */
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
        router.service(
          IntegrationProviderService,
          createProviderService(provider),
        );
      } else if (isAuthProvider(provider)) {
        router.service(AuthProviderService, createAuthService(provider));
      } else if (isCacheProvider(provider)) {
        router.service(CacheService, createCacheService(provider));
      } else if (isS3Provider(provider)) {
        router.service(S3Service, createS3Service(provider));
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

/**
 * Adapts the provider lifecycle service used during startup and health checks.
 *
 * @internal
 */
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
      assertProtocolVersion(request.protocolVersion);
      try {
        await provider.configureProvider(
          request.name,
          objectFromUnknown(request.config),
        );
      } catch (error) {
        throw new ConnectError(
          `configure provider: ${errorMessage(error)}`,
          Code.Unknown,
        );
      }
      return create(ConfigureProviderResponseSchema, {
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
    async healthCheck() {
      if (!provider.supportsHealthCheck()) {
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

/**
 * Adapts an integration provider to the shared protocol service implementation.
 *
 * @internal
 */
export function createProviderService(
  provider: IntegrationProvider,
): Partial<ServiceImpl<typeof IntegrationProviderService>> {
  return {
    getMetadata() {
      return create(ProviderMetadataSchema, {
        name: provider.name,
        displayName: provider.displayName,
        description: provider.description,
        connectionMode: connectionModeToProtoValue(
          provider.connectionMode,
        ) as ProviderConnectionMode,
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
      assertProtocolVersion(request.protocolVersion);
      try {
        await provider.configureProvider(
          request.name,
          objectFromUnknown(request.config),
        );
      } catch (error) {
        throw new ConnectError(
          `configure provider: ${errorMessage(error)}`,
          Code.Unknown,
        );
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
          providerRequest(
            request.token,
            request.connectionParams,
            request.context,
          ),
        ),
      );
    },
    async getSessionCatalog(request: GetSessionCatalogRequest) {
      let catalog: Catalog | Record<string, unknown> | null | undefined;
      try {
        catalog = await provider.catalogForRequest(
          providerRequest(
            request.token,
            request.connectionParams,
            request.context,
          ),
        );
      } catch (error) {
        throw new ConnectError(
          `session catalog: ${errorMessage(error)}`,
          Code.Unknown,
        );
      }
      if (!catalog) {
        throw new ConnectError(
          "provider does not support session catalogs",
          Code.Unimplemented,
        );
      }
      return create(GetSessionCatalogResponseSchema, {
        catalog: catalogToProto(catalog),
      });
    },
    async postConnect() {
      throw new ConnectError(
        "provider does not support post connect",
        Code.Unimplemented,
      );
    },
  };
}

/**
 * Adapts an auth provider to the shared protocol service implementation.
 *
 * @internal
 */
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
        throw new ConnectError(
          "auth provider returned nil response",
          Code.Internal,
        );
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
        throw new ConnectError(
          "auth provider returned nil user",
          Code.Internal,
        );
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

/**
 * Adapts a cache provider to the shared protocol service implementation.
 *
 * @internal
 */
export function createCacheService(
  provider: CacheProvider,
): Partial<ServiceImpl<typeof CacheService>> {
  return {
    async get(request) {
      const value = await provider.get(request.key);
      return create(CacheGetResponseSchema, {
        found: value !== undefined,
        value: value ? cloneUint8Array(value) : new Uint8Array(),
      });
    },
    async getMany(request) {
      const entries = await provider.getMany([...request.keys]);
      return create(CacheGetManyResponseSchema, {
        entries: request.keys.map((key) => {
          const found = Object.hasOwn(entries, key);
          const value = found ? entries[key] : undefined;
          return create(CacheResultSchema, {
            key,
            found,
            value: value ? cloneUint8Array(value) : new Uint8Array(),
          });
        }),
      });
    },
    async set(request) {
      await provider.set(
        request.key,
        cloneUint8Array(request.value),
        durationToSetOptions(request.ttl),
      );
      return create(EmptySchema, {});
    },
    async setMany(request) {
      await provider.setMany(
        request.entries.map((entry) => ({
          key: entry.key,
          value: cloneUint8Array(entry.value),
        })),
        durationToSetOptions(request.ttl),
      );
      return create(EmptySchema, {});
    },
    async delete(request) {
      return create(CacheDeleteResponseSchema, {
        deleted: await provider.delete(request.key),
      });
    },
    async deleteMany(request) {
      return create(CacheDeleteManyResponseSchema, {
        deleted: normalizeBigInt(await provider.deleteMany([...request.keys])),
      });
    },
    async touch(request) {
      return create(CacheTouchResponseSchema, {
        touched: await provider.touch(
          request.key,
          durationToMs(request.ttl),
        ),
      });
    },
  };
}

/**
 * Adapts a secrets provider to the shared protocol service implementation.
 *
 * @internal
 */
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

/**
 * Serializes an integration provider's static catalog as YAML.
 */
export function pluginCatalogYaml(plugin: IntegrationProvider): string {
  return catalogToYaml(plugin.staticCatalog());
}

function providerRequest(
  token: string,
  connectionParams: Record<string, string>,
  requestContext?: ProtoRequestContext,
): Request {
  const subject = requestContext?.subject;
  const credential = requestContext?.credential;
  const access = requestContext?.access;
  return {
    token,
    connectionParams: {
      ...connectionParams,
    },
    subject: {
      id: subject?.id ?? "",
      kind: subject?.kind ?? "",
      displayName: subject?.displayName ?? "",
      authSource: subject?.authSource ?? "",
    },
    credential: {
      mode: credential?.mode ?? "",
      subjectId: credential?.subjectId ?? "",
      connection: credential?.connection ?? "",
      instance: credential?.instance ?? "",
    },
    access: {
      policy: access?.policy ?? "",
      role: access?.role ?? "",
    },
  };
}

function providerKindToProto(kind: ProviderKind): ProtoProviderKind {
  switch (kind) {
    case "integration":
      return ProtoProviderKind.INTEGRATION;
    case "auth":
      return ProtoProviderKind.AUTH;
    case "cache":
      return ProtoProviderKind.CACHE;
    case "secrets":
      return ProtoProviderKind.SECRETS;
    case "s3":
      return ProtoProviderKind.S3;
    case "telemetry":
      return ProtoProviderKind.TELEMETRY;
    default:
      return ProtoProviderKind.UNSPECIFIED;
  }
}

function defaultProviderExport(module: Record<string, unknown>, kind: ProviderKind): unknown {
  switch (kind) {
    case "integration":
      return module.provider ?? module.plugin ?? module.default;
    case "auth":
      return module.auth ?? module.provider ?? module.default;
    case "cache":
      return module.cache ?? module.provider ?? module.default;
    case "secrets":
      return module.secrets ?? module.provider ?? module.default;
    case "s3":
      return module.s3 ?? module.provider ?? module.default;
    case "telemetry":
      return module.telemetry ?? module.provider ?? module.default;
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
        allowedRoles: op.allowedRoles ?? [],
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

function durationToMs(
  value: { seconds: bigint; nanos: number } | undefined,
): number {
  if (!value) {
    return 0;
  }
  const seconds = Number(value.seconds ?? 0n);
  const nanos = Number(value.nanos ?? 0);
  if (!Number.isFinite(seconds) || !Number.isFinite(nanos)) {
    return 0;
  }
  return Math.max(0, (seconds * 1000) + Math.trunc(nanos / 1_000_000));
}

function durationToSetOptions(
  value: { seconds: bigint; nanos: number } | undefined,
): { ttlMs: number } | undefined {
  if (!value) {
    return undefined;
  }
  return {
    ttlMs: durationToMs(value),
  };
}

if (import.meta.main) {
  void main().then(
    (code) => {
      process.exitCode = code;
    },
    (error: unknown) => {
      console.error(
        error instanceof Error ? (error.stack ?? error.message) : String(error),
      );
      process.exitCode = 1;
    },
  );
}
