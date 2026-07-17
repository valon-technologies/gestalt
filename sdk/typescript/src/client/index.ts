/**
 * External server-side Gestalt clients (REST and gRPC).
 *
 * @example
 * ```ts
 * import {
 *   createGestaltClient,
 *   rest,
 *   grpc,
 *   bearer,
 *   unauthenticated,
 * } from "@valon-technologies/gestalt/client";
 * ```
 *
 * @packageDocumentation
 */

export {
  bearer,
  createGestaltClient,
  grpc,
  rest,
  unauthenticated,
  type Auth,
  type BearerAuth,
  type ClientOptions,
  type GestaltClient,
  type GrpcTransport,
  type RestTransport,
  type Unauthenticated,
} from "./client.ts";

export { AppClient } from "./generated/app_client.ts";
export { PUBLIC_METHODS } from "./generated/methods.ts";
export type { PublicMethod, PublicMethodHttp } from "./generated/methods.ts";
export type {
  PublicAppInvokeGraphQLRequest,
  PublicAppInvokeRequest,
} from "./generated/types.ts";
export type { UnaryTransport, PublicUnaryCallOptions } from "./generated/unary_transport.ts";
