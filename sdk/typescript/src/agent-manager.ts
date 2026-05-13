import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AgentManagerHost as AgentManagerHostService,
  type AgentInteraction as ProtoAgentInteraction,
  type AgentSession as ProtoAgentSession,
  type AgentTurn as ProtoAgentTurn,
  type AgentTurnEvent as ProtoAgentTurnEvent,
} from "./internal/gen/v1/agent_pb.ts";
import type { Request } from "./api.ts";
import {
  agentActorFromProto,
  agentMessageFromProto,
  agentMessageToProto,
  agentToolRefToProto,
  agentTurnDisplayFromProto,
} from "./agent-conversions.ts";
import {
  AgentExecutionStatus,
  AgentInteractionState,
  AgentInteractionType,
  AgentSessionState,
  AgentToolSourceMode,
  type AgentInteraction,
  type AgentMessage,
  type AgentSession,
  type AgentToolRef,
  type AgentTurn,
  type AgentTurnEvent,
} from "./agent.ts";
import {
  dateFromTimestamp,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";

/** Environment variable containing the agent-manager host-service target. */
export const ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET";
/** Environment variable containing the optional agent-manager relay token. */
export const ENV_AGENT_MANAGER_SOCKET_TOKEN =
  `${ENV_AGENT_MANAGER_SOCKET}_TOKEN`;
const AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export interface AgentManagerWorkspaceGitCheckout {
  url?: string | undefined;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface AgentManagerWorkspace {
  checkouts?: readonly AgentManagerWorkspaceGitCheckout[] | undefined;
  cwd?: string | undefined;
}

/** Shape accepted when creating an agent session through the host manager. */
export interface AgentManagerCreateSession {
  providerName: string;
  model?: string | undefined;
  clientRef?: string | undefined;
  metadata?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  workspace?: AgentManagerWorkspace | undefined;
}

/** Shape accepted when fetching an agent session through the host manager. */
export interface AgentManagerGetSession {
  sessionId: string;
}

/** Shape accepted when listing agent sessions through the host manager. */
export interface AgentManagerListSessions {
  providerName?: string | undefined;
  state?: AgentSessionState | undefined;
  limit?: number | undefined;
  summaryOnly?: boolean | undefined;
}

/** Shape accepted when updating an agent session through the host manager. */
export interface AgentManagerUpdateSession {
  sessionId: string;
  clientRef?: string | undefined;
  state?: AgentSessionState | undefined;
  metadata?: JsonObjectInput | undefined;
}

/** Shape accepted when creating an agent turn through the host manager. */
export interface AgentManagerCreateTurn {
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
export interface AgentManagerGetTurn {
  turnId: string;
}

/** Shape accepted when listing agent turns through the host manager. */
export interface AgentManagerListTurns {
  sessionId: string;
  status?: AgentExecutionStatus | undefined;
  limit?: number | undefined;
  summaryOnly?: boolean | undefined;
}

/** Shape accepted when cancelling an agent turn through the host manager. */
export interface AgentManagerCancelTurn {
  turnId: string;
  reason?: string | undefined;
}

/** Shape accepted when listing events for an agent turn. */
export interface AgentManagerListTurnEvents {
  turnId: string;
  afterSeq?: bigint | number | undefined;
  limit?: number | undefined;
}

/** Shape accepted when listing agent interactions. */
export interface AgentManagerListInteractions {
  turnId: string;
}

/** Shape accepted when resolving an agent interaction. */
export interface AgentManagerResolveInteraction {
  turnId: string;
  interactionId: string;
  resolution?: JsonObjectInput | undefined;
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
    request: AgentManagerCreateSession,
  ): Promise<AgentSession> {
    return agentSessionFromProto(
      await this.client.createSession({
        providerName: request.providerName,
        model: request.model ?? "",
        clientRef: request.clientRef ?? "",
        metadata: optionalStruct(request.metadata),
        idempotencyKey: request.idempotencyKey ?? "",
        invocationToken: this.invocationToken,
        workspace: workspaceToProto(request.workspace),
      }),
    );
  }

  /** Fetches one agent session. */
  async getSession(request: AgentManagerGetSession): Promise<AgentSession> {
    return agentSessionFromProto(
      await this.client.getSession({
        sessionId: request.sessionId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Lists agent sessions visible to the invocation token. */
  async listSessions(
    request: AgentManagerListSessions = {},
  ): Promise<AgentSession[]> {
    const response = await this.client.listSessions({
      providerName: request.providerName ?? "",
      invocationToken: this.invocationToken,
      state: request.state ?? AgentSessionState.UNSPECIFIED,
      limit: request.limit ?? 0,
      summaryOnly: request.summaryOnly ?? false,
    });
    return response.sessions.map(agentSessionFromProto);
  }

  /** Updates mutable fields on an agent session. */
  async updateSession(
    request: AgentManagerUpdateSession,
  ): Promise<AgentSession> {
    return agentSessionFromProto(
      await this.client.updateSession({
        sessionId: request.sessionId,
        clientRef: request.clientRef ?? "",
        state: request.state ?? AgentSessionState.UNSPECIFIED,
        metadata: optionalStruct(request.metadata),
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Creates an agent turn. */
  async createTurn(request: AgentManagerCreateTurn): Promise<AgentTurn> {
    return agentTurnFromProto(
      await this.client.createTurn({
        sessionId: request.sessionId,
        model: request.model ?? "",
        messages: request.messages?.map(agentMessageToProto) ?? [],
        toolRefs: request.toolRefs?.map(agentToolRefToProto) ?? [],
        toolSource: request.toolSource ?? AgentToolSourceMode.UNSPECIFIED,
        responseSchema: optionalStruct(request.responseSchema),
        metadata: optionalStruct(request.metadata),
        idempotencyKey: request.idempotencyKey ?? "",
        invocationToken: this.invocationToken,
        modelOptions: optionalStruct(request.modelOptions),
      }),
    );
  }

  /** Fetches one agent turn. */
  async getTurn(request: AgentManagerGetTurn): Promise<AgentTurn> {
    return agentTurnFromProto(
      await this.client.getTurn({
        turnId: request.turnId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Lists turns for an agent session. */
  async listTurns(request: AgentManagerListTurns): Promise<AgentTurn[]> {
    const response = await this.client.listTurns({
      sessionId: request.sessionId,
      invocationToken: this.invocationToken,
      status: request.status ?? AgentExecutionStatus.UNSPECIFIED,
      limit: request.limit ?? 0,
      summaryOnly: request.summaryOnly ?? false,
    });
    return response.turns.map(agentTurnFromProto);
  }

  /** Cancels an in-progress agent turn. */
  async cancelTurn(request: AgentManagerCancelTurn): Promise<AgentTurn> {
    return agentTurnFromProto(
      await this.client.cancelTurn({
        turnId: request.turnId,
        reason: request.reason ?? "",
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Lists events emitted for an agent turn. */
  async listTurnEvents(
    request: AgentManagerListTurnEvents,
  ): Promise<AgentTurnEvent[]> {
    const response = await this.client.listTurnEvents({
      turnId: request.turnId,
      afterSeq: BigInt(request.afterSeq ?? 0),
      limit: request.limit ?? 0,
      invocationToken: this.invocationToken,
    });
    return response.events.map(agentTurnEventFromProto);
  }

  /** Lists pending or completed agent interactions. */
  async listInteractions(
    request: AgentManagerListInteractions,
  ): Promise<AgentInteraction[]> {
    const response = await this.client.listInteractions({
      turnId: request.turnId,
      invocationToken: this.invocationToken,
    });
    return response.interactions.map(agentInteractionFromProto);
  }

  /** Resolves an agent interaction with a host response. */
  async resolveInteraction(
    request: AgentManagerResolveInteraction,
  ): Promise<AgentInteraction> {
    return agentInteractionFromProto(
      await this.client.resolveInteraction({
        turnId: request.turnId,
        interactionId: request.interactionId,
        resolution: optionalStruct(request.resolution),
        invocationToken: this.invocationToken,
      }),
    );
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

function workspaceToProto(workspace?: AgentManagerWorkspace | undefined) {
  if (workspace === undefined) {
    return undefined;
  }
  return {
    checkouts: workspace.checkouts?.map((checkout) => ({
      url: checkout.url ?? "",
      ref: checkout.ref ?? "",
      path: checkout.path ?? "",
    })) ?? [],
    cwd: workspace.cwd ?? "",
  };
}

function agentSessionFromProto(session: ProtoAgentSession): AgentSession {
  return {
    id: session.id,
    providerName: session.providerName,
    model: session.model,
    clientRef: session.clientRef,
    state: session.state as AgentSessionState,
    metadata: optionalObjectFromStruct(session.metadata),
    createdBy: agentActorFromProto(session.createdBy),
    createdAt: optionalDate(session.createdAt),
    updatedAt: optionalDate(session.updatedAt),
    lastTurnAt: optionalDate(session.lastTurnAt),
  };
}

function agentTurnFromProto(turn: ProtoAgentTurn): AgentTurn {
  return {
    id: turn.id,
    sessionId: turn.sessionId,
    providerName: turn.providerName,
    model: turn.model,
    status: turn.status as AgentExecutionStatus,
    messages: turn.messages.map(agentMessageFromProto),
    outputText: turn.outputText,
    structuredOutput: optionalObjectFromStruct(turn.structuredOutput),
    statusMessage: turn.statusMessage,
    createdBy: agentActorFromProto(turn.createdBy),
    createdAt: optionalDate(turn.createdAt),
    startedAt: optionalDate(turn.startedAt),
    completedAt: optionalDate(turn.completedAt),
    executionRef: turn.executionRef,
  };
}

function agentTurnEventFromProto(event: ProtoAgentTurnEvent): AgentTurnEvent {
  return {
    id: event.id,
    turnId: event.turnId,
    seq: event.seq,
    type: event.type,
    source: event.source,
    visibility: event.visibility,
    data: optionalObjectFromStruct(event.data),
    createdAt: optionalDate(event.createdAt),
    display: agentTurnDisplayFromProto(event.display),
  };
}

function agentInteractionFromProto(
  interaction: ProtoAgentInteraction,
): AgentInteraction {
  return {
    id: interaction.id,
    type: interaction.type as AgentInteractionType,
    state: interaction.state as AgentInteractionState,
    title: interaction.title,
    prompt: interaction.prompt,
    request: optionalObjectFromStruct(interaction.request),
    resolution: optionalObjectFromStruct(interaction.resolution),
    createdAt: optionalDate(interaction.createdAt),
    resolvedAt: optionalDate(interaction.resolvedAt),
    turnId: interaction.turnId,
    sessionId: interaction.sessionId,
  };
}

function optionalDate(timestamp?: Parameters<typeof dateFromTimestamp>[0]) {
  return timestamp === undefined ? undefined : dateFromTimestamp(timestamp);
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
