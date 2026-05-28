import { createClient, type Client } from "@connectrpc/connect";

import { App as AppService } from "./internal/gen/v1/app_pb.ts";
import type { OperationResult, OperationResultHeaders, Request } from "./api.ts";
import type { ConnectionMode } from "./app.ts";
import type { StringList } from "./internal/gen/v1/app_pb.ts";
import { structFromObject, type JsonObjectInput } from "./protocol.ts";
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
}
/** Grant included when exchanging an invocation token for a child token. */
export interface AppInvocationGrant {
  /** App name that the child token may invoke. */
  app: string;
  /** Specific operation ids allowed by the child token. */
  operations?: string[];
  /** Surface names allowed by the child token. */
  surfaces?: string[];
  /** Whether the child token may invoke every operation on the app. */
  allOperations?: boolean;
}

/** Options for invoking an app GraphQL surface. */
export interface AppGraphQLInvokeOptions
  extends Pick<AppInvokeOptions, "connection" | "instance" | "idempotencyKey"> {
  /** GraphQL variables encoded as a JSON object. */
  variables?: JsonObjectInput;
}

/** Fakeable contract for app invocation calls. */
export interface App {
  invoke(
    app: string,
    operation: string,
    params?: JsonObjectInput,
    options?: AppInvokeOptions,
  ): Promise<OperationResult>;
  invokeGraphQL(
    app: string,
    document: string,
    options?: AppGraphQLInvokeOptions,
  ): Promise<OperationResult>;
  exchangeInvocationToken(options?: {
    grants?: AppInvocationGrant[];
    ttlSeconds?: number;
  }): Promise<string>;
}

/**
 * Transport-backed implementation for invoking sibling app operations.
 *
 * The constructor accepts either a Gestalt request or an invocation token. The
 * token is attached to every operation, GraphQL, and token-exchange request.
 */
class AppImpl implements App {
  private readonly client: Client<typeof AppService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const { target, token } = requireHostServiceTarget("app");
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("app", target),
      hostServiceMetadataInterceptors(token, ""),
    );
    this.client = createClient(AppService, transport);
  }

  /** Invokes one operation on another app. */
  async invoke(
    app: string,
    operation: string,
    params: JsonObjectInput = {},
    options?: AppInvokeOptions,
  ): Promise<OperationResult> {
    const response = await this.client.invoke({
      invocationToken: this.invocationToken,
      app,
      operation,
      params: structFromObject(params),
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
      idempotencyKey: options?.idempotencyKey?.trim() ?? "",
      credentialMode: options?.credentialMode?.trim() ?? "",
    });
    const headers = operationResultHeaders(response.headers);
    return {
      status: response.status,
      ...(headers === undefined ? {} : { headers }),
      body: response.body,
    };
  }

  /** Invokes another plugin's GraphQL surface. */
  async invokeGraphQL(
    app: string,
    document: string,
    options?: AppGraphQLInvokeOptions,
  ): Promise<OperationResult> {
    const trimmedDocument = document.trim();
    if (!trimmedDocument) {
      throw new Error("app: graphql document is required");
    }

    const response = await this.client.invokeGraphQL({
      invocationToken: this.invocationToken,
      app,
      document: trimmedDocument,
      ...(options?.variables !== undefined
        ? { variables: structFromObject(options.variables) }
        : {}),
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
      idempotencyKey: options?.idempotencyKey?.trim() ?? "",
    });
    const headers = operationResultHeaders(response.headers);
    return {
      status: response.status,
      ...(headers === undefined ? {} : { headers }),
      body: response.body,
    };
  }

  /** Exchanges this invocation token for a narrower child token. */
  async exchangeInvocationToken(options?: {
    /** Grants to attach to the child token. */
    grants?: AppInvocationGrant[];
    /** Requested child-token time-to-live in seconds. */
    ttlSeconds?: number;
  }): Promise<string> {
    const response = await this.client.exchangeInvocationToken({
      parentInvocationToken: this.invocationToken,
      grants: (options?.grants ?? [])
        .map((grant) => ({
          app: grant.app.trim(),
          operations: (grant.operations ?? [])
            .map((operation) => operation.trim())
            .filter(Boolean),
          surfaces: (grant.surfaces ?? [])
            .map((surface) => surface.trim().toLowerCase())
            .filter(Boolean),
          allOperations: grant.allOperations ?? false,
        }))
        .filter((grant) => grant.app.length > 0),
      ttlSeconds: BigInt(Math.max(0, options?.ttlSeconds ?? 0)),
    });
    return response.invocationToken;
  }
}

export const App = AppImpl;

function operationResultHeaders(
  headers: { [key: string]: StringList } | undefined,
): OperationResultHeaders | undefined {
  if (headers === undefined || Object.keys(headers).length === 0) {
    return undefined;
  }
  const normalized: OperationResultHeaders = {};
  for (const [name, list] of Object.entries(headers)) {
    normalized[name] = [...list.values];
  }
  return normalized;
}

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("app: invocation token is not available");
  }
  return trimmed;
}
