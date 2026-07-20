/**
 * Server-side external Gestalt client options and factory.
 *
 * @module client/client
 */

import { normalizeAddress } from "./address.ts";
import { authToProvider, type Auth } from "./auth.ts";
import type { AgentClientREST } from "./generated/agent_client.ts";
import { AgentClient } from "./generated/agent_client.ts";
import type { AppClientREST } from "./generated/app_client.ts";
import { AppClient } from "./generated/app_client.ts";
import type { AuthorizationClientREST } from "./generated/authorization_client.ts";
import { AuthorizationClient } from "./generated/authorization_client.ts";
import { ExternalCredentialsClient } from "./generated/externalCredentials_client.ts";
import type { IdentityClientREST } from "./generated/identity_client.ts";
import { IdentityClient } from "./generated/identity_client.ts";
import { IndexedDBClient } from "./generated/indexedDB_client.ts";
import type { WorkflowClientREST } from "./generated/workflow_client.ts";
import { WorkflowClient } from "./generated/workflow_client.ts";
import { createRestTransport } from "./rest_transport.ts";
import type { Transport } from "./generated/transport.ts";

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

export interface RestClientOptions extends ClientOptions {
  transport: RestTransport;
}

export interface GrpcClientOptions extends ClientOptions {
  transport: GrpcTransport;
}

export interface GestaltClient {
  readonly app: AppClient;
  close(): Promise<void>;
}

export interface RestGestaltClient {
  readonly app: AppClientREST;
  readonly agent: AgentClientREST;
  readonly workflow: WorkflowClientREST;
  readonly identity: IdentityClientREST;
  readonly authorization: AuthorizationClientREST;
  close(): Promise<void>;
}

export interface GrpcGestaltClient {
  readonly app: AppClient;
  readonly agent: AgentClient;
  readonly workflow: WorkflowClient;
  readonly identity: IdentityClient;
  readonly authorization: AuthorizationClient;
  readonly indexedDB: IndexedDBClient;
  readonly externalCredentials: ExternalCredentialsClient;
  close(): Promise<void>;
}

export function rest(): RestTransport {
  return { kind: "rest" };
}

export function grpc(): GrpcTransport {
  return { kind: "grpc" };
}

interface CoreGestaltClients {
  readonly app: AppClient;
  readonly agent: AgentClient;
  readonly workflow: WorkflowClient;
  readonly identity: IdentityClient;
  readonly authorization: AuthorizationClient;
}

function bindCoreClients(transport: Transport): CoreGestaltClients {
  return {
    app: new AppClient(transport),
    agent: new AgentClient(transport),
    workflow: new WorkflowClient(transport),
    identity: new IdentityClient(transport),
    authorization: new AuthorizationClient(transport),
  };
}

function asRestGestaltClient(
  clients: CoreGestaltClients,
): Omit<RestGestaltClient, "close"> {
  return clients;
}

function bindGrpcClients(transport: Transport): Omit<GrpcGestaltClient, "close"> {
  return {
    ...bindCoreClients(transport),
    indexedDB: new IndexedDBClient(transport),
    externalCredentials: new ExternalCredentialsClient(transport),
  };
}

export function createGestaltClient(
  options: RestClientOptions,
): Promise<RestGestaltClient>;
export function createGestaltClient(
  options: GrpcClientOptions,
): Promise<GrpcGestaltClient>;
export function createGestaltClient(
  options: ClientOptions,
): Promise<GestaltClient>;
export async function createGestaltClient(
  options: ClientOptions,
): Promise<GestaltClient | RestGestaltClient | GrpcGestaltClient> {
  const baseUrl = normalizeAddress(options.address);
  const auth = authToProvider(options.auth);
  let transport: Transport;
  let close: () => Promise<void> = async () => {};

  if (options.transport.kind === "rest") {
    const restOptions = options as RestClientOptions;
    transport = createRestTransport({
      baseUrl,
      auth,
      ...(restOptions.fetch !== undefined ? { fetch: restOptions.fetch } : {}),
    });
    return {
      ...asRestGestaltClient(bindCoreClients(transport)),
      close,
    };
  }

  if (options.transport.kind === "grpc") {
    const { createGrpcTransport } = await import("./grpc_transport.ts");
    const grpcTransport = await createGrpcTransport({
      baseUrl,
      auth,
    });
    transport = grpcTransport;
    close = () => grpcTransport.close();
    return {
      ...bindGrpcClients(transport),
      close,
    };
  }

  const unknownTransport: never = options.transport;
  throw new Error(`unsupported transport: ${JSON.stringify(unknownTransport)}`);
}
