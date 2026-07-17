/**
 * Server-side external Gestalt client options and factory.
 *
 * @module client/client
 */

import { normalizeAddress } from "./address.ts";
import { authToProvider, type Auth } from "./auth.ts";
import {
  AgentClient,
  AppClient,
  AuthorizationClient,
  ExternalCredentialsClient,
  IdentityClient,
  IndexedDBClient,
  WorkflowClient,
} from "./generated/app_client.ts";
import { createRestUnaryTransport } from "./rest_transport.ts";
import type { UnaryTransport } from "./generated/unary_transport.ts";

export {
  bearer,
  unauthenticated,
  type Auth,
  type BearerAuth,
  type Unauthenticated,
} from "./auth.ts";

export interface RestTransport {
  readonly kind: "rest";
}

export interface GrpcTransport {
  readonly kind: "grpc";
}

export interface ClientOptions {
  address: string | URL;
  transport: RestTransport | GrpcTransport;
  auth: Auth;
  /** Optional fetch override for testing or custom runtimes. */
  fetch?: typeof fetch;
}

export interface RestGestaltClient {
  readonly transport: "rest";
  readonly app: AppClient;
  readonly agent: AgentClient;
  readonly authorization: AuthorizationClient;
  readonly identity: IdentityClient;
  readonly workflow: WorkflowClient;
  close(): Promise<void>;
}

export interface GrpcGestaltClient {
  readonly transport: "grpc";
  readonly app: AppClient;
  readonly agent: AgentClient;
  readonly authorization: AuthorizationClient;
  readonly externalCredentials: ExternalCredentialsClient;
  readonly identity: IdentityClient;
  readonly indexedDB: IndexedDBClient;
  readonly workflow: WorkflowClient;
  close(): Promise<void>;
}

export type GestaltClient = RestGestaltClient | GrpcGestaltClient;

export function rest(): RestTransport {
  return { kind: "rest" };
}

export function grpc(): GrpcTransport {
  return { kind: "grpc" };
}

export function createGestaltClient(
  options: ClientOptions & { transport: RestTransport },
): Promise<RestGestaltClient>;
export function createGestaltClient(
  options: ClientOptions & { transport: GrpcTransport },
): Promise<GrpcGestaltClient>;
export function createGestaltClient(
  options: ClientOptions,
): Promise<GestaltClient>;
export async function createGestaltClient(
  options: ClientOptions,
): Promise<GestaltClient> {
  const baseUrl = normalizeAddress(options.address);
  const auth = authToProvider(options.auth);
  let transport: UnaryTransport;
  let close: () => Promise<void> = async () => {};

  if (options.transport.kind === "rest") {
    transport = createRestUnaryTransport({
      baseUrl,
      auth,
      ...(options.fetch !== undefined ? { fetch: options.fetch } : {}),
    });
    return {
      transport: "rest",
      app: new AppClient(transport),
      agent: new AgentClient(transport),
      authorization: new AuthorizationClient(transport),
      identity: new IdentityClient(transport),
      workflow: new WorkflowClient(transport),
      close,
    };
  }
  if (options.transport.kind === "grpc") {
    const { createGrpcUnaryTransport } = await import("./grpc_transport.ts");
    const grpcTransport = await createGrpcUnaryTransport({
      baseUrl,
      auth,
    });
    transport = grpcTransport;
    close = () => grpcTransport.close();
    return {
      transport: "grpc",
      app: new AppClient(transport),
      agent: new AgentClient(transport),
      authorization: new AuthorizationClient(transport),
      externalCredentials: new ExternalCredentialsClient(transport),
      identity: new IdentityClient(transport),
      indexedDB: new IndexedDBClient(transport),
      workflow: new WorkflowClient(transport),
      close,
    };
  }
  const unknownTransport: never = options.transport;
  throw new Error(`unsupported transport: ${JSON.stringify(unknownTransport)}`);
}
