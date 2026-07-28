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
  type GrpcClientOptions,
  type GrpcGestaltClient,
  type GrpcTransport,
  type RestClientOptions,
  type RestGestaltClient,
  type RestTransport,
  type Unauthenticated,
} from "./client.ts";

export { AgentClient } from "./generated/agent_client.ts";
export { AppClient } from "./generated/app_client.ts";
export { AuthorizationClient } from "./generated/authorization_client.ts";
export { ExternalCredentialsClient } from "./generated/externalCredentials_client.ts";
export { IdentityClient } from "./generated/identity_client.ts";
export { IndexedDBClient } from "./generated/indexedDB_client.ts";
export { WorkflowClient } from "./generated/workflow_client.ts";
export { PUBLIC_METHODS } from "./generated/methods.ts";
export type { PublicMethod, PublicMethodHttp } from "./generated/methods.ts";
export type * from "./generated/types.ts";
export type { Transport, PublicUnaryCallOptions } from "./generated/transport.ts";

export {
  Agent,
  AgentClientError,
  type Agent as AgentHandle,
  type AgentClientOptions,
  type AgentConfig,
  type AgentConfigRevision,
  type AgentConfigUpdate,
  type AgentCreateInput,
  type AgentFetch,
  type AgentInit,
  type AgentInteraction,
  type AgentInteractionResolution,
  type AgentResult,
  type AgentRun,
  type AgentRunEventType,
  type AgentRunStatus,
  type AgentSendMessageOptions,
  type AgentSkillConfig,
  type AgentSkillRef,
  type AgentToolConfig,
  type AgentToolRef,
  type AgentTurn,
  type AgentTurnEvent,
  type AgentWorkspace,
  type AgentWorkspaceGitCheckout,
} from "../agent-client.ts";
