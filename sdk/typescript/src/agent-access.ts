import {
  createClient,
  type Client,
} from "@connectrpc/connect";

import {
  AgentProvider as AgentProviderService,
  type AgentInteraction as ProtoAgentInteraction,
  type AgentSession as ProtoAgentSession,
  type AgentTurn as ProtoAgentTurn,
  type AgentTurnEvent as ProtoAgentTurnEvent,
} from "./internal/gen/v1/agent_pb.ts";
import type { Request } from "./api.ts";
import {
  agentOutputToProto,
  agentMessageFromProto,
  agentMessageToProto,
  agentToolRefToProto,
  agentTurnOutputFromProto,
  agentTurnDisplayFromProto,
} from "./agent-conversions.ts";
import {
  AgentExecutionStatus,
  AgentInteractionState,
  AgentInteractionType,
  AgentSessionState,
  AgentToolSourceMode,
  type AgentInteraction,
  type AgentOutput,
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
import { hostInvocationContext } from "./invocation-context.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";

export interface AgentWorkspaceGitCheckout {
  url?: string | undefined;
  ref?: string | undefined;
  path?: string | undefined;
}

export interface AgentWorkspace {
  checkouts?: readonly AgentWorkspaceGitCheckout[] | undefined;
  cwd?: string | undefined;
}

/** Shape accepted when creating an agent session through the agent facade. */
export interface AgentCreateSession {
  providerName?: string | undefined;
  model?: string | undefined;
  clientRef?: string | undefined;
  metadata?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  workspace?: AgentWorkspace | undefined;
}

/** Shape accepted when fetching an agent session through the agent facade. */
export interface AgentGetSession {
  sessionId: string;
}

/** Shape accepted when listing agent sessions through the agent facade. */
export interface AgentListSessions {
  providerName?: string | undefined;
  state?: AgentSessionState | undefined;
  limit?: number | undefined;
  summaryOnly?: boolean | undefined;
}

/** Shape accepted when updating an agent session through the agent facade. */
export interface AgentUpdateSession {
  sessionId: string;
  clientRef?: string | undefined;
  state?: AgentSessionState | undefined;
  metadata?: JsonObjectInput | undefined;
}

/** Shape accepted when creating an agent turn through the agent facade. */
export interface AgentCreateTurn {
  sessionId: string;
  model?: string | undefined;
  messages?: readonly AgentMessage[] | undefined;
  toolRefs?: readonly AgentToolRef[] | undefined;
  toolSource?: AgentToolSourceMode | undefined;
  output: AgentOutput;
  metadata?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  modelOptions?: JsonObjectInput | undefined;
  timeoutSeconds?: number | undefined;
}

/** Shape accepted when fetching an agent turn through the agent facade. */
export interface AgentGetTurn {
  turnId: string;
}

/** Shape accepted when listing agent turns through the agent facade. */
export interface AgentListTurns {
  sessionId: string;
  status?: AgentExecutionStatus | undefined;
  limit?: number | undefined;
  summaryOnly?: boolean | undefined;
}

/** Shape accepted when cancelling an agent turn through the agent facade. */
export interface AgentCancelTurn {
  turnId: string;
  reason?: string | undefined;
}

/** Shape accepted when listing events for an agent turn. */
export interface AgentListTurnEvents {
  turnId: string;
  afterSeq?: bigint | number | undefined;
  limit?: number | undefined;
}

/** Shape accepted when listing agent interactions. */
export interface AgentListInteractions {
  turnId: string;
}

/** Shape accepted when resolving an agent interaction. */
export interface AgentResolveInteraction {
  turnId: string;
  interactionId: string;
  resolution?: JsonObjectInput | undefined;
}

/** Fakeable client contract for managing agent sessions and turns. */
export interface Agent {
  createSession(request: AgentCreateSession): Promise<AgentSession>;
  getSession(request: AgentGetSession): Promise<AgentSession>;
  listSessions(request?: AgentListSessions): Promise<AgentSession[]>;
  updateSession(request: AgentUpdateSession): Promise<AgentSession>;
  createTurn(request: AgentCreateTurn): Promise<AgentTurn>;
  getTurn(request: AgentGetTurn): Promise<AgentTurn>;
  listTurns(request: AgentListTurns): Promise<AgentTurn[]>;
  cancelTurn(request: AgentCancelTurn): Promise<AgentTurn>;
  listTurnEvents(request: AgentListTurnEvents): Promise<AgentTurnEvent[]>;
  listInteractions(
    request: AgentListInteractions,
  ): Promise<AgentInteraction[]>;
  resolveInteraction(
    request: AgentResolveInteraction,
  ): Promise<AgentInteraction>;
}

/**
 * Client for managing agent sessions, turns, events, and interactions.
 *
 * The constructor accepts either a Gestalt request or an invocation token. Each
 * agent call forwards that token to the agent-provider facade.
 */
class AgentImpl implements Agent {
  private readonly client: Client<typeof AgentProviderService>;
  private readonly invocationContext: ReturnType<typeof hostInvocationContext>;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationContext = hostInvocationContext(requestOrToken);

    const target = process.env[ENV_HOST_SERVICE_SOCKET]?.trim();
    if (!target) {
      throw new Error(`agent: ${ENV_HOST_SERVICE_SOCKET} is not set`);
    }
    const relayToken =
      process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";

    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("agent", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(AgentProviderService, transport);
  }

  /** Creates an agent session. */
  async createSession(
    request: AgentCreateSession,
  ): Promise<AgentSession> {
    return agentSessionFromProto(
      await this.client.createSession({
        providerName: request.providerName ?? "",
        model: request.model ?? "",
        clientRef: request.clientRef ?? "",
        metadata: optionalStruct(request.metadata),
        idempotencyKey: request.idempotencyKey ?? "",
        ...this.invocationContext,
        workspace: workspaceToProto(request.workspace),
      }),
    );
  }

  /** Fetches one agent session. */
  async getSession(request: AgentGetSession): Promise<AgentSession> {
    return agentSessionFromProto(
      await this.client.getSession({
        sessionId: request.sessionId,
        ...this.invocationContext,
      }),
    );
  }

  /** Lists agent sessions visible to the invocation token. */
  async listSessions(
    request: AgentListSessions = {},
  ): Promise<AgentSession[]> {
    const response = await this.client.listSessions({
      providerName: request.providerName ?? "",
      ...this.invocationContext,
      state: request.state ?? AgentSessionState.UNSPECIFIED,
      limit: request.limit ?? 0,
      summaryOnly: request.summaryOnly ?? false,
    });
    return response.sessions.map(agentSessionFromProto);
  }

  /** Updates mutable fields on an agent session. */
  async updateSession(
    request: AgentUpdateSession,
  ): Promise<AgentSession> {
    return agentSessionFromProto(
      await this.client.updateSession({
        sessionId: request.sessionId,
        clientRef: request.clientRef ?? "",
        state: request.state ?? AgentSessionState.UNSPECIFIED,
        metadata: optionalStruct(request.metadata),
        ...this.invocationContext,
      }),
    );
  }

  /** Creates an agent turn. */
  async createTurn(request: AgentCreateTurn): Promise<AgentTurn> {
    return agentTurnFromProto(
      await this.client.createTurn({
        sessionId: request.sessionId,
        model: request.model ?? "",
        messages: request.messages?.map(agentMessageToProto) ?? [],
        toolRefs: request.toolRefs?.map(agentToolRefToProto) ?? [],
        toolSource: request.toolSource ?? AgentToolSourceMode.UNSPECIFIED,
        output: agentOutputToProto(request.output),
        metadata: optionalStruct(request.metadata),
        idempotencyKey: request.idempotencyKey ?? "",
        ...this.invocationContext,
        modelOptions: optionalStruct(request.modelOptions),
        timeoutSeconds: request.timeoutSeconds ?? 0,
      }),
    );
  }

  /** Fetches one agent turn. */
  async getTurn(request: AgentGetTurn): Promise<AgentTurn> {
    return agentTurnFromProto(
      await this.client.getTurn({
        turnId: request.turnId,
        ...this.invocationContext,
      }),
    );
  }

  /** Lists turns for an agent session. */
  async listTurns(request: AgentListTurns): Promise<AgentTurn[]> {
    const response = await this.client.listTurns({
      sessionId: request.sessionId,
      ...this.invocationContext,
      status: request.status ?? AgentExecutionStatus.UNSPECIFIED,
      limit: request.limit ?? 0,
      summaryOnly: request.summaryOnly ?? false,
    });
    return response.turns.map(agentTurnFromProto);
  }

  /** Cancels an in-progress agent turn. */
  async cancelTurn(request: AgentCancelTurn): Promise<AgentTurn> {
    return agentTurnFromProto(
      await this.client.cancelTurn({
        turnId: request.turnId,
        reason: request.reason ?? "",
        ...this.invocationContext,
      }),
    );
  }

  /** Lists events emitted for an agent turn. */
  async listTurnEvents(
    request: AgentListTurnEvents,
  ): Promise<AgentTurnEvent[]> {
    const response = await this.client.listTurnEvents({
      turnId: request.turnId,
      afterSeq: BigInt(request.afterSeq ?? 0),
      limit: request.limit ?? 0,
      ...this.invocationContext,
    });
    return response.events.map(agentTurnEventFromProto);
  }

  /** Lists pending or completed agent interactions. */
  async listInteractions(
    request: AgentListInteractions,
  ): Promise<AgentInteraction[]> {
    const response = await this.client.listInteractions({
      turnId: request.turnId,
      ...this.invocationContext,
    });
    return response.interactions.map(agentInteractionFromProto);
  }

  /** Resolves an agent interaction with a host response. */
  async resolveInteraction(
    request: AgentResolveInteraction,
  ): Promise<AgentInteraction> {
    return agentInteractionFromProto(
      await this.client.resolveInteraction({
        turnId: request.turnId,
        interactionId: request.interactionId,
        resolution: optionalStruct(request.resolution),
        ...this.invocationContext,
      }),
    );
  }
}

function workspaceToProto(workspace?: AgentWorkspace | undefined) {
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

export const Agent = AgentImpl;

function agentSessionFromProto(session: ProtoAgentSession): AgentSession {
  return {
    id: session.id,
    providerName: session.providerName,
    model: session.model,
    clientRef: session.clientRef,
    state: session.state as AgentSessionState,
    metadata: optionalObjectFromStruct(session.metadata),
    createdBySubjectId: session.createdBySubjectId ?? "",
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
    output: agentTurnOutputFromProto(turn.output),
    statusMessage: turn.statusMessage,
    createdBySubjectId: turn.createdBySubjectId ?? "",
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
