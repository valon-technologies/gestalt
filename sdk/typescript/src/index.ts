/**
 * @packageDocumentation
 *
 * Authored TypeScript APIs for building Gestalt providers, helper CLIs, and
 * runtime adapters.
 *
 * @example
 * ```ts
 * import { definePlugin, ok, operation, s } from "@valon-technologies/gestalt";
 *
 * export const plugin = definePlugin({
 *   displayName: "Example Provider",
 *   operations: [
 *     operation({
 *       id: "hello",
 *       input: s.object({ name: s.string({ default: "World" }) }),
 *       output: s.object({ message: s.string() }),
 *       async handler(input) {
 *         return ok({ message: `Hello, ${input.name}` });
 *       },
 *     }),
 *   ],
 * });
 * ```
 *
 * @example
 * ```ts
 * import { parseRuntimeArgs, serve } from "@valon-technologies/gestalt/runtime";
 * ```
 */
export {
  connectionParam,
  ok,
  response,
  responseBrand,
  request,
  type Access,
  type MaybePromise,
  type Credential,
  type OperationResult,
  type Request,
  type Response,
  type Subject,
} from "./api.ts";
export {
  catalogToJson,
  catalogToYaml,
  schemaToCatalogSchema,
  schemaToParameters,
  writeCatalogYaml,
  type Catalog,
  type CatalogOperation,
  type CatalogParameter,
  type CatalogSchema,
} from "./catalog.ts";
export {
  buildProviderBinary,
  bunBuildCommand,
  bunTarget,
  parseBuildArgs,
} from "./build.ts";
export {
  ENV_PLUGIN_INVOKER_SOCKET,
  PluginInvoker,
  type PluginInvokeOptions,
} from "./invoker.ts";
export {
  AuthenticationProvider,
  defineAuthenticationProvider,
  isAuthenticationProvider,
  type AuthenticatedUser,
  type AuthenticationProviderOptions,
  type AuthenticationSessionSettings,
  type BeginLoginRequest,
  type BeginLoginResponse,
  type CompleteLoginRequest,
} from "./auth.ts";
export {
  Cache,
  CacheProvider,
  cacheSocketEnv,
  defineCacheProvider,
  isCacheProvider,
  type CacheEntry,
  type CacheProviderOptions,
  type CacheSetOptions,
} from "./cache.ts";
export {
  defineSecretsProvider,
  isSecretsProvider,
  type SecretsProviderOptions,
} from "./secrets.ts";
export {
  PluginProvider,
  connectionModeToProtoValue,
  connectionParamToProto,
  definePlugin,
  isPluginProvider,
  operation,
  type ConnectionMode,
  type ConnectionParamDefinition,
  type OperationDefinition,
  type OperationOptions,
  type PluginDefinitionOptions,
  type SessionCatalog,
  type SessionCatalogHandler,
} from "./plugin.ts";
export {
  RuntimeProvider,
  isRuntimeProvider,
  slugName,
  type CloseHandler,
  type ConfigureHandler,
  type HealthCheckHandler,
  type ProviderKind,
  type ProviderMetadata,
  type RuntimeProviderOptions,
  type WarningsHandler,
} from "./provider.ts";
export {
  array,
  boolean,
  type InferSchema,
  integer,
  number,
  object,
  optional,
  s,
  string,
  type Schema,
  type SchemaOptions,
} from "./schema.ts";
export {
  CURRENT_PROTOCOL_VERSION,
  ENV_PROVIDER_PARENT_PID,
  ENV_PROVIDER_SOCKET,
  ENV_WRITE_CATALOG,
  createAuthenticationService,
  createCacheService,
  createSecretsService,
  createProviderService,
  createRuntimeService,
  loadProviderFromTarget,
  main as runtimeMain,
  parseRuntimeArgs,
  runBundledProvider,
  runLoadedProvider,
  serve,
} from "./runtime.ts";
export {
  defaultProviderName,
  formatModuleTarget,
  formatProviderTarget,
  parseModuleTarget,
  parseProviderTarget,
  readPackageConfig,
  readPackageProviderTarget,
  resolveProviderImportUrl,
  resolveProviderModulePath,
  type ModuleTarget,
  type PackageConfig,
  type ProviderTarget,
} from "./target.ts";
export {
  IndexedDB,
  ObjectStore,
  Index,
  Cursor,
  CursorDirection,
  NotFoundError,
  AlreadyExistsError,
  ColumnType,
  indexedDBSocketEnv,
  type Record,
  type KeyRange,
  type ColumnSchema,
  type IndexSchema,
  type ObjectStoreSchema,
  type OpenCursorOptions,
} from "./indexeddb.ts";
export {
  S3,
  S3Object,
  S3Provider,
  S3InvalidRangeError,
  S3NotFoundError,
  S3PreconditionFailedError,
  PresignMethod,
  createS3Service,
  defineS3Provider,
  isS3Provider,
  s3SocketEnv,
  type ByteRange,
  type CopyOptions,
  type ListOptions,
  type ListPage,
  type ObjectMeta,
  type ObjectRef,
  type PresignOptions,
  type PresignResult,
  type ProviderReadResult,
  type ReadOptions,
  type ReadResult,
  type S3BodySource,
  type S3ProviderOptions,
  type WriteOptions,
} from "./s3.ts";
export {
  ENV_WORKFLOW_HOST_SOCKET,
  WorkflowHost,
  WorkflowProvider,
  WorkflowRunStatus,
  createWorkflowProviderService,
  defineWorkflowProvider,
  isWorkflowProvider,
  type BoundWorkflowEventTrigger,
  type BoundWorkflowRun,
  type BoundWorkflowSchedule,
  type CancelWorkflowProviderRunRequest,
  type DeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest,
  type GetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest,
  type InvokeWorkflowOperationRequest,
  type InvokeWorkflowOperationResponse,
  type ListWorkflowProviderEventTriggersRequest,
  type ListWorkflowProviderRunsRequest,
  type ListWorkflowProviderSchedulesRequest,
  type PauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest,
  type PublishWorkflowProviderEventRequest,
  type ResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest,
  type StartWorkflowProviderRunRequest,
  type UpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest,
  type WorkflowEvent,
  type WorkflowProviderOptions,
} from "./workflow.ts";
