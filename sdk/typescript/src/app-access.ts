import { createClient, type Client } from "@connectrpc/connect";

import { App as AppService } from "./internal/gen/v1/app_pb.ts";
import type { OperationResult, Request } from "./api.ts";
import {
  decodeAppResult,
  decodeGraphQLResult,
  operationResult,
} from "./app-decode.ts";
import type { ConnectionMode } from "./app.ts";
import {
  stringListsFromProto,
  structFromObject,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
} from "./host-service.ts";

/** Options that select the target connection for an operation invocation. */
export interface AppInvokeOptions {
  /** Connected account id or name to invoke against. */
  connection?: string;
  /** Provider instance id or name to invoke against. */
  instance?: string;
  /** Idempotency key forwarded to the target operation. */
  idempotencyKey?: string;
  /** Credential mode requested for the target operation. */
  credentialMode?: ConnectionMode;
  /** Timeout in milliseconds for the host-service RPC. */
  timeoutMs?: number;
}

/** Options for invoking an app GraphQL surface. */
export interface AppGraphQLInvokeOptions
  extends Pick<AppInvokeOptions, "connection" | "instance" | "idempotencyKey" | "timeoutMs"> {
  /** GraphQL variables encoded as a JSON object. */
  variables?: JsonObjectInput;
}

/** Fakeable contract for app invocation calls. */
export interface App {
  invoke<T = unknown>(
    app: string,
    operation: string,
    params?: JsonObjectInput,
    options?: AppInvokeOptions,
  ): Promise<T>;
  invokeRaw(
    app: string,
    operation: string,
    params?: JsonObjectInput,
    options?: AppInvokeOptions,
  ): Promise<OperationResult>;
  invokeGraphQL<T = unknown>(
    app: string,
    document: string,
    options?: AppGraphQLInvokeOptions,
  ): Promise<T>;
  invokeGraphQLRaw(
    app: string,
    document: string,
    options?: AppGraphQLInvokeOptions,
  ): Promise<OperationResult>;
}

/**
 * Transport-backed implementation for invoking sibling app operations.
 *
 * The constructor accepts a Gestalt request and forwards its host request
 * context to every operation and GraphQL request.
 */
class AppImpl implements App {
  private readonly client: Client<typeof AppService>;
  private readonly request: Request;

  constructor(request: Request);
  constructor(request: Request) {
    this.request = request;

    const { target, token } = requireHostServiceTarget("app");
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("app", target),
      hostServiceMetadataInterceptors(token, ""),
    );
    this.client = createClient(AppService, transport);
  }

  /** Invokes one operation on another app. */
  async invoke<T = unknown>(
    app: string,
    operation: string,
    params: JsonObjectInput = {},
    options?: AppInvokeOptions,
  ): Promise<T> {
    return decodeAppResult<T>(
      app,
      operation,
      await this.invokeRaw(app, operation, params, options),
    );
  }

  /** Invokes one operation on another app and returns the raw transport result. */
  async invokeRaw(
    app: string,
    operation: string,
    params: JsonObjectInput = {},
    options?: AppInvokeOptions,
  ): Promise<OperationResult> {
    const response = await this.client.invoke({
      app,
      operation,
      params: structFromObject(params),
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
      idempotencyKey: options?.idempotencyKey?.trim() ?? "",
      credentialMode: options?.credentialMode?.trim() ?? "",
      context: this.request.__requestContext,
    }, connectOptions(options?.timeoutMs));
    return operationResult({
      status: response.status,
      headers: stringListsFromProto(response.headers),
      body: response.body,
    });
  }

  /** Invokes another plugin's GraphQL surface. */
  async invokeGraphQL<T = unknown>(
    app: string,
    document: string,
    options?: AppGraphQLInvokeOptions,
  ): Promise<T> {
    return decodeGraphQLResult<T>(
      app,
      await this.invokeGraphQLRaw(app, document, options),
    );
  }

  /** Invokes another plugin's GraphQL surface and returns the raw transport result. */
  async invokeGraphQLRaw(
    app: string,
    document: string,
    options?: AppGraphQLInvokeOptions,
  ): Promise<OperationResult> {
    const trimmedDocument = document.trim();
    if (!trimmedDocument) {
      throw new Error("app: graphql document is required");
    }

    const response = await this.client.invokeGraphQL({
      app,
      document: trimmedDocument,
      ...(options?.variables !== undefined
        ? { variables: structFromObject(options.variables) }
        : {}),
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
      idempotencyKey: options?.idempotencyKey?.trim() ?? "",
      context: this.request.__requestContext,
    }, connectOptions(options?.timeoutMs));
    return operationResult({
      status: response.status,
      headers: stringListsFromProto(response.headers),
      body: response.body,
    });
  }

}

export const App = AppImpl;

function connectOptions(timeoutMs?: number | undefined) {
  if (timeoutMs === undefined || timeoutMs <= 0) {
    return undefined;
  }
  return {
    timeoutMs,
  };
}
