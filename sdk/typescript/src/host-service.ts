import { connect } from "node:net";

import type { Interceptor, Transport } from "@connectrpc/connect";
import { createGrpcTransport, Http2SessionManager } from "@connectrpc/connect-node";

export const ENV_HOST_SERVICE_SOCKET = "GESTALT_HOST_SERVICE_SOCKET";
export const ENV_HOST_SERVICE_TOKEN = "GESTALT_HOST_SERVICE_TOKEN";
export const HOST_SERVICE_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";
export const HOST_SERVICE_BINDING_HEADER = "x-gestalt-host-binding";
export const INVOCATION_TOKEN_HEADER = "x-gestalt-invocation-token";

export type HostServiceTransportOptions = {
  baseUrl: string;
  nodeOptions?: { path: string };
};

export type HostServiceGrpcTransport = Transport & {
  close(): void;
};

export function parseHostServiceTarget(
  serviceName: string,
  rawTarget: string,
): HostServiceTransportOptions {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error(`${serviceName}: transport target is required`);
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `${serviceName}: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `${serviceName}: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `${serviceName}: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `${serviceName}: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

export function hostServiceMetadataInterceptors(
  token: string,
  binding: string,
  invocationToken = "",
): Interceptor[] {
  const normalizedToken = token.trim();
  const normalizedBinding = binding.trim();
  const normalizedInvocationToken = invocationToken.trim();
  if (!normalizedToken && !normalizedBinding && !normalizedInvocationToken) {
    return [];
  }
  return [
    (next) => async (req) => {
      if (normalizedToken) {
        req.header.set(HOST_SERVICE_RELAY_TOKEN_HEADER, normalizedToken);
      }
      if (normalizedBinding) {
        req.header.set(HOST_SERVICE_BINDING_HEADER, normalizedBinding);
      }
      if (normalizedInvocationToken) {
        req.header.set(INVOCATION_TOKEN_HEADER, normalizedInvocationToken);
      }
      return await next(req);
    },
  ];
}

export function createHostServiceGrpcTransport(
  transportOptions: HostServiceTransportOptions,
  interceptors: Interceptor[] = [],
): HostServiceGrpcTransport {
  const nodeOptions = transportOptions.nodeOptions
    ? {
        createConnection: () =>
          connect({ path: transportOptions.nodeOptions!.path }),
      }
    : undefined;
  const sessionManager = new Http2SessionManager(
    transportOptions.baseUrl,
    undefined,
    nodeOptions,
  );
  const transport = createGrpcTransport({
    baseUrl: transportOptions.baseUrl,
    sessionManager,
    interceptors,
  });
  return Object.assign(transport, {
    close: () => sessionManager.abort(),
  });
}

export function requireHostServiceTarget(
  serviceName: string,
): { target: string; token: string } {
  const target = process.env[ENV_HOST_SERVICE_SOCKET];
  if (!target) {
    throw new Error(`${serviceName}: ${ENV_HOST_SERVICE_SOCKET} is not set`);
  }
  return {
    target,
    token: process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "",
  };
}
