/**
 * gRPC public Gestalt client factory (Node.js).
 */

import { normalizeClientAddress } from "./address.ts";
import type { GestaltGrpcClient, GestaltGrpcClientOptions } from "./types.ts";
import { createGrpcTransport } from "./grpc_transport.ts";
import type { PublicTransport } from "./transport.ts";
import {
  PublicAgentClient,
  PublicAppClient,
  PublicAuthorizationClient,
  PublicExternalCredentialsClient,
  PublicIdentityClient,
  PublicIndexedDBClient,
  PublicWorkflowClient,
} from "./generated/grpc_clients.ts";

export async function createGestaltClient(
  options: GestaltGrpcClientOptions,
): Promise<GestaltGrpcClient> {
  const transport = createGrpcTransport({
    baseUrl: normalizeClientAddress(options.address),
    auth: options.auth,
  });
  return createGrpcClients(transport);
}

function createGrpcClients(transport: PublicTransport): GestaltGrpcClient {
  return {
    app: new PublicAppClient(transport),
    agent: new PublicAgentClient(transport),
    workflow: new PublicWorkflowClient(transport),
    identity: new PublicIdentityClient(transport),
    authorization: new PublicAuthorizationClient(transport),
    indexedDB: new PublicIndexedDBClient(transport),
    externalCredentials: new PublicExternalCredentialsClient(transport),
  };
}

export type { GestaltGrpcClient, GestaltGrpcClientOptions } from "./types.ts";
