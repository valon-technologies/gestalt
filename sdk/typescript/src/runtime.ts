import { existsSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http2";
import { dirname, resolve } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ConnectRouter,
  type ServiceImpl,
} from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";

import {
  AuthenticationProvider as AuthenticationProviderService,
  AuthSessionSettingsSchema,
  AuthenticatedUserSchema,
  BeginLoginResponseSchema,
  type CompleteLoginRequest as AuthCompleteLoginRequest,
  type ValidateExternalTokenRequest,
} from "../gen/v1/authentication_pb.ts";
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
  ResolveHTTPSubjectResponseSchema,
  OperationResultSchema,
  ProviderMetadataSchema,
  type HTTPSubjectRequest as ProtoHTTPSubjectRequest,
  type RequestContext as ProtoRequestContext,
  type ResolveHTTPSubjectRequest as ProtoResolveHTTPSubjectRequest,
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
import { WorkflowProvider as WorkflowProviderService } from "../gen/v1/workflow_pb.ts";
import { errorMessage, type Request } from "./api.ts";
import {
  AuthenticationProvider,
  isAuthenticationProvider,
  type AuthenticatedUser,
} from "./auth.ts";
import { CacheProvider, isCacheProvider } from "./cache.ts";
import { SecretsProvider, isSecretsProvider } from "./secrets.ts";
import { catalogToYaml, type Catalog } from "./catalog.ts";
import {
  HTTPSubjectResolutionError,
  type HTTPSubjectRequest,
  type HTTPSubjectResolutionContext,
} from "./http-subject.ts";
import {
  PluginProvider,
  connectionModeToProtoValue,
  connectionParamToProto,
  isPluginProvider,
} from "./plugin.ts";
import {
  providerKindLabel,
  resolveDefaultProviderExport,
} from "./provider-kind.ts";
import { type ProviderKind, slugName } from "./provider.ts";
import { S3Provider, createS3Service, isS3Provider } from "./s3.ts";
import {
  WorkflowProvider,
  createWorkflowProviderService,
  isWorkflowProvider,
} from "./workflow.ts";
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
 * Environment variable used to request generated manifest metadata export.
 */
export const ENV_WRITE_MANIFEST_METADATA =
  "GESTALT_PLUGIN_WRITE_MANIFEST_METADATA";
/**
 * Protocol version currently implemented by the TypeScript runtime.
 */
export const CURRENT_PROTOCOL_VERSION = 3;
/**
 * Command-line usage for the runtime entrypoint.
 */
export const USAGE = "usage: bun run runtime.ts ROOT PROVIDER_TARGET";
export { createWorkflowProviderService } from "./workflow.ts";

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
  | PluginProvider
  | AuthenticationProvider
  | CacheProvider
  | SecretsProvider
  | S3Provider
  | WorkflowProvider;

type ProviderRuntimeEntry = {
  isProvider: (value: unknown) => value is LoadedProvider;
  protoKind: ProtoProviderKind;
  registerService: (router: ConnectRouter, provider: LoadedProvider) => void;
};

const PROVIDER_RUNTIME_ENTRIES: Partial<
  Record<ProviderKind, ProviderRuntimeEntry>
> = {
  integration: {
    isProvider: isPluginProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.INTEGRATION,
    registerService(router, provider) {
      router.service(
        IntegrationProviderService,
        createProviderService(provider as PluginProvider),
      );
    },
  },
  authentication: {
    isProvider:
      isAuthenticationProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.AUTHENTICATION,
    registerService(router, provider) {
      router.service(
        AuthenticationProviderService,
        createAuthenticationService(provider as AuthenticationProvider),
      );
    },
  },
  cache: {
    isProvider: isCacheProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.CACHE,
    registerService(router, provider) {
      router.service(
        CacheService,
        createCacheService(provider as CacheProvider),
      );
    },
  },
  secrets: {
    isProvider: isSecretsProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.SECRETS,
    registerService(router, provider) {
      router.service(
        SecretsProviderService,
        createSecretsService(provider as SecretsProvider),
      );
    },
  },
  s3: {
    isProvider: isS3Provider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.S3,
    registerService(router, provider) {
      router.service(S3Service, createS3Service(provider as S3Provider));
    },
  },
  workflow: {
    isProvider: isWorkflowProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.WORKFLOW,
    registerService(router, provider) {
      router.service(
        WorkflowProviderService,
        createWorkflowProviderService(provider as WorkflowProvider),
      );
    },
  },
};

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
  const explicitTarget = rawTarget?.trim();
  const target = explicitTarget
    ? parseProviderTarget(explicitTarget)
    : config.providerTarget ?? readPackageProviderTarget(root);
  const targetValue = explicitTarget || formatProviderTarget(target);
  const module = await import(resolveProviderImportUrl(root, target));
  const candidate =
    (target.exportName ? Reflect.get(module, target.exportName) : undefined) ??
    resolveDefaultProviderExport(module, target.kind);

  const defaultName =
    slugName(config.name ?? "") ||
    slugName(dirname(resolve(root, target.modulePath)));
  const provider = resolveLoadedProvider(candidate, target.kind, targetValue);
  provider.resolveName(defaultName);
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
  const manifestMetadataPath = process.env[ENV_WRITE_MANIFEST_METADATA];
  if (catalogPath || manifestMetadataPath) {
    if (!isPluginProvider(provider)) {
      throw new Error(
        "static catalog and manifest metadata generation are only supported for plugin providers",
      );
    }
    if (catalogPath) {
      writeFileSync(catalogPath, catalogToYaml(provider.staticCatalog()), "utf8");
    }
    if (manifestMetadataPath && provider.supportsManifestMetadata()) {
      provider.writeManifestMetadata(manifestMetadataPath);
    }
    return;
  }

  await serve(provider);
}

/**
 * Runs a bundled provider export after validating its provider kind.
 */
export async function runBundledProvider(
  provider: unknown,
  kind: ProviderKind,
  providerName: string,
): Promise<void> {
  await runLoadedProvider(resolveLoadedProvider(provider, kind, "bundled target"), {
    providerName,
  });
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
      registerProviderService(router, provider);
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
        kind: providerRuntimeEntry(provider.kind).protoKind,
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
 * Adapts a plugin provider to the shared protocol service implementation.
 *
 * @internal
 */
export function createProviderService(
  provider: LoadedProvider,
): Partial<ServiceImpl<typeof IntegrationProviderService>> {
  if (!isPluginProvider(provider)) {
    throw new Error("provider is not a plugin provider");
  }
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
            request.invocationToken,
          ),
        ),
      );
    },
    async resolveHTTPSubject(request: ProtoResolveHTTPSubjectRequest) {
      let subject;
      try {
        subject = await provider.resolveHTTPSubject(
          providerHTTPSubjectRequest(request.request),
          providerHTTPSubjectResolutionContext(request.context),
        );
      } catch (error) {
        if (error instanceof HTTPSubjectResolutionError) {
          return create(ResolveHTTPSubjectResponseSchema, {
            rejectStatus: error.status,
            rejectMessage: error.message,
          });
        }
        throw new ConnectError(
          `resolve http subject: ${errorMessage(error)}`,
          Code.Unknown,
        );
      }
      return create(ResolveHTTPSubjectResponseSchema, subject
        ? {
            subject: {
              id: subject.id,
              kind: subject.kind,
              displayName: subject.displayName,
              authSource: subject.authSource,
            },
          }
        : {});
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
 * Adapts an authentication provider to the shared protocol service
 * implementation.
 *
 * @internal
 */
export function createAuthenticationService(
  provider: AuthenticationProvider,
): Partial<ServiceImpl<typeof AuthenticationProviderService>> {
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
          "authentication provider returned nil response",
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
          "authentication provider returned nil user",
          Code.Internal,
        );
      }
      return authenticatedUserToProto(user);
    },
    async validateExternalToken(request: ValidateExternalTokenRequest) {
      if (!provider.supportsExternalTokenValidation()) {
        throw new ConnectError(
          "authentication provider does not support external token validation",
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
          "authentication provider does not expose session settings",
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

function providerRequest(
  token: string,
  connectionParams: Record<string, string>,
  requestContext?: ProtoRequestContext,
  invocationToken = "",
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
    workflow: {
      ...(requestContext?.workflow ?? {}),
    },
    invocationToken,
  };
}

function providerHTTPSubjectRequest(
  request?: ProtoHTTPSubjectRequest,
): HTTPSubjectRequest {
  return {
    binding: request?.binding ?? "",
    method: request?.method ?? "",
    path: request?.path ?? "",
    contentType: request?.contentType ?? "",
    headers: providerStringLists(request?.headers),
    query: providerStringLists(request?.query),
    params: objectFromUnknown(request?.params),
    rawBody: new Uint8Array(request?.rawBody ?? new Uint8Array()),
    securityScheme: request?.securityScheme ?? "",
    verifiedSubject: request?.verifiedSubject ?? "",
    verifiedClaims: {
      ...(request?.verifiedClaims ?? {}),
    },
  };
}

function providerHTTPSubjectResolutionContext(
  requestContext?: ProtoRequestContext,
): HTTPSubjectResolutionContext {
  const request = providerRequest("", {}, requestContext);
  return {
    subject: request.subject,
    credential: request.credential,
    access: request.access,
    workflow: request.workflow,
  };
}

function providerStringLists(
  input: Record<string, { values?: string[] }> | undefined,
): Record<string, string[]> {
  const output: Record<string, string[]> = {};
  for (const [key, value] of Object.entries(input ?? {})) {
    output[key] = [...(value.values ?? [])];
  }
  return output;
}

function providerRuntimeEntry(
  kind: ProviderKind,
): ProviderRuntimeEntry {
  const entry = PROVIDER_RUNTIME_ENTRIES[kind];
  if (!entry) {
    throw new Error(
      `TypeScript SDK does not yet support provider kind ${JSON.stringify(kind)}`,
    );
  }
  return entry;
}

function resolveLoadedProvider(
  candidate: unknown,
  kind: ProviderKind,
  source: string,
): LoadedProvider {
  const entry = providerRuntimeEntry(kind);
  if (!entry.isProvider(candidate)) {
    throw new Error(
      `${source} did not resolve to a Gestalt ${providerKindLabel(kind)}`,
    );
  }
  return candidate;
}

function registerProviderService(
  router: ConnectRouter,
  provider: LoadedProvider,
): void {
  providerRuntimeEntry(provider.kind).registerService(router, provider);
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
