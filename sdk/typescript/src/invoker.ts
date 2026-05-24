import { createClient, type Client } from "@connectrpc/connect";

import { AppInvoker as AppInvokerService } from "./internal/gen/v1/app_pb.ts";
import type { OperationResult, Request } from "./api.ts";
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
export interface AppGraphQLInvokeOptions extends AppInvokeOptions {
  /** GraphQL variables encoded as a JSON object. */
  variables?: JsonObjectInput;
}

/** Fakeable client contract for app invoker calls. */
export interface AppInvokerClientLike {
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
 * Client for invoking sibling app operations through the host.
 *
 * The constructor accepts either a Gestalt request or an invocation token. The
 * token is attached to every operation, GraphQL, and token-exchange request.
 */
export class AppInvoker implements AppInvokerClientLike {
  private readonly client: Client<typeof AppInvokerService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const { target, token } = requireHostServiceTarget("app invoker");
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("app invoker", target),
      hostServiceMetadataInterceptors(token, ""),
    );
    this.client = createClient(AppInvokerService, transport);
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
    });
    return {
      status: response.status,
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
      throw new Error("app invoker: graphql document is required");
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
    return {
      status: response.status,
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

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("app invoker: invocation token is not available");
  }
  return trimmed;
}
