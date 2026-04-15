import {
  type Catalog,
  type CatalogOperation,
  type CatalogSchema,
  catalogToJson,
  schemaToCatalogSchema,
  schemaToParameters,
  writeCatalogYaml,
} from "./catalog.ts";
import {
  errorMessage,
  type MaybePromise,
  type OperationResult,
  type Request,
  responseBrand,
  type Response,
} from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
import type { Schema } from "./schema.ts";

/**
 * How an integration provider expects to authenticate or connect.
 */
export type ConnectionMode =
  | "unspecified"
  | "none"
  | "user"
  | "identity"
  | "either";

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
 * Operation definition accepted by {@link operation} and {@link definePlugin}.
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
 * Normalized integration operation definition.
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
 * Runtime hooks required to implement an integration provider.
 */
export interface PluginDefinitionOptions extends RuntimeProviderOptions {
  connectionMode?: ConnectionMode;
  authTypes?: string[];
  connectionParams?: Record<string, ConnectionParamDefinition>;
  iconSvg?: string;
  operations: Array<OperationDefinition<any, any>>;
  sessionCatalog?: SessionCatalogHandler;
}

/**
 * Alias for integration providers to mirror the Go and Python SDKs.
 */
export type IntegrationProviderOptions = PluginDefinitionOptions;

/**
 * Normalizes an integration operation definition.
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
 * Integration provider implementation consumed by the Gestalt runtime.
 *
 * @example
 * ```ts
 * import { defineIntegrationProvider, ok, operation, s } from "@valon-technologies/gestalt";
 *
 * export const provider = defineIntegrationProvider({
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
export class IntegrationProvider extends RuntimeProvider {
  readonly kind = "integration" as const;
  readonly iconSvg: string;
  readonly connectionMode: ConnectionMode;
  readonly authTypes: string[];
  readonly connectionParams: Record<string, ConnectionParamDefinition>;

  private readonly sessionCatalogHandler: SessionCatalogHandler | undefined;
  private readonly operations = new Map<
    string,
    OperationDefinition<any, any>
  >();

  constructor(options: PluginDefinitionOptions) {
    super(options);
    this.iconSvg = options.iconSvg?.trim() ?? "";
    this.connectionMode = options.connectionMode ?? "unspecified";
    this.authTypes = [...(options.authTypes ?? [])];
    this.connectionParams = normalizeConnectionParams(options.connectionParams);
    this.sessionCatalogHandler = options.sessionCatalog;

    for (const entry of options.operations) {
      const id = entry.id.trim();
      if (!id) {
        throw new Error("operation id is required");
      }
      if (this.operations.has(id)) {
        throw new Error(`duplicate operation id ${JSON.stringify(id)}`);
      }
      this.operations.set(id, {
        ...entry,
        id,
        method: normalizeMethod(entry.method),
        title: entry.title?.trim() ?? "",
        description: entry.description?.trim() ?? "",
        allowedRoles: normalizeAllowedRoles(entry.allowedRoles),
        tags: [...(entry.tags ?? [])],
      });
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
    return await this.sessionCatalogHandler?.(request);
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
      const response = isResponse(raw) ? raw : { status: 200, body: raw };
      const body = entry.output
        ? entry.output.parse(response.body, "$response")
        : response.body;

      return {
        status: response.status ?? 200,
        body: JSON.stringify(body),
      };
    } catch (error) {
      return errorResult(500, errorMessage(error));
    }
  }
}

/**
 * Backwards-compatible alias for the integration provider class.
 */
export const Plugin = IntegrationProvider;

/**
 * Creates an integration provider.
 */
export function defineIntegrationProvider(
  options: PluginDefinitionOptions,
): IntegrationProvider {
  return new IntegrationProvider(options);
}

/**
 * Backwards-compatible alias for {@link defineIntegrationProvider}.
 */
export function definePlugin(
  options: PluginDefinitionOptions,
): IntegrationProvider {
  return new IntegrationProvider(options);
}

/**
 * Runtime type guard for integration providers loaded from user modules.
 */
export function isIntegrationProvider(
  value: unknown,
): value is IntegrationProvider {
  return (
    value instanceof IntegrationProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "integration" &&
      "staticCatalog" in value &&
      "execute" in value)
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
  return {
    status,
    body: JSON.stringify({
      error: message,
    }),
  };
}

/**
 * Converts a connection mode into the shared protocol enum value.
 */
export function connectionModeToProtoValue(mode: ConnectionMode): number {
  switch (mode) {
    case "none":
      return 1;
    case "user":
      return 2;
    case "identity":
      return 3;
    case "either":
      return 4;
    case "unspecified":
    default:
      return 0;
  }
}

/**
 * Converts a connection parameter definition into protocol wire metadata.
 */
export function connectionParamToProto(value: ConnectionParamDefinition): {
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
