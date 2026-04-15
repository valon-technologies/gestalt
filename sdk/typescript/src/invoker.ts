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

export class PluginInvoker {
  private readonly client: Client<typeof PluginInvokerService>;
  private readonly requestHandle: string;

  constructor(request: Request);
  constructor(requestHandle: string);
  constructor(requestOrHandle: Request | string) {
    const socketPath = process.env[ENV_PLUGIN_INVOKER_SOCKET];
    if (!socketPath) {
      throw new Error(`plugin invoker: ${ENV_PLUGIN_INVOKER_SOCKET} is not set`);
    }

    this.requestHandle = normalizeRequestHandle(requestOrHandle);
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
      requestHandle: this.requestHandle,
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
}

function normalizeRequestHandle(requestOrHandle: Request | string): string {
  const requestHandle =
    typeof requestOrHandle === "string"
      ? requestOrHandle
      : requestOrHandle.requestHandle;
  const trimmed = requestHandle.trim();
  if (!trimmed) {
    throw new Error("plugin invoker: request handle is required");
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
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((entry) => toJsonValue(entry));
  }
  if (typeof value === "object") {
    return toJsonObject(value as Record<string, unknown>);
  }
  throw new Error("plugin invoker: params must be JSON-serializable");
}
