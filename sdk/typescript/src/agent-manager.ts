import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AgentManagerCancelTurnRequestSchema,
  AgentManagerCreateSessionRequestSchema,
  AgentManagerCreateTurnRequestSchema,
  AgentManagerGetSessionRequestSchema,
  AgentManagerGetTurnRequestSchema,
  AgentManagerHost as AgentManagerHostService,
  AgentManagerListInteractionsRequestSchema,
  AgentManagerListSessionsRequestSchema,
  AgentManagerListTurnEventsRequestSchema,
  AgentManagerListTurnsRequestSchema,
  AgentManagerResolveInteractionRequestSchema,
  AgentManagerUpdateSessionRequestSchema,
  type AgentInteraction,
  type AgentSession,
  type AgentTurn,
  type AgentTurnEvent,
} from "./internal/gen/v1/agent_pb.ts";
import type { Request } from "./api.ts";

/** Environment variable containing the agent-manager host-service target. */
export const ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET";
/** Environment variable containing the optional agent-manager relay token. */
export const ENV_AGENT_MANAGER_SOCKET_TOKEN =
  `${ENV_AGENT_MANAGER_SOCKET}_TOKEN`;
const AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

/** Shape accepted when creating an agent session through the host manager. */
export type AgentManagerCreateSessionInput = MessageInitShape<
  typeof AgentManagerCreateSessionRequestSchema
>;
/** Shape accepted when fetching an agent session through the host manager. */
export type AgentManagerGetSessionInput = MessageInitShape<
  typeof AgentManagerGetSessionRequestSchema
>;
/** Shape accepted when listing agent sessions through the host manager. */
export type AgentManagerListSessionsInput = MessageInitShape<
  typeof AgentManagerListSessionsRequestSchema
>;
/** Shape accepted when updating an agent session through the host manager. */
export type AgentManagerUpdateSessionInput = MessageInitShape<
  typeof AgentManagerUpdateSessionRequestSchema
>;
/** Shape accepted when creating an agent turn through the host manager. */
export type AgentManagerCreateTurnInput = MessageInitShape<
  typeof AgentManagerCreateTurnRequestSchema
>;
/** Shape accepted when fetching an agent turn through the host manager. */
export type AgentManagerGetTurnInput = MessageInitShape<
  typeof AgentManagerGetTurnRequestSchema
>;
/** Shape accepted when listing agent turns through the host manager. */
export type AgentManagerListTurnsInput = MessageInitShape<
  typeof AgentManagerListTurnsRequestSchema
>;
/** Shape accepted when cancelling an agent turn through the host manager. */
export type AgentManagerCancelTurnInput = MessageInitShape<
  typeof AgentManagerCancelTurnRequestSchema
>;
/** Shape accepted when listing events for an agent turn. */
export type AgentManagerListTurnEventsInput = MessageInitShape<
  typeof AgentManagerListTurnEventsRequestSchema
>;
/** Shape accepted when listing agent interactions. */
export type AgentManagerListInteractionsInput = MessageInitShape<
  typeof AgentManagerListInteractionsRequestSchema
>;
/** Shape accepted when resolving an agent interaction. */
export type AgentManagerResolveInteractionInput = MessageInitShape<
  typeof AgentManagerResolveInteractionRequestSchema
>;

/**
 * Client for managing agent sessions, turns, events, and interactions.
 *
 * The constructor accepts either a Gestalt request or an invocation token. Each
 * manager call forwards that token to the host service.
 */
export class AgentManager {
  private readonly client: Client<typeof AgentManagerHostService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const target = process.env[ENV_AGENT_MANAGER_SOCKET];
    if (!target) {
      throw new Error(`agent manager: ${ENV_AGENT_MANAGER_SOCKET} is not set`);
    }
    const relayToken =
      process.env[ENV_AGENT_MANAGER_SOCKET_TOKEN]?.trim() ?? "";

    const transport = createGrpcTransport({
      ...agentManagerTransportOptions(target),
      interceptors: relayToken
        ? [agentManagerRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(AgentManagerHostService, transport);
  }

  /** Creates an agent session. */
  async createSession(
    request: AgentManagerCreateSessionInput,
  ): Promise<AgentSession> {
    return await this.client.createSession({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Fetches one agent session. */
  async getSession(request: AgentManagerGetSessionInput): Promise<AgentSession> {
    return await this.client.getSession({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Lists agent sessions visible to the invocation token. */
  async listSessions(
    request: AgentManagerListSessionsInput = {},
  ): Promise<AgentSession[]> {
    const response = await this.client.listSessions({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.sessions];
  }

  /** Updates mutable fields on an agent session. */
  async updateSession(
    request: AgentManagerUpdateSessionInput,
  ): Promise<AgentSession> {
    return await this.client.updateSession({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Creates an agent turn. */
  async createTurn(request: AgentManagerCreateTurnInput): Promise<AgentTurn> {
    return await this.client.createTurn({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Fetches one agent turn. */
  async getTurn(request: AgentManagerGetTurnInput): Promise<AgentTurn> {
    return await this.client.getTurn({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Lists turns for an agent session. */
  async listTurns(request: AgentManagerListTurnsInput): Promise<AgentTurn[]> {
    const response = await this.client.listTurns({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.turns];
  }

  /** Cancels an in-progress agent turn. */
  async cancelTurn(request: AgentManagerCancelTurnInput): Promise<AgentTurn> {
    return await this.client.cancelTurn({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Lists events emitted for an agent turn. */
  async listTurnEvents(
    request: AgentManagerListTurnEventsInput,
  ): Promise<AgentTurnEvent[]> {
    const response = await this.client.listTurnEvents({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.events];
  }

  /** Lists pending or completed agent interactions. */
  async listInteractions(
    request: AgentManagerListInteractionsInput,
  ): Promise<AgentInteraction[]> {
    const response = await this.client.listInteractions({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.interactions];
  }

  /** Resolves an agent interaction with a host response. */
  async resolveInteraction(
    request: AgentManagerResolveInteractionInput,
  ): Promise<AgentInteraction> {
    return await this.client.resolveInteraction({
      ...request,
      invocationToken: this.invocationToken,
    });
  }
}

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("agent manager: invocation token is not available");
  }
  return trimmed;
}

function agentManagerTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("agent manager: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `agent manager: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `agent manager: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `agent manager: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `agent manager: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function agentManagerRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(AGENT_MANAGER_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}
