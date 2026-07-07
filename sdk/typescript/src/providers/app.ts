import {
  type Catalog,
  type CatalogOperation,
  type CatalogSchema,
  catalogToJson,
  schemaToCatalogSchema,
  schemaToParameters,
  writeCatalogYaml,
} from "../catalog.ts";
import {
  errorMessage,
  type MaybePromise,
  type OperationResult,
  type Request,
  responseBrand,
  type Response,
  type ResponseHeaders,
  type SubjectInput,
} from "../api.ts";
import {
  cloneHTTPSubjectRequest,
  cloneHTTPSubjectResolutionContext,
  type HTTPSubjectRequest,
  type HTTPSubjectResolutionContext,
  type HTTPSubjectResolver,
} from "../http-subject.ts";
import { operationResult } from "../app-decode.ts";
import {
  isProviderBase,
  ProviderBase,
  type ProviderBaseOptions,
} from "../provider.ts";
import type { Schema } from "../schema.ts";
import { IndexedDB } from "./indexeddb.ts";
import {
  type MigrationRunOptions,
  type Revision,
  runMigrations,
} from "./migrations.ts";

const JSON_CONTENT_TYPE = "application/json";

/**
 * How an app provider expects to authenticate or connect.
 */
export type ConnectionMode =
  | "unspecified"
  | "none"
  | "subject";

/**
 * Metadata for a single connection parameter exposed by a provider.
 */
export interface ConnectionParamDefinition {
  required?: boolean;
  description?: string;
  defaultValue?: string;
  from?: string;
  field?: string;
}

/**
 * Operation definition accepted by {@link operation} and {@link defineApp}.
 */
export interface OperationOptions<In, Out> {
  id: string;
  method?: string;
  title?: string;
  description?: string;
  allowedRoles?: string[];
  tags?: string[];
  readOnly?: boolean;
  visible?: boolean;
  input?: Schema<In>;
  output?: Schema<Out>;
  handler: (input: In, request: Request) => MaybePromise<Out | Response<Out>>;
}

/**
 * Normalized app operation definition.
 */
export interface OperationDefinition<In, Out> extends OperationOptions<
  In,
  Out
> {}

/**
 * Session-specific catalog payload returned by a provider at runtime.
 */
export type SessionCatalog = Catalog | Record<string, unknown>;

/**
 * Callback used to resolve a catalog for an authenticated request context.
 */
export type SessionCatalogHandler = (
  request: Request,
) => MaybePromise<SessionCatalog | null | undefined>;

/**
 * Migrations declared on an app: either a bare ordered list of revisions, or a
 * list plus binding/ledger overrides.
 */
export type MigrationsOption = Revision[] | MigrationRunOptions;

/**
 * Runtime hooks required to implement an app provider.
 */
export interface AppDefinitionOptions extends ProviderBaseOptions {
  connectionMode?: ConnectionMode;
  authTypes?: string[];
  connectionParams?: Record<string, ConnectionParamDefinition>;
  resolveHTTPSubject?: HTTPSubjectResolver;
  iconSvg?: string;
  operations: Array<OperationDefinition<any, any>>;
  sessionCatalog?: SessionCatalogHandler;
  /**
   * Revisions the SDK runs once, on the first request the app receives, before
   * handling it. They run at request time rather than during provider
   * configuration because the indexeddb host service they need is only
   * reachable while serving a request. The author never calls a runner.
   */
  migrations?: MigrationsOption;
}

function normalizeResponseHeaders(
  headers?: ResponseHeaders,
): Record<string, string[]> {
  const normalized: Record<string, string[]> = {};
  for (const [name, value] of Object.entries(headers ?? {})) {
    normalized[name] = Array.isArray(value) ? [...value] : [value];
  }
  if (
    !Object.keys(normalized).some((name) =>
      name.toLowerCase() === "content-type"
    )
  ) {
    normalized["Content-Type"] = [JSON_CONTENT_TYPE];
  }
  return normalized;
}

/**
 * Normalizes an app operation definition.
 */
export function operation<In, Out>(
  options: OperationOptions<In, Out>,
): OperationDefinition<In, Out> {
  return {
    ...options,
    id: options.id.trim(),
    method: normalizeMethod(options.method),
    title: options.title?.trim() ?? "",
    description: options.description?.trim() ?? "",
    allowedRoles: normalizeAllowedRoles(options.allowedRoles),
    tags: [...(options.tags ?? [])],
  };
}

/**
 * App provider implementation consumed by the Gestalt runtime.
 *
 * @example
 * ```ts
 * import { defineApp, ok, operation, s } from "@valon-technologies/gestalt";
 *
 * export const app = defineApp({
 *   displayName: "Example Provider",
 *   operations: [
 *     operation({
 *       id: "ping",
 *       method: "GET",
 *       readOnly: true,
 *       input: s.object({ name: s.string({ default: "World" }) }),
 *       output: s.object({ message: s.string() }),
 *       async handler(input) {
 *         return ok({ message: `Hello, ${input.name}` });
 *       },
 *     }),
 *   ],
 * });
 * ```
 */
export class AppProvider extends ProviderBase {
  readonly kind = "integration" as const;
  readonly iconSvg: string;
  readonly connectionMode: ConnectionMode;
  readonly authTypes: string[];
  readonly connectionParams: Record<string, ConnectionParamDefinition>;

  private readonly sessionCatalogHandler: SessionCatalogHandler | undefined;
  private readonly httpSubjectResolver: HTTPSubjectResolver | undefined;
  private readonly operations = new Map<string, OperationDefinition<any, any>>();
  private readonly migrations: MigrationRunOptions | undefined;
  private migrationRun: Promise<void> | undefined;

  constructor(options: AppDefinitionOptions) {
    super(options);
    this.iconSvg = options.iconSvg?.trim() ?? "";
    this.connectionMode = options.connectionMode ?? "unspecified";
    this.authTypes = [...(options.authTypes ?? [])];
    this.connectionParams = normalizeConnectionParams(options.connectionParams);
    this.httpSubjectResolver = options.resolveHTTPSubject;
    this.sessionCatalogHandler = options.sessionCatalog;
    this.migrations = normalizeMigrations(options.migrations);

    for (const rawEntry of options.operations) {
      const entry = operation(rawEntry);
      if (!entry.id) {
        throw new Error("operation id is required");
      }
      if (this.operations.has(entry.id)) {
        throw new Error(`duplicate operation id ${JSON.stringify(entry.id)}`);
      }
      this.operations.set(entry.id, entry);
    }
  }

  private ensureMigrations(): Promise<void> {
    if (!this.migrations) {
      return Promise.resolve();
    }
    if (!this.migrationRun) {
      this.migrationRun = this.applyMigrations().catch((error: unknown) => {
        this.migrationRun = undefined;
        throw error;
      });
    }
    return this.migrationRun;
  }

  private async applyMigrations(): Promise<void> {
    const options = this.migrations;
    if (!options) {
      return;
    }
    const db = new IndexedDB(options.dbBinding);
    try {
      await runMigrations(db, options);
    } finally {
      db.close();
    }
  }

  /**
   * Reports whether the provider exposes a session-specific catalog.
   */
  supportsSessionCatalog(): boolean {
    return this.sessionCatalogHandler !== undefined;
  }

  /**
   * Resolves a catalog for the current request context, if configured.
   */
  async catalogForRequest(
    request: Request,
  ): Promise<SessionCatalog | null | undefined> {
    await this.ensureMigrations();
    return await this.sessionCatalogHandler?.(request);
  }

  /**
   * Resolves the concrete Gestalt subject for a verified hosted HTTP request,
   * if the app opts into subject resolution.
   */
  async resolveHTTPSubject(
    request: HTTPSubjectRequest,
    context: HTTPSubjectResolutionContext,
  ): Promise<SubjectInput | null | undefined> {
    await this.ensureMigrations();
    return await this.httpSubjectResolver?.(
      cloneHTTPSubjectRequest(request),
      cloneHTTPSubjectResolutionContext(context),
    );
  }

  /**
   * Returns the static catalog emitted during provider startup.
   */
  staticCatalog(): Catalog {
    const catalog: Catalog = {
      operations: [...this.operations.values()].map<CatalogOperation>(
        (entry) => {
          const operationCatalog: CatalogOperation = {
            id: entry.id,
            method: normalizeMethod(entry.method),
          };
          if (entry.title) {
            operationCatalog.title = entry.title;
          }
          if (entry.description) {
            operationCatalog.description = entry.description;
          }
          const parameters = schemaToParameters(
            entry.input as Schema<unknown> | undefined,
          );
          if (parameters.length > 0) {
            operationCatalog.parameters = parameters;
          }
          const inputSchema = schemaToCatalogSchema(
            entry.input as Schema<unknown> | undefined,
          );
          if (inputSchema !== undefined) {
            operationCatalog.inputSchema = inputSchema;
          }
          const outputSchema = schemaToCatalogSchema(
            entry.output as Schema<unknown> | undefined,
          );
          if (outputSchema !== undefined) {
            operationCatalog.outputSchema = outputSchema;
          }
          if (entry.tags && entry.tags.length > 0) {
            operationCatalog.tags = [...entry.tags];
          }
          if (entry.allowedRoles && entry.allowedRoles.length > 0) {
            operationCatalog.allowedRoles = [...entry.allowedRoles];
          }
          if (entry.readOnly !== undefined) {
            operationCatalog.readOnly = entry.readOnly;
          }
          if (entry.visible !== undefined) {
            operationCatalog.visible = entry.visible;
          }
          return operationCatalog;
        },
      ),
    };

    if (this.name) {
      catalog.name = this.name;
    }
    if (this.displayName) {
      catalog.displayName = this.displayName;
    }
    if (this.description) {
      catalog.description = this.description;
    }
    if (this.iconSvg) {
      catalog.iconSvg = this.iconSvg;
    }
    return catalog;
  }

  /**
   * Writes the provider's static catalog to disk as YAML.
   */
  writeCatalog(path: string): void {
    writeCatalogYaml(path, this.staticCatalog());
  }

  /**
   * Returns the static catalog serialized as JSON.
   */
  catalogJson(): string {
    return catalogToJson(this.staticCatalog());
  }

  /**
   * Executes an operation against validated input and request metadata.
   */
  async execute(
    operationId: string,
    params: Record<string, unknown>,
    request: Request,
  ): Promise<OperationResult> {
    try {
      await this.ensureMigrations();
    } catch (error) {
      return errorResult(500, errorMessage(error));
    }

    const entry = this.operations.get(operationId);
    if (!entry) {
      return errorResult(404, "unknown operation");
    }

    let input: unknown = undefined;
    try {
      if (entry.input) {
        input = entry.input.parse(
          normalizeOperationInput(entry.input, params),
          "$",
        );
      }
    } catch (error) {
      return errorResult(400, errorMessage(error));
    }

    try {
      const raw = await entry.handler(input, request);
      const response = isResponse(raw) ? raw : undefined;
      const responseBody = response === undefined ? raw : response.body;
      const body = entry.output
        ? entry.output.parse(responseBody, "$response")
        : responseBody;

      return operationResult({
        status: response?.status ?? 200,
        headers: normalizeResponseHeaders(response?.headers),
        body: encodeOperationBody(body),
      });
    } catch (error) {
      return errorResult(500, errorMessage(error));
    }
  }
}

/**
 * Creates an app provider.
 */
export function defineApp(
  options: AppDefinitionOptions,
): AppProvider {
  return new AppProvider(options);
}

/**
 * Runtime type guard for app providers loaded from user modules.
 */
export function isAppProvider(
  value: unknown,
): value is AppProvider {
  return (
    value instanceof AppProvider ||
    (isProviderBase(value) &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "integration" &&
      "staticCatalog" in value &&
      typeof (value as { staticCatalog?: unknown }).staticCatalog === "function" &&
      "execute" in value &&
      typeof (value as { execute?: unknown }).execute === "function" &&
      "supportsSessionCatalog" in value &&
      typeof (value as { supportsSessionCatalog?: unknown }).supportsSessionCatalog === "function" &&
      "catalogForRequest" in value &&
      typeof (value as { catalogForRequest?: unknown }).catalogForRequest === "function" &&
      "resolveHTTPSubject" in value &&
      typeof (value as { resolveHTTPSubject?: unknown }).resolveHTTPSubject === "function")
  );
}

function normalizeConnectionParams(
  input: Record<string, ConnectionParamDefinition> | undefined,
): Record<string, ConnectionParamDefinition> {
  const output: Record<string, ConnectionParamDefinition> = {};
  for (const [key, value] of Object.entries(input ?? {})) {
    const entry: ConnectionParamDefinition = {};
    if (value.required !== undefined) {
      entry.required = value.required;
    }
    if (value.description?.trim()) {
      entry.description = value.description.trim();
    }
    if (value.defaultValue !== undefined) {
      entry.defaultValue = value.defaultValue;
    }
    if (value.from?.trim()) {
      entry.from = value.from.trim();
    }
    if (value.field?.trim()) {
      entry.field = value.field.trim();
    }
    output[key] = entry;
  }
  return output;
}

function normalizeMigrations(
  input: MigrationsOption | undefined,
): MigrationRunOptions | undefined {
  if (input === undefined) {
    return undefined;
  }
  if (Array.isArray(input)) {
    return input.length > 0 ? { revisions: input } : undefined;
  }
  return input.revisions.length > 0 ? input : undefined;
}

function isResponse(value: unknown): value is Response<unknown> {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  if (!(responseBrand in value)) {
    return false;
  }
  const status = (value as { status?: unknown }).status;
  return (
    status === undefined ||
    (typeof status === "number" && Number.isInteger(status))
  );
}

function normalizeMethod(value: string | undefined): string {
  return (value?.trim() || "POST").toUpperCase();
}

function normalizeAllowedRoles(value: string[] | undefined): string[] {
  const normalized: string[] = [];
  const seen = new Set<string>();
  for (const role of value ?? []) {
    const trimmed = role.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    normalized.push(trimmed);
  }
  return normalized;
}

function normalizeOperationInput(
  schema: Schema<unknown>,
  params: Record<string, unknown>,
): unknown {
  if (schema.fields) {
    return params ?? {};
  }
  const entries = Object.entries(params ?? {});
  if (entries.length === 1) {
    return entries[0]?.[1];
  }
  return params;
}

function errorResult(status: number, message: string): OperationResult {
  return operationResult({
    status,
    headers: normalizeResponseHeaders(),
    body: encodeOperationBody({
      error: message,
    }),
  });
}

function encodeOperationBody(body: unknown): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(body));
}

/**
 * Encodes a connection mode for provider metadata.
 */
export function encodeConnectionMode(mode: ConnectionMode): number {
  switch (mode) {
    case "none":
      return 1;
    case "subject":
      return 2;
    case "unspecified":
    default:
      return 0;
  }
}

/**
 * Encodes a connection parameter definition for provider metadata.
 */
export function encodeConnectionParam(value: ConnectionParamDefinition): {
  required?: boolean;
  description?: string;
  defaultValue?: string;
  from?: string;
  field?: string;
} {
  const output: {
    required?: boolean;
    description?: string;
    defaultValue?: string;
    from?: string;
    field?: string;
  } = {};
  if (value.required !== undefined) {
    output.required = value.required;
  }
  if (value.description !== undefined) {
    output.description = value.description;
  }
  if (value.defaultValue !== undefined) {
    output.defaultValue = value.defaultValue;
  }
  if (value.from !== undefined) {
    output.from = value.from;
  }
  if (value.field !== undefined) {
    output.field = value.field;
  }
  return output;
}
