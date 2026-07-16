/**
 * gRPC public Gestalt transport client (Node.js).
 */

export {
  bearer,
  bearerAuth,
  unauthenticated,
  type AuthKind,
  type AuthProvider,
} from "./auth.ts";
export { createGestaltClient } from "./client_grpc.ts";
export type {
  GestaltGrpcClient,
  GestaltGrpcClientOptions,
  GrpcTransport,
} from "./types.ts";
export { GestaltError, GestaltErrorCode } from "./errors.ts";
export type { PublicTransport } from "./transport.ts";
export { createGrpcTransport, type GrpcTransportOptions } from "./grpc_transport.ts";
export * from "./generated/grpc_clients.ts";
export { PUBLIC_METHODS, type PublicMethod } from "./generated/methods.ts";

/** Selects the gRPC public transport. */
export function grpc(): import("./types.ts").GrpcTransport {
  return { kind: "grpc" };
}
