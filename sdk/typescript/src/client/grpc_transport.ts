/**
 * Connect gRPC transport for the public Gestalt API.
 *
 * Loaded lazily so REST-only callers do not initialize HTTP/2 dependencies.
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";
import { createClient, type Client } from "@connectrpc/connect";

import { App } from "../internal/gen/v1/app_pb.ts";

import type { AuthProvider } from "./auth.ts";
import { toGestaltError } from "./errors.ts";
import { PUBLIC_METHODS, type PublicMethod } from "./generated/methods.ts";
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

type AppServiceClient = Client<typeof App>;

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
  let appClient: AppServiceClient | undefined;

  const getAppClient = (): AppServiceClient => {
    if (!appClient) {
      if (!transport) {
        transport = createGrpcTransport({
          baseUrl: options.baseUrl,
          sessionManager,
        });
      }
      appClient = createClient(App, transport);
    }
    return appClient;
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

      try {
        const client = getAppClient();
        if (method.grpcPath === PUBLIC_METHODS.app.invoke.grpcPath) {
          return (await client.invoke(
            request as Parameters<AppServiceClient["invoke"]>[0],
            requestOptions,
          )) as unknown as Output;
        }
        if (method.grpcPath === PUBLIC_METHODS.app.invokeGraphQL.grpcPath) {
          return (await client.invokeGraphQL(
            request as Parameters<AppServiceClient["invokeGraphQL"]>[0],
            requestOptions,
          )) as unknown as Output;
        }
        throw new Error(`unknown public gRPC method ${method.grpcPath}`);
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
      appClient = undefined;
    },
  };
}
