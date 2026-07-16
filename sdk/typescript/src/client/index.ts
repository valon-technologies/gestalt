/**
 * Public Gestalt transport client (shared types and auth).
 *
 * Import `@valon-technologies/gestalt/client/rest` for browser-safe REST or
 * `@valon-technologies/gestalt/client/grpc` for Node.js gRPC.
 */

import { createGestaltClient as createRestGestaltClient } from "./client_rest.ts";
import type {
  GestaltClient,
  GestaltClientOptions,
  GestaltGrpcClientOptions,
  GestaltRestClientOptions,
} from "./types.ts";

export {
  bearer,
  bearerAuth,
  session,
  sessionAuth,
  unauthenticated,
  restCredentials,
  type AuthKind,
  type AuthProvider,
} from "./auth.ts";
export {
  GestaltError,
  GestaltErrorCode,
  parseGatewayError,
} from "./errors.ts";
export type { PublicTransport } from "./transport.ts";
export { PUBLIC_METHODS, type PublicMethod } from "./generated/methods.ts";
export type {
  GestaltClient,
  GestaltClientOptions,
  GestaltGrpcClient,
  GestaltGrpcClientOptions,
  GestaltRestClient,
  GestaltRestClientOptions,
  GestaltTransport,
  GrpcTransport,
  RestTransport,
} from "./types.ts";

export async function createGestaltClient(
  options: GestaltClientOptions,
): Promise<GestaltClient> {
  if (options.transport.kind === "grpc") {
    throw new Error(
      "gRPC transport is not available from @valon-technologies/gestalt/client; import @valon-technologies/gestalt/client/grpc instead",
    );
  }
  return createRestGestaltClient(options as GestaltRestClientOptions);
}
