/**
 * Browser REST client, session auth, and mount helpers for Gestalt web apps.
 *
 * @example
 * ```ts
 * import {
 *   createGestaltClient,
 *   session,
 *   bearer,
 *   unauthenticated,
 * } from "@valon-technologies/gestalt-web";
 * ```
 *
 * @packageDocumentation
 */

export {
  bearer,
  bindApp,
  createGestaltClient,
  session,
  unauthenticated,
  type Auth,
  type BearerAuth,
  type BoundAppClient,
  type BoundAppInvokeRequest,
  type ClientOptions,
  type GestaltClient,
  type JsonObject,
  type SessionAuth,
  type Unauthenticated,
} from "./client.ts";

export {
  GestaltError,
  GestaltErrorCode,
  httpStatusToGestaltCode,
  toGestaltError,
} from "./client/runtime/rpc_support.ts";
export { InvokeError } from "./client/runtime/invoke_support.ts";

export { AgentClient } from "./client/generated/agent_client.ts";
export { AppClient } from "./client/generated/app_client.ts";
export { AuthorizationClient } from "./client/generated/authorization_client.ts";
export { IdentityClient } from "./client/generated/identity_client.ts";
export { WorkflowClient } from "./client/generated/workflow_client.ts";
export { PUBLIC_METHODS } from "./client/generated/methods.ts";
export type { PublicMethod, PublicMethodHttp } from "./client/generated/methods.ts";
export type * from "./client/generated/types.ts";
export type {
  PublicUnaryCallOptions,
  UnaryTransport,
} from "./client/generated/unary_transport.ts";
