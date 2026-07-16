/**
 * REST-only public Gestalt transport client.
 */

export {
  bearer,
  bearerAuth,
  session,
  sessionAuth,
  unauthenticated,
  type AuthKind,
  type AuthProvider,
} from "./auth.ts";
export { createGestaltClient } from "./client_rest.ts";
export type {
  GestaltRestClient,
  GestaltRestClientOptions,
  RestTransport,
} from "./types.ts";
export { GestaltError, GestaltErrorCode, parseGatewayError } from "./errors.ts";
export type { PublicTransport } from "./transport.ts";
export { createRestTransport, type RestTransportOptions } from "./rest_transport.ts";
export * from "./generated/rest_clients.ts";
export { PUBLIC_METHODS, type PublicMethod } from "./generated/methods.ts";

/** Selects the REST public transport. */
export function rest(): import("./types.ts").RestTransport {
  return { kind: "rest" };
}
