/**
 * Browser REST Gestalt client options and factory.
 *
 * @module client
 */

import { normalizeAddress } from "./address.ts";
import { AgentClient } from "./client/generated/agent_client.ts";
import { AppClient } from "./client/generated/app_client.ts";
import { AuthorizationClient } from "./client/generated/authorization_client.ts";
import { IdentityClient } from "./client/generated/identity_client.ts";
import { WorkflowClient } from "./client/generated/workflow_client.ts";
import type { PublicAppInvokeRequest } from "./client/generated/types.ts";
import type {
  PublicUnaryCallOptions,
  UnaryTransport,
} from "./client/generated/unary_transport.ts";
import { createRestUnaryTransport } from "./rest_transport.ts";

export interface SessionAuth {
  readonly kind: "session";
}

export interface BearerAuth {
  readonly kind: "bearer";
  readonly token: () => string | Promise<string>;
}

export interface Unauthenticated {
  readonly kind: "unauthenticated";
}

export type Auth = SessionAuth | BearerAuth | Unauthenticated;

export interface ClientOptions {
  address?: string | URL;
  auth: Auth;
  /** Optional fetch override for testing. */
  fetch?: typeof fetch;
}

export interface GestaltClient {
  readonly address: string;
  readonly app: AppClient;
  readonly agent: AgentClient;
  readonly workflow: WorkflowClient;
  readonly identity: IdentityClient;
  readonly authorization: AuthorizationClient;
}

/** App-scoped invoke params: bind the app name once, pass only operation + params. */
export type BoundAppInvokeRequest = Omit<PublicAppInvokeRequest, "app">;

export interface BoundAppClient {
  invoke<T = unknown>(
    request: BoundAppInvokeRequest,
    callOptions?: PublicUnaryCallOptions,
  ): Promise<T>;
}

export function bindApp(client: GestaltClient, app: string): BoundAppClient {
  return {
    invoke<T>(
      request: BoundAppInvokeRequest,
      callOptions?: PublicUnaryCallOptions,
    ): Promise<T> {
      return client.app.invoke<T>({ ...request, app }, callOptions);
    },
  };
}

export type { JsonObjectInput } from "./client/runtime/rpc_support.ts";

export function session(): SessionAuth {
  return { kind: "session" };
}

export function bearer(token: () => string | Promise<string>): BearerAuth {
  return { kind: "bearer", token };
}

export function unauthenticated(): Unauthenticated {
  return { kind: "unauthenticated" };
}

export function createGestaltClient(options: ClientOptions): GestaltClient {
  const address = resolveAddress(options.address);
  const transport = createRestUnaryTransport({
    baseUrl: address,
    auth: options.auth,
    ...(options.fetch !== undefined ? { fetch: options.fetch } : {}),
  });
  return {
    address,
    app: new AppClient(transport),
    agent: new AgentClient(transport),
    workflow: new WorkflowClient(transport),
    identity: new IdentityClient(transport),
    authorization: new AuthorizationClient(transport),
  };
}

function resolveAddress(address: string | URL | undefined): string {
  if (address === undefined) {
    if (typeof globalThis.location === "undefined") {
      throw new Error("createGestaltClient requires an explicit address");
    }
    return globalThis.location.origin;
  }
  return normalizeAddress(address);
}
