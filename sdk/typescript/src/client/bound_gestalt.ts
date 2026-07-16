/**
 * Bound provider gRPC client factory (Node.js host-service relay).
 */

import type { RequestContext as NativeRequestContext } from "../app.ts";
import type { RequestContext as WireRequestContext } from "../internal/gen/v1/app_pb.ts";
import { toWireRequestContext } from "../internal/codec/app.ts";
import type { GestaltGrpcClient } from "./types.ts";
import { createBoundGrpcTransport } from "./bound_grpc_transport.ts";
import {
  PublicAgentClient,
  PublicAppClient,
  PublicAuthorizationClient,
  PublicExternalCredentialsClient,
  PublicIdentityClient,
  PublicIndexedDBClient,
  PublicWorkflowClient,
} from "./generated/grpc_clients.ts";

export async function createBoundGestaltClient(
  context?: NativeRequestContext | WireRequestContext,
  callerBearerToken?: string,
): Promise<GestaltGrpcClient> {
  const wireContext = toBoundWireContext(context);
  const transport = createBoundGrpcTransport({
    context: wireContext,
    callerBearerToken,
  });
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

function toBoundWireContext(
  context: NativeRequestContext | WireRequestContext | undefined,
): WireRequestContext | undefined {
  if (context === undefined) {
    return undefined;
  }
  if ("$typeName" in context) {
    return context;
  }
  return toWireRequestContext(context);
}
