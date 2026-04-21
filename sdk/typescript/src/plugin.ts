import { writeFileSync } from "node:fs";

import YAML from "yaml";

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
 * Webhook execution modes supported by hosted ingress.
 */
export type WebhookExecutionMode = "sync" | "async_ack";

/**
 * Webhook security scheme types supported by hosted ingress.
 */
export type WebhookSecuritySchemeType =
  | "hmac"
  | "apiKey"
  | "http"
  | "mutualTLS"
  | "none";

/**
 * API-key webhook locations supported by hosted ingress.
 */
export type WebhookIn = "header" | "query";

/**
 * HTTP auth schemes supported by hosted ingress.
 */
export type WebhookHTTPScheme = "basic" | "bearer";

/**
 * Secret reference used by a hosted webhook security scheme.
 */
export interface WebhookSecretRef {
  env?: string;
  secret?: string;
}

/**
 * HMAC signature verification settings for hosted webhooks.
 */
export interface WebhookSignatureConfig {
  algorithm?: string;
  signatureHeader?: string;
  timestampHeader?: string;
  deliveryIdHeader?: string;
  payloadTemplate?: string;
  digestPrefix?: string;
}

/**
 * Replay protection settings for hosted webhooks.
 */
export interface WebhookReplayConfig {
  maxAge?: string;
}

/**
 * Mutual TLS verification settings for hosted webhooks.
 */
export interface WebhookMTLSConfig {
  subjectAltName?: string;
}

/**
 * Authored hosted webhook security scheme definition.
 */
export interface WebhookSecuritySchemeDefinition {
  type: WebhookSecuritySchemeType;
  description?: string;
  name?: string;
  in?: WebhookIn;
  scheme?: WebhookHTTPScheme;
  secret?: WebhookSecretRef;
  signature?: WebhookSignatureConfig;
  replay?: WebhookReplayConfig;
  mtls?: WebhookMTLSConfig;
}

/**
 * Hosted webhook content metadata for a single media type.
 */
export interface WebhookMediaTypeDefinition {
  schema?: Schema<unknown> | Record<string, unknown>;
}

/**
 * Hosted webhook request body definition.
 */
export interface WebhookRequestBodyDefinition {
  required?: boolean;
  content?: Record<string, WebhookMediaTypeDefinition>;
}

/**
 * Hosted webhook response definition.
 */
export interface WebhookResponseDefinition {
  description?: string;
  headers?: Record<string, string>;
  content?: Record<string, WebhookMediaTypeDefinition>;
  body?: unknown;
}

/**
 * OpenAPI-style security requirement mapping for a webhook operation.
 */
export type SecurityRequirement = Record<string, string[]>;

/**
 * Hosted webhook operation metadata.
 */
export interface WebhookOperationDefinition {
  operationId?: string;
  summary?: string;
  description?: string;
  requestBody?: WebhookRequestBodyDefinition;
  responses?: Record<string, WebhookResponseDefinition>;
  security?: SecurityRequirement[];
}

/**
 * Hosted webhook workflow target metadata.
 */
export interface WebhookWorkflowTargetDefinition {
  provider?: string;
  plugin?: string;
  operation?: string;
  connection?: string;
  instance?: string;
  input?: Record<string, unknown>;
}

/**
 * Hosted webhook target definition.
 */
export interface WebhookTargetDefinition {
  operation?: string;
  workflow?: WebhookWorkflowTargetDefinition;
}

/**
 * Hosted webhook execution settings.
 */
export interface WebhookExecutionDefinition {
  mode?: WebhookExecutionMode;
  acceptedResponse?: string;
}

/**
 * Authored hosted webhook definition.
 */
export interface WebhookDefinition {
  summary?: string;
  description?: string;
  path?: string;
  get?: WebhookOperationDefinition;
  post?: WebhookOperationDefinition;
  put?: WebhookOperationDefinition;
  delete?: WebhookOperationDefinition;
  target?: WebhookTargetDefinition;
  execution?: WebhookExecutionDefinition;
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
  iconSvg?: string;
  securitySchemes?: Record<string, WebhookSecuritySchemeDefinition>;
  webhooks?: Record<string, WebhookDefinition>;
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
  readonly securitySchemes: Record<string, WebhookSecuritySchemeDefinition>;
  readonly webhooks: Record<string, WebhookDefinition>;

  private readonly sessionCatalogHandler: SessionCatalogHandler | undefined;
  private readonly operations = new Map<string, OperationDefinition<any, any>>();

  constructor(options: PluginDefinitionOptions) {
    super(options);
    this.iconSvg = options.iconSvg?.trim() ?? "";
    this.connectionMode = options.connectionMode ?? "unspecified";
    this.authTypes = [...(options.authTypes ?? [])];
    this.connectionParams = normalizeConnectionParams(options.connectionParams);
    this.securitySchemes = normalizeSecuritySchemes(options.securitySchemes);
    this.webhooks = normalizeWebhooks(options.webhooks);
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
   * Returns manifest metadata emitted alongside the provider manifest.
   */
  manifestMetadata(): Record<string, unknown> {
    return pluginManifestMetadataObject(this);
  }

  /**
   * Serializes manifest metadata to YAML for host-side merge workflows.
   */
  manifestMetadataYaml(): string {
    return YAML.stringify(this.manifestMetadata());
  }

  /**
   * Writes manifest metadata to disk as YAML.
   */
  writeManifestMetadata(path: string): void {
    writeFileSync(path, this.manifestMetadataYaml(), "utf8");
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

function normalizeSecuritySchemes(
  input: Record<string, WebhookSecuritySchemeDefinition> | undefined,
): Record<string, WebhookSecuritySchemeDefinition> {
  const output: Record<string, WebhookSecuritySchemeDefinition> = {};
  for (const [key, value] of Object.entries(input ?? {})) {
    const entry: WebhookSecuritySchemeDefinition = {
      type: value.type,
    };
    if (value.description?.trim()) {
      entry.description = value.description.trim();
    }
    if (value.name?.trim()) {
      entry.name = value.name.trim();
    }
    if (value.in?.trim()) {
      entry.in = value.in.trim() as WebhookIn;
    }
    if (value.scheme?.trim()) {
      entry.scheme = value.scheme.trim() as WebhookHTTPScheme;
    }
    if (value.secret) {
      const secret: WebhookSecretRef = {};
      if (value.secret.env?.trim()) {
        secret.env = value.secret.env.trim();
      }
      if (value.secret.secret?.trim()) {
        secret.secret = value.secret.secret.trim();
      }
      if (Object.keys(secret).length > 0) {
        entry.secret = secret;
      }
    }
    if (value.signature) {
      const signature: WebhookSignatureConfig = {};
      if (value.signature.algorithm?.trim()) {
        signature.algorithm = value.signature.algorithm.trim();
      }
      if (value.signature.signatureHeader?.trim()) {
        signature.signatureHeader = value.signature.signatureHeader.trim();
      }
      if (value.signature.timestampHeader?.trim()) {
        signature.timestampHeader = value.signature.timestampHeader.trim();
      }
      if (value.signature.deliveryIdHeader?.trim()) {
        signature.deliveryIdHeader = value.signature.deliveryIdHeader.trim();
      }
      if (value.signature.payloadTemplate?.trim()) {
        signature.payloadTemplate = value.signature.payloadTemplate.trim();
      }
      if (value.signature.digestPrefix?.trim()) {
        signature.digestPrefix = value.signature.digestPrefix.trim();
      }
      if (Object.keys(signature).length > 0) {
        entry.signature = signature;
      }
    }
    if (value.replay?.maxAge?.trim()) {
      entry.replay = {
        maxAge: value.replay.maxAge.trim(),
      };
    }
    if (value.mtls?.subjectAltName?.trim()) {
      entry.mtls = {
        subjectAltName: value.mtls.subjectAltName.trim(),
      };
    }
    output[key] = entry;
  }
  return output;
}

function normalizeWebhooks(
  input: Record<string, WebhookDefinition> | undefined,
): Record<string, WebhookDefinition> {
  const output: Record<string, WebhookDefinition> = {};
  for (const [key, value] of Object.entries(input ?? {})) {
    const entry: WebhookDefinition = {};
    if (value.summary?.trim()) {
      entry.summary = value.summary.trim();
    }
    if (value.description?.trim()) {
      entry.description = value.description.trim();
    }
    if (value.path?.trim()) {
      entry.path = value.path.trim();
    }
    if (value.get) {
      entry.get = normalizeWebhookOperation(value.get);
    }
    if (value.post) {
      entry.post = normalizeWebhookOperation(value.post);
    }
    if (value.put) {
      entry.put = normalizeWebhookOperation(value.put);
    }
    if (value.delete) {
      entry.delete = normalizeWebhookOperation(value.delete);
    }
    if (value.target) {
      entry.target = normalizeWebhookTarget(value.target);
    }
    if (value.execution) {
      entry.execution = normalizeWebhookExecution(value.execution);
    }
    output[key] = entry;
  }
  return output;
}

function normalizeWebhookOperation(
  value: WebhookOperationDefinition,
): WebhookOperationDefinition {
  const entry: WebhookOperationDefinition = {};
  if (value.operationId?.trim()) {
    entry.operationId = value.operationId.trim();
  }
  if (value.summary?.trim()) {
    entry.summary = value.summary.trim();
  }
  if (value.description?.trim()) {
    entry.description = value.description.trim();
  }
  if (value.requestBody) {
    entry.requestBody = normalizeWebhookRequestBody(value.requestBody);
  }
  if (value.responses) {
    entry.responses = Object.fromEntries(
      Object.entries(value.responses).map(([status, response]) => [
        status,
        normalizeWebhookResponse(response),
      ]),
    );
  }
  if (value.security && value.security.length > 0) {
    entry.security = value.security.map((requirement) =>
      Object.fromEntries(
        Object.entries(requirement).map(([name, scopes]) => [
          name.trim(),
          normalizeRequirementScopes(scopes),
        ]),
      ),
    );
  }
  return entry;
}

function normalizeWebhookRequestBody(
  value: WebhookRequestBodyDefinition,
): WebhookRequestBodyDefinition {
  const entry: WebhookRequestBodyDefinition = {};
  if (value.required !== undefined) {
    entry.required = value.required;
  }
  if (value.content) {
    entry.content = Object.fromEntries(
      Object.entries(value.content).map(([contentType, mediaType]) => [
        contentType,
        normalizeWebhookMediaType(mediaType),
      ]),
    );
  }
  return entry;
}

function normalizeWebhookResponse(
  value: WebhookResponseDefinition,
): WebhookResponseDefinition {
  const entry: WebhookResponseDefinition = {};
  if (value.description?.trim()) {
    entry.description = value.description.trim();
  }
  if (value.headers) {
    entry.headers = Object.fromEntries(
      Object.entries(value.headers).map(([name, headerValue]) => [
        name,
        headerValue,
      ]),
    );
  }
  if (value.content) {
    entry.content = Object.fromEntries(
      Object.entries(value.content).map(([contentType, mediaType]) => [
        contentType,
        normalizeWebhookMediaType(mediaType),
      ]),
    );
  }
  if (value.body !== undefined) {
    entry.body = cloneUnknown(value.body);
  }
  return entry;
}

function normalizeWebhookMediaType(
  value: WebhookMediaTypeDefinition,
): WebhookMediaTypeDefinition {
  const entry: WebhookMediaTypeDefinition = {};
  if (value.schema !== undefined) {
    entry.schema = isSchema(value.schema)
      ? value.schema
      : cloneUnknown(value.schema) as Record<string, unknown>;
  }
  return entry;
}

function normalizeWebhookTarget(
  value: WebhookTargetDefinition,
): WebhookTargetDefinition {
  const entry: WebhookTargetDefinition = {};
  if (value.operation?.trim()) {
    entry.operation = value.operation.trim();
  }
  if (value.workflow) {
    const workflow: WebhookWorkflowTargetDefinition = {};
    if (value.workflow.provider?.trim()) {
      workflow.provider = value.workflow.provider.trim();
    }
    if (value.workflow.plugin?.trim()) {
      workflow.plugin = value.workflow.plugin.trim();
    }
    if (value.workflow.operation?.trim()) {
      workflow.operation = value.workflow.operation.trim();
    }
    if (value.workflow.connection?.trim()) {
      workflow.connection = value.workflow.connection.trim();
    }
    if (value.workflow.instance?.trim()) {
      workflow.instance = value.workflow.instance.trim();
    }
    if (value.workflow.input) {
      workflow.input = cloneUnknown(value.workflow.input) as Record<
        string,
        unknown
      >;
    }
    if (Object.keys(workflow).length > 0) {
      entry.workflow = workflow;
    }
  }
  return entry;
}

function normalizeWebhookExecution(
  value: WebhookExecutionDefinition,
): WebhookExecutionDefinition {
  const entry: WebhookExecutionDefinition = {};
  if (value.mode?.trim()) {
    entry.mode = value.mode.trim() as WebhookExecutionMode;
  }
  if (value.acceptedResponse?.trim()) {
    entry.acceptedResponse = value.acceptedResponse.trim();
  }
  return entry;
}

function normalizeRequirementScopes(scopes: string[]): string[] {
  const output: string[] = [];
  const seen = new Set<string>();
  for (const scope of scopes) {
    const trimmed = scope.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    output.push(trimmed);
  }
  return output;
}

function pluginManifestMetadataObject(
  provider: PluginProvider,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (Object.keys(provider.securitySchemes).length > 0) {
    output.securitySchemes = Object.fromEntries(
      Object.entries(provider.securitySchemes).map(([name, scheme]) => [
        name,
        webhookSecuritySchemeObject(scheme),
      ]),
    );
  }
  if (Object.keys(provider.webhooks).length > 0) {
    output.webhooks = Object.fromEntries(
      Object.entries(provider.webhooks).map(([name, webhook]) => [
        name,
        webhookDefinitionObject(webhook),
      ]),
    );
  }
  return output;
}

function webhookSecuritySchemeObject(
  value: WebhookSecuritySchemeDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {
    type: value.type,
  };
  if (value.description) {
    output.description = value.description;
  }
  if (value.name) {
    output.name = value.name;
  }
  if (value.in) {
    output.in = value.in;
  }
  if (value.scheme) {
    output.scheme = value.scheme;
  }
  if (value.secret && Object.keys(value.secret).length > 0) {
    output.secret = {
      ...value.secret,
    };
  }
  if (value.signature && Object.keys(value.signature).length > 0) {
    output.signature = {
      ...value.signature,
    };
  }
  if (value.replay && Object.keys(value.replay).length > 0) {
    output.replay = {
      ...value.replay,
    };
  }
  if (value.mtls && Object.keys(value.mtls).length > 0) {
    output.mtls = {
      ...value.mtls,
    };
  }
  return output;
}

function webhookDefinitionObject(
  value: WebhookDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.summary) {
    output.summary = value.summary;
  }
  if (value.description) {
    output.description = value.description;
  }
  if (value.path) {
    output.path = value.path;
  }
  if (value.get) {
    output.get = webhookOperationObject(value.get);
  }
  if (value.post) {
    output.post = webhookOperationObject(value.post);
  }
  if (value.put) {
    output.put = webhookOperationObject(value.put);
  }
  if (value.delete) {
    output.delete = webhookOperationObject(value.delete);
  }
  if (value.target) {
    output.target = webhookTargetObject(value.target);
  }
  if (value.execution) {
    output.execution = webhookExecutionObject(value.execution);
  }
  return output;
}

function webhookOperationObject(
  value: WebhookOperationDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.operationId) {
    output.operationId = value.operationId;
  }
  if (value.summary) {
    output.summary = value.summary;
  }
  if (value.description) {
    output.description = value.description;
  }
  if (value.requestBody) {
    output.requestBody = webhookRequestBodyObject(value.requestBody);
  }
  if (value.responses && Object.keys(value.responses).length > 0) {
    output.responses = Object.fromEntries(
      Object.entries(value.responses).map(([status, response]) => [
        status,
        webhookResponseObject(response),
      ]),
    );
  }
  if (value.security && value.security.length > 0) {
    output.security = value.security.map((requirement) => ({
      ...requirement,
    }));
  }
  return output;
}

function webhookRequestBodyObject(
  value: WebhookRequestBodyDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.required !== undefined) {
    output.required = value.required;
  }
  if (value.content && Object.keys(value.content).length > 0) {
    output.content = Object.fromEntries(
      Object.entries(value.content).map(([contentType, mediaType]) => [
        contentType,
        webhookMediaTypeObject(mediaType),
      ]),
    );
  }
  return output;
}

function webhookResponseObject(
  value: WebhookResponseDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.description) {
    output.description = value.description;
  }
  if (value.headers && Object.keys(value.headers).length > 0) {
    output.headers = {
      ...value.headers,
    };
  }
  if (value.content && Object.keys(value.content).length > 0) {
    output.content = Object.fromEntries(
      Object.entries(value.content).map(([contentType, mediaType]) => [
        contentType,
        webhookMediaTypeObject(mediaType),
      ]),
    );
  }
  if (value.body !== undefined) {
    output.body = cloneUnknown(value.body);
  }
  return output;
}

function webhookMediaTypeObject(
  value: WebhookMediaTypeDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.schema !== undefined) {
    output.schema = isSchema(value.schema)
      ? schemaToCatalogSchema(value.schema)
      : cloneUnknown(value.schema);
  }
  return output;
}

function webhookTargetObject(
  value: WebhookTargetDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.operation) {
    output.operation = value.operation;
  }
  if (value.workflow) {
    const workflow: Record<string, unknown> = {};
    if (value.workflow.provider) {
      workflow.provider = value.workflow.provider;
    }
    if (value.workflow.plugin) {
      workflow.plugin = value.workflow.plugin;
    }
    if (value.workflow.operation) {
      workflow.operation = value.workflow.operation;
    }
    if (value.workflow.connection) {
      workflow.connection = value.workflow.connection;
    }
    if (value.workflow.instance) {
      workflow.instance = value.workflow.instance;
    }
    if (value.workflow.input) {
      workflow.input = cloneUnknown(value.workflow.input);
    }
    if (Object.keys(workflow).length > 0) {
      output.workflow = workflow;
    }
  }
  return output;
}

function webhookExecutionObject(
  value: WebhookExecutionDefinition,
): Record<string, unknown> {
  const output: Record<string, unknown> = {};
  if (value.mode) {
    output.mode = value.mode;
  }
  if (value.acceptedResponse) {
    output.acceptedResponse = value.acceptedResponse;
  }
  return output;
}

function isSchema(value: unknown): value is Schema<unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as Schema<unknown>).catalogType === "string" &&
    typeof (value as Schema<unknown>).parse === "function"
  );
}

function cloneUnknown<T>(value: T): T {
  if (Array.isArray(value)) {
    return value.map((entry) => cloneUnknown(entry)) as T;
  }
  if (typeof value === "object" && value !== null) {
    return Object.fromEntries(
      Object.entries(value).map(([key, entry]) => [key, cloneUnknown(entry)]),
    ) as T;
  }
  return value;
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
