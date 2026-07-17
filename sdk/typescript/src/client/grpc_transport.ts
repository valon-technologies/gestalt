/**
 * Connect gRPC transport for the public Gestalt API.
 *
 * Loaded lazily so REST-only callers do not initialize HTTP/2 dependencies.
 */

import type { DescMessage, DescService, Message } from "@bufbuild/protobuf";
import { createClient, type Client, type Transport } from "@connectrpc/connect";

import { Agent } from "../internal/gen/v1/agent_pb.ts";
import { App } from "../internal/gen/v1/app_pb.ts";
import { Authorization } from "../internal/gen/v1/authorization_pb.ts";
import { ExternalCredentials } from "../internal/gen/v1/external_credential_pb.ts";
import { Identity } from "../internal/gen/v1/identity_pb.ts";
import { IndexedDB } from "../internal/gen/v1/indexeddb_pb.ts";
import { Workflow } from "../internal/gen/v1/workflow_pb.ts";

import type { AuthProvider } from "./auth.ts";
import { toGestaltError } from "./errors.ts";
import type { PublicMethod } from "./generated/methods.ts";
import {
  isAbortLike,
  raceWithAbort,
  resolveEffectiveAbortSignal,
  throwIfAborted,
  toTransportGestaltError,
} from "./generated/transport_support.ts";
import type {
  PublicUnaryCallOptions,
  UnaryTransport,
} from "./generated/unary_transport.ts";

export interface GrpcUnaryTransportOptions {
  baseUrl: string;
  auth: AuthProvider;
}

const PUBLIC_SERVICES = {
  Agent,
  App,
  Authorization,
  ExternalCredentials,
  Identity,
  IndexedDB,
  Workflow,
} as const satisfies Record<string, DescService>;

type PublicServiceName = keyof typeof PUBLIC_SERVICES;
type PublicServiceClient<S extends DescService> = Client<S>;

type ConnectGrpcTransport = ReturnType<
  typeof import("@connectrpc/connect-node").createGrpcTransport
>;

export async function createGrpcUnaryTransport(
  options: GrpcUnaryTransportOptions,
): Promise<UnaryTransport & { close(): Promise<void> }> {
  const { createGrpcTransport, Http2SessionManager } = await import(
    "@connectrpc/connect-node"
  );
  const sessionManager = new Http2SessionManager(options.baseUrl);
  let transport: ConnectGrpcTransport | undefined;
  const clients = new Map<PublicServiceName, PublicServiceClient<DescService>>();

  const getClient = <S extends DescService>(
    serviceName: PublicServiceName,
    service: S,
  ): PublicServiceClient<S> => {
    const existing = clients.get(serviceName);
    if (existing) {
      return existing as PublicServiceClient<S>;
    }
    if (!transport) {
      transport = createGrpcTransport({
        baseUrl: options.baseUrl,
        sessionManager,
      });
    }
    const client = createClient(service, transport);
    clients.set(serviceName, client as PublicServiceClient<DescService>);
    return client;
  };

  return {
    async unary<Output extends Message>(
      method: PublicMethod,
      request: Message,
      _requestSchema: DescMessage,
      _responseSchema: DescMessage,
      callOptions?: PublicUnaryCallOptions,
    ): Promise<Output> {
      const signal = resolveEffectiveAbortSignal(callOptions);
      let authorization: string | undefined;
      try {
        throwIfAborted(signal);
        authorization = await raceWithAbort(
          options.auth.getAuthorization(),
          signal,
          { removeListener: callOptions?.timeoutMs === undefined },
        );
      } catch (error) {
        throw toTransportGestaltError(callOptions, error, signal);
      }

      const requestOptions = {
        ...(signal !== undefined ? { signal } : {}),
        ...(authorization
          ? { headers: { Authorization: authorization } }
          : {}),
      };

      const serviceName = method.service as PublicServiceName;
      const service = PUBLIC_SERVICES[serviceName];
      if (!service) {
        throw new Error(`unknown public gRPC service ${method.service}`);
      }

      const client = getClient(serviceName, service);
      const rpcName = lowerFirst(method.method);
      const rpc = (client as Record<string, unknown>)[rpcName];
      if (typeof rpc !== "function") {
        throw new Error(`unknown public gRPC method ${method.grpcPath}`);
      }

      try {
        return (await (rpc as (req: Message, opts: object) => Promise<Output>).call(
          client,
          request,
          requestOptions,
        )) as Output;
      } catch (error) {
        if (isAbortLike(error, signal)) {
          throw toTransportGestaltError(callOptions, error, signal);
        }
        throw toGestaltError(error);
      }
    },

    async close(): Promise<void> {
      sessionManager.abort();
      transport = undefined;
      clients.clear();
    },
  };
}

function lowerFirst(value: string): string {
  if (value === "") {
    return value;
  }
  return value[0]!.toLowerCase() + value.slice(1);
}
