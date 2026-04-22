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
  type HTTPAck,
  type HTTPBinding,
  type HTTPRequestBody,
  type HTTPSecurityScheme,
  type PluginManifestMetadata,
  hasPluginManifestMetadata,
  writeManifestMetadataYaml,
} from "./manifest-metadata.ts";
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
 * How a plugin provider expects to authenticate or connect.
 */
export type ConnectionMode =
  | "unspecified"
  | "none"
  | "user"
  | "identity";

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
 * Normalized plugin operation definition.
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
 * Runtime hooks required to implement a plugin provider.
 */
export interface PluginDefinitionOptions extends RuntimeProviderOptions {
  connectionMode?: ConnectionMode;
  authTypes?: string[];
  connectionParams?: Record<string, ConnectionParamDefinition>;
  securitySchemes?: Record<string, HTTPSecurityScheme>;
  http?: Record<string, HTTPBinding>;
  iconSvg?: string;
  operations: Array<OperationDefinition<any, any>>;
  sessionCatalog?: SessionCatalogHandler;
}

/**
 * Normalizes a plugin operation definition.
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
 * Plugin provider implementation consumed by the Gestalt runtime.
 *
 * @example
 * ```ts
 * import { definePlugin, ok, operation, s } from "@valon-technologies/gestalt";
 *
 * export const plugin = definePlugin({
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
export class PluginProvider extends RuntimeProvider {
  readonly kind = "integration" as const;
  readonly iconSvg: string;
  readonly connectionMode: ConnectionMode;
  readonly authTypes: string[];
  readonly connectionParams: Record<string, ConnectionParamDefinition>;
  readonly securitySchemes: Record<string, HTTPSecurityScheme>;
  readonly http: Record<string, HTTPBinding>;

  private readonly sessionCatalogHandler: SessionCatalogHandler | undefined;
  private readonly operations = new Map<string, OperationDefinition<any, any>>();

  constructor(options: PluginDefinitionOptions) {
    super(options);
    this.iconSvg = options.iconSvg?.trim() ?? "";
    this.connectionMode = options.connectionMode ?? "unspecified";
    this.authTypes = [...(options.authTypes ?? [])];
    this.connectionParams = normalizeConnectionParams(options.connectionParams);
    this.securitySchemes = normalizeHTTPSecuritySchemes(options.securitySchemes);
    this.http = normalizeHTTPBindings(options.http);
    this.sessionCatalogHandler = options.sessionCatalog;

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
   * Returns generated manifest-backed HTTP/security metadata for the provider.
   */
  staticManifestMetadata(): PluginManifestMetadata {
    const metadata: PluginManifestMetadata = {};
    if (Object.keys(this.securitySchemes).length > 0) {
      metadata.securitySchemes = cloneHTTPSecuritySchemes(this.securitySchemes);
    }
    if (Object.keys(this.http).length > 0) {
      metadata.http = cloneHTTPBindings(this.http);
    }
    return metadata;
  }

  /**
   * Reports whether the provider emits manifest metadata in addition to catalog metadata.
   */
  supportsManifestMetadata(): boolean {
    return hasPluginManifestMetadata(this.staticManifestMetadata());
  }

  /**
   * Writes generated manifest metadata to disk as YAML.
   */
  writeManifestMetadata(path: string): void {
    const metadata = this.staticManifestMetadata();
    if (!hasPluginManifestMetadata(metadata)) {
      return;
    }
    writeManifestMetadataYaml(path, metadata);
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
 * Creates a plugin provider.
 */
export function definePlugin(
  options: PluginDefinitionOptions,
): PluginProvider {
  return new PluginProvider(options);
}

/**
 * Runtime type guard for plugin providers loaded from user modules.
 */
export function isPluginProvider(
  value: unknown,
): value is PluginProvider {
  return (
    value instanceof PluginProvider ||
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

function normalizeHTTPSecuritySchemes(
  input: Record<string, HTTPSecurityScheme> | undefined,
): Record<string, HTTPSecurityScheme> {
  const output: Record<string, HTTPSecurityScheme> = {};
  for (const [key, value] of Object.entries(input ?? {})) {
    output[key] = cloneHTTPSecurityScheme(value);
  }
  return output;
}

function cloneHTTPSecuritySchemes(
  input: Record<string, HTTPSecurityScheme>,
): Record<string, HTTPSecurityScheme> {
  const output: Record<string, HTTPSecurityScheme> = {};
  for (const [key, value] of Object.entries(input)) {
    output[key] = cloneHTTPSecurityScheme(value);
  }
  return output;
}

function cloneHTTPSecurityScheme(value: HTTPSecurityScheme): HTTPSecurityScheme {
  const output: HTTPSecurityScheme = {};
  if (value.type !== undefined) {
    output.type = value.type;
  }
  if (value.description !== undefined) {
    output.description = value.description;
  }
  if (value.signatureHeader !== undefined) {
    output.signatureHeader = value.signatureHeader;
  }
  if (value.signaturePrefix !== undefined) {
    output.signaturePrefix = value.signaturePrefix;
  }
  if (value.payloadTemplate !== undefined) {
    output.payloadTemplate = value.payloadTemplate;
  }
  if (value.timestampHeader !== undefined) {
    output.timestampHeader = value.timestampHeader;
  }
  if (value.maxAgeSeconds !== undefined) {
    output.maxAgeSeconds = value.maxAgeSeconds;
  }
  if (value.name !== undefined) {
    output.name = value.name;
  }
  if (value.in !== undefined) {
    output.in = value.in;
  }
  if (value.scheme !== undefined) {
    output.scheme = value.scheme;
  }
  if (value.secret) {
    output.secret = {
      ...value.secret,
    };
  }
  return output;
}

function normalizeHTTPBindings(
  input: Record<string, HTTPBinding> | undefined,
): Record<string, HTTPBinding> {
  const output: Record<string, HTTPBinding> = {};
  for (const [key, value] of Object.entries(input ?? {})) {
    output[key] = cloneHTTPBinding(value);
  }
  return output;
}

function cloneHTTPBindings(
  input: Record<string, HTTPBinding>,
): Record<string, HTTPBinding> {
  const output: Record<string, HTTPBinding> = {};
  for (const [key, value] of Object.entries(input)) {
    output[key] = cloneHTTPBinding(value);
  }
  return output;
}

function cloneHTTPBinding(value: HTTPBinding): HTTPBinding {
  const output: HTTPBinding = {
    path: value.path,
    method: value.method,
    security: value.security,
    target: value.target,
  };
  if (value.requestBody) {
    output.requestBody = cloneHTTPRequestBody(value.requestBody);
  }
  if (value.ack) {
    output.ack = cloneHTTPAck(value.ack);
  }
  return output;
}

function cloneHTTPRequestBody(value: HTTPRequestBody): HTTPRequestBody {
  const output: HTTPRequestBody = {};
  if (value.required !== undefined) {
    output.required = value.required;
  }
  if (value.content) {
    output.content = {};
    for (const key of Object.keys(value.content)) {
      output.content[key] = {};
    }
  }
  return output;
}

function cloneHTTPAck(value: HTTPAck): HTTPAck {
  const output: HTTPAck = {};
  if (value.status !== undefined) {
    output.status = value.status;
  }
  if (value.headers) {
    output.headers = {
      ...value.headers,
    };
  }
  if (value.body !== undefined) {
    output.body = cloneHTTPBodyValue(value.body);
  }
  return output;
}

function cloneHTTPBodyValue<T>(value: T): T {
  return structuredClone(value);
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
