import { type Catalog, type CatalogOperation, type CatalogSchema, catalogToJson, schemaToCatalogSchema, schemaToParameters, writeCatalogYaml } from "./catalog.ts";
import { errorMessage, type MaybePromise, type OperationResult, type Request, type Response } from "./api.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
import type { Schema } from "./schema.ts";

export type ConnectionMode =
  | "unspecified"
  | "none"
  | "user"
  | "identity"
  | "either";

export interface ConnectionParamDefinition {
  required?: boolean;
  description?: string;
  defaultValue?: string;
  from?: string;
  field?: string;
}

export interface OperationOptions<In, Out> {
  id: string;
  method?: string;
  title?: string;
  description?: string;
  tags?: string[];
  readOnly?: boolean;
  visible?: boolean;
  input?: Schema<In>;
  output?: Schema<Out>;
  handler: (input: In, request: Request) => MaybePromise<Out | Response<Out>>;
}

export interface OperationDefinition<In, Out> extends OperationOptions<In, Out> {}

export type SessionCatalog = Catalog | Record<string, unknown>;
export type SessionCatalogHandler = (request: Request) => MaybePromise<SessionCatalog | null | undefined>;

export interface PluginDefinitionOptions extends RuntimeProviderOptions {
  connectionMode?: ConnectionMode;
  authTypes?: string[];
  connectionParams?: Record<string, ConnectionParamDefinition>;
  iconSvg?: string;
  operations: Array<OperationDefinition<any, any>>;
  sessionCatalog?: SessionCatalogHandler;
}

export function operation<In, Out>(options: OperationOptions<In, Out>): OperationDefinition<In, Out> {
  return {
    ...options,
    id: options.id.trim(),
    method: normalizeMethod(options.method),
    title: options.title?.trim() ?? "",
    description: options.description?.trim() ?? "",
    tags: [...(options.tags ?? [])],
  };
}

export class IntegrationProvider extends RuntimeProvider {
  readonly kind = "integration" as const;
  readonly iconSvg: string;
  readonly connectionMode: ConnectionMode;
  readonly authTypes: string[];
  readonly connectionParams: Record<string, ConnectionParamDefinition>;

  private readonly sessionCatalogHandler: SessionCatalogHandler | undefined;
  private readonly operations = new Map<string, OperationDefinition<any, any>>();

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
        tags: [...(entry.tags ?? [])],
      });
    }
  }

  supportsSessionCatalog(): boolean {
    return this.sessionCatalogHandler !== undefined;
  }

  async catalogForRequest(request: Request): Promise<SessionCatalog | null | undefined> {
    return await this.sessionCatalogHandler?.(request);
  }

  staticCatalog(): Catalog {
    const catalog: Catalog = {
      operations: [...this.operations.values()].map<CatalogOperation>((entry) => {
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
        const parameters = schemaToParameters(entry.input as Schema<unknown> | undefined);
        if (parameters.length > 0) {
          operationCatalog.parameters = parameters;
        }
        const inputSchema = schemaToCatalogSchema(entry.input as Schema<unknown> | undefined);
        if (inputSchema !== undefined) {
          operationCatalog.inputSchema = inputSchema;
        }
        const outputSchema = schemaToCatalogSchema(entry.output as Schema<unknown> | undefined);
        if (outputSchema !== undefined) {
          operationCatalog.outputSchema = outputSchema;
        }
        if (entry.tags && entry.tags.length > 0) {
          operationCatalog.tags = [...entry.tags];
        }
        if (entry.readOnly !== undefined) {
          operationCatalog.readOnly = entry.readOnly;
        }
        if (entry.visible !== undefined) {
          operationCatalog.visible = entry.visible;
        }
        return operationCatalog;
      }),
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

  writeCatalog(path: string): void {
    writeCatalogYaml(path, this.staticCatalog());
  }

  catalogJson(): string {
    return catalogToJson(this.staticCatalog());
  }

  async execute(operationId: string, params: Record<string, unknown>, request: Request): Promise<OperationResult> {
    const entry = this.operations.get(operationId);
    if (!entry) {
      return errorResult(404, "unknown operation");
    }

    let input: unknown = undefined;
    try {
      if (entry.input) {
        input = entry.input.parse(normalizeOperationInput(entry.input, params), "$");
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

export const Plugin = IntegrationProvider;

export function defineIntegrationProvider(options: PluginDefinitionOptions): IntegrationProvider {
  return new IntegrationProvider(options);
}

export function definePlugin(options: PluginDefinitionOptions): IntegrationProvider {
  return new IntegrationProvider(options);
}

export function isIntegrationProvider(value: unknown): value is IntegrationProvider {
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
  if (!("body" in value) || !("status" in value)) {
    return false;
  }
  const status = (value as { status?: unknown }).status;
  return status === undefined || (typeof status === "number" && Number.isInteger(status));
}

function normalizeMethod(value: string | undefined): string {
  return (value?.trim() || "POST").toUpperCase();
}

function normalizeOperationInput(schema: Schema<unknown>, params: Record<string, unknown>): unknown {
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

export function connectionParamToProto(
  value: ConnectionParamDefinition,
): {
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
