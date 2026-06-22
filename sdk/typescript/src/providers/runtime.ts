#!/usr/bin/env bun

import { rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http2";
import { dirname, resolve } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ConnectRouter,
  type HandlerContext,
  type ServiceImpl,
} from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";

import {
  Agent as AgentProviderService,
} from "../internal/gen/v1/agent_pb.ts";
import {
  Authorization as AuthorizationProviderService,
} from "../internal/gen/v1/authorization_pb.ts";
import {
  Identity as IdentityProviderService,
  AuthorizeResponseSchema,
  GetGrantResponseSchema,
  GrantScopeSchema,
  IntrospectResponseSchema,
  ListGrantsResponseSchema,
  RevokeGrantResponseSchema,
  TokenResponseSchema,
  UserInfoResponseSchema,
} from "../internal/gen/v1/identity_pb.ts";
import {
  Cache as CacheService,
  CacheDeleteManyResponseSchema,
  CacheDeleteResponseSchema,
  CacheGetManyResponseSchema,
  CacheGetResponseSchema,
  CacheResultSchema,
  CacheTouchResponseSchema,
} from "../internal/gen/v1/cache_pb.ts";
import {
  Secrets as SecretsProviderService,
  GetSecretResponseSchema,
  type GetSecretRequest,
} from "../internal/gen/v1/secrets_pb.ts";
import {
  CatalogOperationSchema as ProtoCatalogOperationSchema,
  CatalogParameterSchema as ProtoCatalogParameterSchema,
  CatalogSchema as ProtoCatalogSchema,
  ConnectionMode as ProviderConnectionMode,
  GetSessionCatalogResponseSchema,
  OperationAnnotationsSchema as ProtoOperationAnnotationsSchema,
  ResolveHTTPSubjectResponseSchema,
  OperationResultSchema,
  ProviderMetadataSchema,
  type HTTPSubjectRequest as ProtoHTTPSubjectRequest,
  type RequestContext as ProtoRequestContext,
  type ResolveHTTPSubjectRequest as ProtoResolveHTTPSubjectRequest,
  type SubjectContext as ProtoSubjectContext,
  type SubjectPermissionContext as ProtoSubjectPermissionContext,
  AppProvider as AppProviderService,
  StartProviderResponseSchema,
  type ExecuteRequest,
  type GetSessionCatalogRequest,
  type StartProviderRequest,
} from "../internal/gen/v1/app_pb.ts";
import {
  Runtime as RuntimeProviderService,
} from "../internal/gen/v1/runtime_provider_pb.ts";
import {
  ConfigureProviderResponseSchema,
  HealthCheckResponseSchema,
  ProviderIdentitySchema,
  ProviderKind as ProtoProviderKind,
  ProviderLifecycle,
  StartRuntimeProviderResponseSchema,
  type ConfigureProviderRequest,
} from "../internal/gen/v1/runtime_pb.ts";
import { S3 as S3Service } from "../internal/gen/v1/s3_pb.ts";
import { Workflow as WorkflowProviderService } from "../internal/gen/v1/workflow_pb.ts";
import {
  attachRequestHelpers,
  errorMessage,
  parseSubjectId,
  type OperationResult,
  type Request,
  type Subject,
  type SubjectPermission,
} from "../api.ts";
import {
  AgentProvider,
  createAgentProviderService,
  isAgentProvider,
  type AgentToolRef,
} from "./agent.ts";
import { agentToolRefFromProto } from "../agent-conversions.ts";
import { fromWireRequestContext } from "../internal/codec/app.ts";
import type { RequestContext as WireRequestContext } from "../internal/gen/v1/app_pb.ts";
import type { RequestContext } from "../app.ts";
import {
  IdentityProvider,
  CALLER_BEARER_TOKEN_METADATA_KEY,
  isIdentityProvider,
  type IdentityCallContext,
} from "./identity.ts";
import {
  AuthorizationProvider,
  createAuthorizationProviderService,
  isAuthorizationProvider,
} from "./authorization.ts";
import { CacheProvider, isCacheProvider } from "./cache.ts";
import { SecretsProvider, isSecretsProvider } from "./secrets.ts";
import { catalogToYaml, type Catalog } from "../catalog.ts";
import {
  stringListsFromProto,
  stringListsToProto,
  valueFromJson,
  type JsonInput,
} from "../protocol.ts";
import {
  HTTPSubjectResolutionError,
  type HTTPSubjectRequest,
  type HTTPSubjectResolutionContext,
} from "../http-subject.ts";
import {
  AppProvider,
  encodeConnectionMode,
  encodeConnectionParam,
  isAppProvider,
} from "./app.ts";
import {
  RuntimeProvider,
  createRuntimeProviderService,
  isRuntimeProvider,
} from "../runtime-provider.ts";
import {
  providerKindLabel,
  resolveDefaultProviderExport,
} from "../provider-kind.ts";
import { type ProviderKind, slugName } from "../provider.ts";
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
} from "../target.ts";

/**
 * Environment variable containing the Unix socket path for a running provider.
 */
export const ENV_PROVIDER_SOCKET = "GESTALT_PROVIDER_SOCKET";
/**
 * Environment variable containing the parent process ID supplied by the host.
 */
export const ENV_PROVIDER_PARENT_PID = "GESTALT_APP_PARENT_PID";
/**
 * Environment variable used to request static catalog generation.
 */
export const ENV_WRITE_CATALOG = "GESTALT_APP_WRITE_CATALOG";
/**
 * Protocol version currently implemented by the TypeScript runtime.
 */
export const CURRENT_PROTOCOL_VERSION = 5;
/**
 * Command-line usage for the runtime entrypoint.
 */
export const USAGE = "usage: bun run runtime.ts ROOT PROVIDER_TARGET";
export { createAgentProviderService } from "./agent.ts";
export { createAuthorizationProviderService } from "./authorization.ts";
export { createRuntimeProviderService } from "../runtime-provider.ts";
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
  | AppProvider
  | AuthorizationProvider
  | IdentityProvider
  | CacheProvider
  | SecretsProvider
  | S3Provider
  | RuntimeProvider
  | AgentProvider
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
    isProvider: isAppProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.APP,
    registerService(router, provider) {
      router.service(
        AppProviderService,
        createProviderService(provider as AppProvider),
      );
    },
  },
  authorization: {
    isProvider:
      isAuthorizationProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.AUTHORIZATION,
    registerService(router, provider) {
      router.service(
        AuthorizationProviderService,
        createAuthorizationProviderService(provider as AuthorizationProvider),
      );
    },
  },
  identity: {
    isProvider:
      isIdentityProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.IDENTITY,
    registerService(router, provider) {
      router.service(
        IdentityProviderService,
        createIdentityService(provider as IdentityProvider),
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
  runtime: {
    isProvider:
      isRuntimeProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.RUNTIME,
    registerService(router, provider) {
      router.service(
        RuntimeProviderService,
        createRuntimeProviderService(provider as RuntimeProvider),
      );
    },
  },
  agent: {
    isProvider: isAgentProvider as (value: unknown) => value is LoadedProvider,
    protoKind: ProtoProviderKind.AGENT,
    registerService(router, provider) {
      router.service(
        AgentProviderService,
        createAgentProviderService(provider as AgentProvider),
      );
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
  if (catalogPath) {
    if (!isAppProvider(provider)) {
      throw new Error("static catalog generation is only supported for app providers");
    }
    writeFileSync(catalogPath, catalogToYaml(provider.staticCatalog()), "utf8");
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
  rmSync(socketPath, { force: true });

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
          rmSync(socketPath, { force: true });
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
      try {
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
      } catch (error) {
        throw providerRuntimeError("provider identity", error);
      }
    },
    async configureProvider(request: ConfigureProviderRequest) {
      assertProtocolVersion(request.protocolVersion);
      try {
        await provider.configureProvider(
          request.name,
          objectFromUnknown(request.config),
        );
      } catch (error) {
        throw providerRuntimeError("configure provider", error);
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
    async startProvider() {
      try {
        await provider.startProvider();
      } catch (error) {
        throw providerRuntimeError("start provider", error);
      }
      return create(StartRuntimeProviderResponseSchema, {
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
  };
}

/**
 * Adapts an app provider to the shared protocol service implementation.
 *
 * @internal
 */
export function createProviderService(
  provider: LoadedProvider,
): Partial<ServiceImpl<typeof AppProviderService>> {
  if (!isAppProvider(provider)) {
    throw new Error("provider is not an app provider");
  }
  return {
    async getMetadata() {
      try {
        return create(ProviderMetadataSchema, {
          name: provider.name,
          displayName: provider.displayName,
          description: provider.description,
          connectionMode: encodeConnectionMode(
            provider.connectionMode,
          ) as ProviderConnectionMode,
          authTypes: [...provider.authTypes],
          connectionParams: Object.fromEntries(
            Object.entries(provider.connectionParams).map(([key, value]) => [
              key,
              encodeConnectionParam(value),
            ]),
          ),
          staticCatalog: catalogToProto(provider.staticCatalog()),
          supportsSessionCatalog: provider.supportsSessionCatalog(),
          minProtocolVersion: CURRENT_PROTOCOL_VERSION,
          maxProtocolVersion: CURRENT_PROTOCOL_VERSION,
        });
      } catch (error) {
        throw providerRuntimeError("provider metadata", error);
      }
    },
    async startProvider(request: StartProviderRequest) {
      assertProtocolVersion(request.protocolVersion);
      try {
        await provider.configureProvider(
          request.name,
          objectFromUnknown(request.config),
        );
      } catch (error) {
        throw providerRuntimeError("configure provider", error);
      }
      return create(StartProviderResponseSchema, {
        protocolVersion: CURRENT_PROTOCOL_VERSION,
      });
    },
    async execute(request: ExecuteRequest) {
      return operationResultToProto(
	        await provider.execute(
	          request.operation,
	          objectFromUnknown(request.params),
          providerRequest(
            request.token,
            request.connectionParams,
            request.context,
            request.idempotencyKey,
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
              email: subject.email ?? "",
              displayName: subject.displayName ?? "",
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
  };
}

/**
 * Adapts an identity provider to the shared protocol service implementation.
 *
 * @internal
 */
export function createIdentityService(
  provider: IdentityProvider,
): Partial<ServiceImpl<typeof IdentityProviderService>> {
  return {
    async authorize(request) {
      const response = await provider.authorize({
        responseType: request.responseType,
        clientId: request.clientId,
        redirectUri: request.redirectUri,
        scope: request.scope,
        state: request.state,
      });
      if (!response?.redirectUri) {
        throw new ConnectError(
          "identity provider returned empty redirect URI",
          Code.Internal,
        );
      }
      return create(AuthorizeResponseSchema, {
        redirectUri: response.redirectUri,
      });
    },
    async token(request) {
      const response = await provider.token({
        grantType: request.grantType,
        code: request.code,
        redirectUri: request.redirectUri,
        clientId: request.clientId,
        state: request.state,
        scope: request.scope,
        subjectToken: request.subjectToken,
        subjectTokenType: request.subjectTokenType,
        expiresIn: request.expiresIn,
      });
      if (!response?.accessToken) {
        throw new ConnectError(
          "identity provider returned empty access token",
          Code.Internal,
        );
      }
      return create(TokenResponseSchema, {
        accessToken: response.accessToken,
        tokenType: response.tokenType || "Bearer",
        expiresIn: normalizeBigInt(response.expiresIn ?? 0),
        refreshToken: response.refreshToken ?? "",
        scope: response.scope ?? "",
        grantId: response.grantId ?? "",
      });
    },
    async introspect(request) {
      const response = await provider.introspect({
        token: request.token,
        tokenTypeHint: request.tokenTypeHint,
      });
      if (!response) {
        throw new ConnectError(
          "identity provider returned nil introspection",
          Code.Internal,
        );
      }
      return create(IntrospectResponseSchema, {
        active: response.active,
        subject: response.subject ?? "",
        scope: response.scope ?? "",
        clientId: response.clientId ?? "",
        audience: [...(response.audience ?? [])],
      });
    },
    async userInfo(request, context) {
      const response = await provider.userInfo(
        {},
        authCallContextFromHandler(context),
      );
      if (!response?.subjectId) {
        throw new ConnectError(
          "identity provider returned empty userinfo",
          Code.Internal,
        );
      }
      return create(UserInfoResponseSchema, {
        subjectId: response.subjectId,
        email: response.email ?? "",
        name: response.name ?? "",
      });
    },
    async listGrants(request, context) {
      const response = await provider.listGrants(
        {},
        authCallContextFromHandler(context),
      );
      return create(ListGrantsResponseSchema, {
        grantIds: [...(response.grantIds ?? [])],
      });
    },
    async getGrant(request, context) {
      try {
        const response = await provider.getGrant(
          { grantId: request.grantId },
          authCallContextFromHandler(context),
        );
        return create(GetGrantResponseSchema, {
          scopes: (response.scopes ?? []).map((scope) =>
            create(GrantScopeSchema, {
              scope: scope.scope,
              resource: [...(scope.resource ?? [])],
            }),
          ),
          createdAt: normalizeBigInt(response.createdAt ?? 0),
          expiresAt: normalizeBigInt(response.expiresAt ?? 0),
        });
      } catch (error) {
        throw grantRPCError(error);
      }
    },
    async revokeGrant(request, context) {
      try {
        await provider.revokeGrant(
          { grantId: request.grantId },
          authCallContextFromHandler(context),
        );
        return create(RevokeGrantResponseSchema, {});
      } catch (error) {
        throw grantRPCError(error);
      }
    },
  };
}

function authCallContextFromHandler(context: HandlerContext): IdentityCallContext {
  return {
    callerBearerToken:
      context.requestHeader.get(CALLER_BEARER_TOKEN_METADATA_KEY) ?? "",
  };
}

function grantRPCError(error: unknown): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  const message =
    error instanceof Error ? error.message : "grant management failed";
  if (/not found/i.test(message)) {
    return new ConnectError(message, Code.NotFound);
  }
  if (/unauthorized|forbidden|denied/i.test(message)) {
    return new ConnectError(message, Code.PermissionDenied);
  }
  if (/not support/i.test(message)) {
    return new ConnectError(message, Code.Unimplemented);
  }
  return new ConnectError(message, Code.Internal);
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
  idempotencyKey = "",
): Request {
  const credential = requestContext?.credential;
  const access = requestContext?.access;
  const host = requestContext?.host;
  return attachRequestHelpers({
    token,
    connectionParams: {
      ...connectionParams,
    },
    subject: providerSubject(requestContext?.subject),
    agentSubject: providerSubject(requestContext?.agentSubject),
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
    toolRefs: providerRequestToolRefs(requestContext),
    toolRefsSet: requestContext?.toolRefsSet ?? false,
    host: {
      publicBaseUrl: host?.publicBaseUrl ?? "",
    },
    __requestContext: requestContext === undefined
      ? undefined
      : fromWireRequestContext(requestContext),
    idempotencyKey: idempotencyKey.trim(),
  });
}

function providerSubject(subject?: ProtoSubjectContext): Subject {
  return {
    id: subject?.id ?? "",
    email: subject?.email ?? "",
    displayName: subject?.displayName ?? "",
    scopes: [...(subject?.scopes ?? [])],
    permissions: subjectPermissionsFromProto(subject?.permissions),
    kind: subjectKind(subject?.id ?? ""),
  };
}

function subjectPermissionsFromProto(
  permissions?: readonly ProtoSubjectPermissionContext[] | undefined,
): SubjectPermission[] {
  return permissions?.map((permission) => ({
    app: permission.app,
    operations: permission.allOperations ? [] : [...permission.operations],
  })) ?? [];
}

function subjectKind(subjectID: string): string {
  return parseSubjectId(subjectID)?.kind ?? "";
}

function providerRequestToolRefs(
  requestContext?: ProtoRequestContext,
): AgentToolRef[] {
  return requestContext?.toolRefs?.map(agentToolRefFromProto) ?? [];
}

function providerHTTPSubjectRequest(
  request?: ProtoHTTPSubjectRequest,
): HTTPSubjectRequest {
  return {
    binding: request?.binding ?? "",
    method: request?.method ?? "",
    path: request?.path ?? "",
    contentType: request?.contentType ?? "",
    headers: stringListsFromProto(request?.headers),
    query: stringListsFromProto(request?.query),
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
    host: request.host,
    workflow: request.workflow,
  };
}

function operationResultToProto(result: OperationResult) {
  return create(OperationResultSchema, {
    status: result.status,
    body: result.body,
    headers: stringListsToProto(result.headers),
  });
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

function providerRuntimeError(label: string, error: unknown): ConnectError {
  return new ConnectError(`${label}: ${errorMessage(error)}`, Code.Unknown);
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
        inputSchema: catalogSchemaToWire(op.inputSchema),
        outputSchema: catalogSchemaToWire(op.outputSchema),
        annotations: op.annotations
          ? create(ProtoOperationAnnotationsSchema, {
              readOnlyHint: op.annotations.readOnlyHint,
              idempotentHint: op.annotations.idempotentHint,
              destructiveHint: op.annotations.destructiveHint,
              openWorldHint: op.annotations.openWorldHint,
            })
          : undefined,
        requiredScopes: op.requiredScopes ?? [],
        tags: op.tags ?? [],
        readOnly: op.readOnly ?? false,
        transport: op.transport ?? "",
        allowedRoles: op.allowedRoles ?? [],
        parameters: (op.parameters ?? []).map((p) =>
          create(ProtoCatalogParameterSchema, {
            name: p.name,
            type: p.type,
            description: p.description ?? "",
            required: p.required ?? false,
            default: p.default !== undefined
              ? valueFromJson(p.default as JsonInput)
              : undefined,
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

function catalogSchemaToWire(schema: unknown): string {
  if (schema === undefined || schema === null) {
    return "";
  }
  if (typeof schema === "string") {
    return schema;
  }
  return JSON.stringify(schema);
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

/**
 * Converts a protocol-level wire request context into the generated clients'
 * native form, for providers that receive contexts through their own protocol
 * surfaces and need to hand them to `connect(..., { context })`.
 */
export function nativeRequestContext(
  context: WireRequestContext | undefined,
): RequestContext | undefined {
  return context === undefined ? undefined : fromWireRequestContext(context);
}
