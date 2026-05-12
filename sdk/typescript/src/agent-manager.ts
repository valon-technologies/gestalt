import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AgentManagerHost as AgentManagerHostService,
} from "./internal/gen/v1/agent_pb.ts";
import type { Request } from "./api.ts";
import {
  AgentExecutionStatus,
  AgentSessionState,
  AgentToolSourceMode,
  agentInteractionFromProto,
  agentMessageToProto,
  agentSessionFromProto,
  agentToolRefToProto,
  agentTurnEventFromProto,
  agentTurnFromProto,
  type AgentInteraction,
  type AgentMessage,
  type AgentSession,
  type AgentToolRef,
  type AgentTurn,
  type AgentTurnEvent,
} from "./agent.ts";
import { structFromObject, type JsonObjectInput } from "./protocol.ts";

/** Environment variable containing the agent-manager host-service target. */
export const ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET";
/** Environment variable containing the optional agent-manager relay token. */
export const ENV_AGENT_MANAGER_SOCKET_TOKEN =
  `${ENV_AGENT_MANAGER_SOCKET}_TOKEN`;
const AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export interface AgentWorkspaceGitCheckoutInput {
  url?: string | undefined;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface AgentWorkspaceInput {
  checkouts?: readonly AgentWorkspaceGitCheckoutInput[] | undefined;
  cwd?: string | undefined;
}

/** Shape accepted when creating an agent session through the host manager. */
export interface AgentManagerCreateSessionInput {
  providerName?: string | undefined;
  model?: string | undefined;
  clientRef?: string | undefined;
  metadata?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  workspace?: AgentWorkspaceInput | undefined;
}

/** Shape accepted when fetching an agent session through the host manager. */
export interface AgentManagerGetSessionInput {
  sessionId: string;
}

/** Shape accepted when listing agent sessions through the host manager. */
export interface AgentManagerListSessionsInput {
  providerName?: string | undefined;
  state?: AgentSessionState | undefined;
  limit?: number | undefined;
  summaryOnly?: boolean | undefined;
}

/** Shape accepted when updating an agent session through the host manager. */
export interface AgentManagerUpdateSessionInput {
  sessionId: string;
  clientRef?: string | undefined;
  state?: AgentSessionState | undefined;
  metadata?: JsonObjectInput | undefined;
}

/** Shape accepted when creating an agent turn through the host manager. */
export interface AgentManagerCreateTurnInput {
  sessionId: string;
  model?: string | undefined;
  messages?: readonly AgentMessage[] | undefined;
  toolRefs?: readonly AgentToolRef[] | undefined;
  toolSource?: AgentToolSourceMode | undefined;
  responseSchema?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  modelOptions?: JsonObjectInput | undefined;
}

/** Shape accepted when fetching an agent turn through the host manager. */
export interface AgentManagerGetTurnInput {
  turnId: string;
}

/** Shape accepted when listing agent turns through the host manager. */
export interface AgentManagerListTurnsInput {
  sessionId: string;
  status?: AgentExecutionStatus | undefined;
  limit?: number | undefined;
  summaryOnly?: boolean | undefined;
}

/** Shape accepted when cancelling an agent turn through the host manager. */
export interface AgentManagerCancelTurnInput {
  turnId: string;
  reason?: string | undefined;
}

/** Shape accepted when listing events for an agent turn. */
export interface AgentManagerListTurnEventsInput {
  turnId: string;
  afterSeq?: bigint | number | undefined;
  limit?: number | undefined;
}

/** Shape accepted when listing agent interactions. */
export interface AgentManagerListInteractionsInput {
  turnId: string;
}

/** Shape accepted when resolving an agent interaction. */
export interface AgentManagerResolveInteractionInput {
  turnId: string;
  interactionId: string;
  resolution?: JsonObjectInput | undefined;
}

export interface ListAgentManagerSessionsResponse {
  sessions: readonly AgentSession[];
}

export interface ListAgentManagerTurnsResponse {
  turns: readonly AgentTurn[];
}

export interface ListAgentManagerTurnEventsResponse {
  events: readonly AgentTurnEvent[];
}

export interface ListAgentManagerInteractionsResponse {
  interactions: readonly AgentInteraction[];
}

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
    return agentSessionFromProto(await this.client.createSession({
      ...agentManagerCreateSessionRequest(request),
      invocationToken: this.invocationToken,
    }));
  }

  /** Fetches one agent session. */
  async getSession(request: AgentManagerGetSessionInput): Promise<AgentSession> {
    return agentSessionFromProto(await this.client.getSession({
      sessionId: request.sessionId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Lists agent sessions visible to the invocation token. */
  async listSessions(
    request: AgentManagerListSessionsInput = {},
  ): Promise<ListAgentManagerSessionsResponse> {
    const response = await this.client.listSessions({
      providerName: request.providerName ?? "",
      state: request.state ?? AgentSessionState.UNSPECIFIED,
      limit: request.limit ?? 0,
      summaryOnly: request.summaryOnly ?? false,
      invocationToken: this.invocationToken,
    });
    return { sessions: response.sessions.map(agentSessionFromProto) };
  }

  /** Updates mutable fields on an agent session. */
  async updateSession(
    request: AgentManagerUpdateSessionInput,
  ): Promise<AgentSession> {
    return agentSessionFromProto(await this.client.updateSession({
      sessionId: request.sessionId,
      clientRef: request.clientRef ?? "",
      state: request.state ?? AgentSessionState.UNSPECIFIED,
      metadata: optionalStruct(request.metadata),
      invocationToken: this.invocationToken,
    }));
  }

  /** Creates an agent turn. */
  async createTurn(request: AgentManagerCreateTurnInput): Promise<AgentTurn> {
    return agentTurnFromProto(await this.client.createTurn({
      ...agentManagerCreateTurnRequest(request),
      invocationToken: this.invocationToken,
    }));
  }

  /** Fetches one agent turn. */
  async getTurn(request: AgentManagerGetTurnInput): Promise<AgentTurn> {
    return agentTurnFromProto(await this.client.getTurn({
      turnId: request.turnId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Lists turns for an agent session. */
  async listTurns(
    request: AgentManagerListTurnsInput,
  ): Promise<ListAgentManagerTurnsResponse> {
    const response = await this.client.listTurns({
      sessionId: request.sessionId,
      status: request.status ?? AgentExecutionStatus.UNSPECIFIED,
      limit: request.limit ?? 0,
      summaryOnly: request.summaryOnly ?? false,
      invocationToken: this.invocationToken,
    });
    return { turns: response.turns.map(agentTurnFromProto) };
  }

  /** Cancels an in-progress agent turn. */
  async cancelTurn(request: AgentManagerCancelTurnInput): Promise<AgentTurn> {
    return agentTurnFromProto(await this.client.cancelTurn({
      turnId: request.turnId,
      reason: request.reason ?? "",
      invocationToken: this.invocationToken,
    }));
  }

  /** Lists events emitted for an agent turn. */
  async listTurnEvents(
    request: AgentManagerListTurnEventsInput,
  ): Promise<ListAgentManagerTurnEventsResponse> {
    const response = await this.client.listTurnEvents({
      turnId: request.turnId,
      afterSeq: request.afterSeq === undefined ? 0n : BigInt(request.afterSeq),
      limit: request.limit ?? 0,
      invocationToken: this.invocationToken,
    });
    return { events: response.events.map(agentTurnEventFromProto) };
  }

  /** Lists pending or completed agent interactions. */
  async listInteractions(
    request: AgentManagerListInteractionsInput,
  ): Promise<ListAgentManagerInteractionsResponse> {
    const response = await this.client.listInteractions({
      turnId: request.turnId,
      invocationToken: this.invocationToken,
    });
    return { interactions: response.interactions.map(agentInteractionFromProto) };
  }

  /** Resolves an agent interaction with a host response. */
  async resolveInteraction(
    request: AgentManagerResolveInteractionInput,
  ): Promise<AgentInteraction> {
    return agentInteractionFromProto(await this.client.resolveInteraction({
      turnId: request.turnId,
      interactionId: request.interactionId,
      resolution: optionalStruct(request.resolution),
      invocationToken: this.invocationToken,
    }));
  }
}

function agentManagerCreateSessionRequest(request: AgentManagerCreateSessionInput) {
  return {
    providerName: request.providerName ?? "",
    model: request.model ?? "",
    clientRef: request.clientRef ?? "",
    metadata: optionalStruct(request.metadata),
    idempotencyKey: request.idempotencyKey ?? "",
    workspace: request.workspace === undefined
      ? undefined
      : {
        checkouts: (request.workspace.checkouts ?? []).map((checkout) => ({
          url: checkout.url ?? "",
          ref: checkout.ref ?? "",
          path: checkout.path ?? "",
        })),
        cwd: request.workspace.cwd ?? "",
      },
  };
}

function agentManagerCreateTurnRequest(request: AgentManagerCreateTurnInput) {
  return {
    sessionId: request.sessionId,
    model: request.model ?? "",
    messages: (request.messages ?? []).map(agentMessageToProto),
    toolRefs: (request.toolRefs ?? []).map((ref) => agentToolRefToProto(ref)!),
    toolSource: request.toolSource ?? AgentToolSourceMode.UNSPECIFIED,
    responseSchema: optionalStruct(request.responseSchema),
    metadata: optionalStruct(request.metadata),
    idempotencyKey: request.idempotencyKey ?? "",
    modelOptions: optionalStruct(request.modelOptions),
  };
}

function optionalStruct(value?: JsonObjectInput | undefined) {
  return value === undefined ? undefined : structFromObject(value);
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
