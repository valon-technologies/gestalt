/**
 * Connect gRPC transport for the public Gestalt API.
 *
 * Loaded lazily so REST-only callers do not initialize HTTP/2 dependencies.
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";

import type { AuthProvider } from "./auth.ts";
import { toGestaltError } from "./errors.ts";
import {
  createPublicGrpcClients,
  dispatchGrpcUnary,
} from "./generated/grpc_dispatch.ts";
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
  const transport: ConnectGrpcTransport = createGrpcTransport({
    baseUrl: options.baseUrl,
    sessionManager,
  });
  const appClient = createPublicGrpcClients(transport);

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
        return await dispatchGrpcUnary<Output>(
          appClient,
          method,
          request,
          requestOptions,
        );
      } catch (error) {
        if (isAbortLike(error, signal)) {
          throw toTransportGestaltError(callOptions, error, signal);
        }
        throw toGestaltError(error);
      }
    },

    async close(): Promise<void> {
      sessionManager.abort();
    },
  };
}
