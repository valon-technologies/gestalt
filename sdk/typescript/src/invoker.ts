import { connect } from "node:net";

import type { JsonObject, JsonValue } from "@bufbuild/protobuf";
import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import { PluginInvoker as PluginInvokerService } from "../gen/v1/plugin_pb.ts";
import type { OperationResult, Request } from "./api.ts";

export const ENV_PLUGIN_INVOKER_SOCKET = "GESTALT_PLUGIN_INVOKER_SOCKET";

export interface PluginInvokeOptions {
  connection?: string;
  instance?: string;
}

export interface PluginInvocationGrant {
  plugin: string;
  operations?: string[];
  surfaces?: string[];
  allOperations?: boolean;
}

export interface PluginGraphQLInvokeOptions extends PluginInvokeOptions {
  variables?: Record<string, unknown>;
}

export class PluginInvoker {
  private readonly client: Client<typeof PluginInvokerService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const socketPath = process.env[ENV_PLUGIN_INVOKER_SOCKET];
    if (!socketPath) {
      throw new Error(`plugin invoker: ${ENV_PLUGIN_INVOKER_SOCKET} is not set`);
    }

    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: {
        createConnection: () => connect(socketPath),
      },
    });
    this.client = createClient(PluginInvokerService, transport);
  }

  async invoke(
    plugin: string,
    operation: string,
    params: Record<string, unknown> = {},
    options?: PluginInvokeOptions,
  ): Promise<OperationResult> {
    const response = await this.client.invoke({
      invocationToken: this.invocationToken,
      plugin,
      operation,
      params: toJsonObject(params),
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
    });
    return {
      status: response.status,
      body: response.body,
    };
  }

  async invokeGraphQL(
    plugin: string,
    document: string,
    options?: PluginGraphQLInvokeOptions,
  ): Promise<OperationResult> {
    const trimmedDocument = document.trim();
    if (!trimmedDocument) {
      throw new Error("plugin invoker: graphql document is required");
    }

    const response = await this.client.invokeGraphQL({
      invocationToken: this.invocationToken,
      plugin,
      document: trimmedDocument,
      ...(options?.variables
        ? { variables: toJsonObject(options.variables) }
        : {}),
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
    });
    return {
      status: response.status,
      body: response.body,
    };
  }

  async exchangeInvocationToken(options?: {
    grants?: PluginInvocationGrant[];
    ttlSeconds?: number;
  }): Promise<string> {
    const response = await this.client.exchangeInvocationToken({
      parentInvocationToken: this.invocationToken,
      grants: (options?.grants ?? [])
        .map((grant) => ({
          plugin: grant.plugin.trim(),
          operations: (grant.operations ?? [])
            .map((operation) => operation.trim())
            .filter(Boolean),
          surfaces: (grant.surfaces ?? [])
            .map((surface) => surface.trim().toLowerCase())
            .filter(Boolean),
          allOperations: grant.allOperations ?? false,
        }))
        .filter((grant) => grant.plugin.length > 0),
      ttlSeconds: BigInt(Math.max(0, options?.ttlSeconds ?? 0)),
    });
    return response.invocationToken;
  }
}

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("plugin invoker: invocation token is not available");
  }
  return trimmed;
}

function toJsonObject(params: Record<string, unknown>): JsonObject {
  const output: JsonObject = {};
  for (const [key, value] of Object.entries(params ?? {})) {
    if (value === undefined) {
      continue;
    }
    output[key] = toJsonValue(value);
  }
  return output;
}

function toJsonValue(value: unknown): JsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return value as JsonValue;
  }
  if (Array.isArray(value)) {
    return value.map((entry) => toJsonValue(entry));
  }
  if (typeof value === "object") {
    return toJsonObject(value as Record<string, unknown>);
  }
  throw new Error("plugin invoker: params must be JSON-serializable");
}
