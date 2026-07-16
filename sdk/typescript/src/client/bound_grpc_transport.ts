/**
 * Host-service gRPC transport for bound provider public clients.
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";
import { clone } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";

import { Agent } from "../internal/gen/v1/agent_pb.ts";
import { App } from "../internal/gen/v1/app_pb.ts";
import { Authorization } from "../internal/gen/v1/authorization_pb.ts";
import { ExternalCredentials } from "../internal/gen/v1/external_credential_pb.ts";
import { Identity } from "../internal/gen/v1/identity_pb.ts";
import { IndexedDB } from "../internal/gen/v1/indexeddb_pb.ts";
import { Workflow } from "../internal/gen/v1/workflow_pb.ts";
import type { RequestContext } from "../internal/gen/v1/app_pb.ts";

type MessageWithOptionalContext = Message & { context?: RequestContext };
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
} from "../host-service.ts";
import { CALLER_BEARER_TOKEN_METADATA_KEY } from "../providers/identity.ts";
import { toGestaltError } from "./errors.ts";
import type { PublicMethod } from "./generated/methods.ts";
import type { PublicTransport } from "./transport.ts";

const PUBLIC_SERVICES = {
  "gestalt.provider.v1.Agent": Agent,
  "gestalt.provider.v1.App": App,
  "gestalt.provider.v1.Authorization": Authorization,
  "gestalt.provider.v1.ExternalCredentials": ExternalCredentials,
  "gestalt.provider.v1.Identity": Identity,
  "gestalt.provider.v1.IndexedDB": IndexedDB,
  "gestalt.provider.v1.Workflow": Workflow,
} as const;

export interface BoundGrpcTransportOptions {
  context?: RequestContext | undefined;
  callerBearerToken?: string | undefined;
}

export function createBoundGrpcTransport(
  options: BoundGrpcTransportOptions = {},
): PublicTransport {
  const { context, callerBearerToken } = options;
  const { target, token } = requireHostServiceTarget("app");
  const transport = createHostServiceGrpcTransport(
    parseHostServiceTarget("app", target),
    [
      ...hostServiceMetadataInterceptors(token, ""),
      ...callerBearerTokenMetadataInterceptors(callerBearerToken ?? ""),
    ],
  );
  const serviceCache = new Map<string, ReturnType<typeof createClient>>();

  return {
    async unary<Res extends Message>(
      method: PublicMethod,
      request: Message,
      requestSchema: DescMessage,
      _responseSchema: DescMessage,
    ): Promise<Res> {
      const serviceTypeName = method.grpcPath.split("/")[1] ?? "";
      const service =
        PUBLIC_SERVICES[serviceTypeName as keyof typeof PUBLIC_SERVICES];
      if (!service) {
        throw new Error(`unknown public gRPC service ${serviceTypeName}`);
      }
      let client = serviceCache.get(serviceTypeName);
      if (!client) {
        client = createClient(service, transport);
        serviceCache.set(serviceTypeName, client);
      }
      const rpcName = lowerFirst(method.method);
      const call = (client as Record<string, (req: Message) => Promise<Message>>)[
        rpcName
      ];
      if (!call) {
        throw new Error(`gRPC method ${method.grpcPath} is not available`);
      }
      let wireRequest = request;
      if (context !== undefined) {
        const cloned = clone(requestSchema, request) as MessageWithOptionalContext;
        if (!cloned.context) {
          cloned.context = context;
        }
        wireRequest = cloned;
      }
      try {
        const response = await call(wireRequest);
        return response as Res;
      } catch (error) {
        throw toGestaltError(error);
      }
    },
  };
}

function lowerFirst(value: string): string {
  return value.length === 0 ? value : value[0]!.toLowerCase() + value.slice(1);
}

function callerBearerTokenMetadataInterceptors(
  token: string,
): ReturnType<typeof hostServiceMetadataInterceptors> {
  const normalizedToken = token.trim();
  if (!normalizedToken) {
    return [];
  }
  return [
    (next) => async (req) => {
      req.header.set(CALLER_BEARER_TOKEN_METADATA_KEY, normalizedToken);
      return await next(req);
    },
  ];
}
