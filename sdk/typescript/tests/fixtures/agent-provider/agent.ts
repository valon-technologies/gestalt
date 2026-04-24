import { create, type JsonObject } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import {
  AgentExecutionStatus,
  AgentInteractionSchema,
  AgentInteractionState,
  AgentInteractionType,
  AgentSessionSchema,
  AgentSessionState,
  AgentTurnEventSchema,
  AgentTurnSchema,
  type AgentInteraction,
  type AgentSession,
  type AgentTurn,
  type AgentTurnEvent,
  type CreateAgentProviderTurnRequest,
  type GetAgentProviderInteractionRequest,
  type GetAgentProviderSessionRequest,
} from "../../../gen/v1/agent_pb.ts";
import { defineAgentProvider } from "../../../src/index.ts";

const sessions = new Map<string, AgentSession>();
const turns = new Map<string, AgentTurn>();
const turnEvents = new Map<string, AgentTurnEvent[]>();
const interactions = new Map<string, AgentInteraction>();
let canceledTurns = 0;

export const provider = defineAgentProvider({
  displayName: "Fixture Agent",
  description: "Agent provider fixture used by SDK tests",
  configure() {
    sessions.clear();
    turns.clear();
    turnEvents.clear();
    interactions.clear();
    canceledTurns = 0;
  },
  async createSession(request) {
    return upsertSession({
      sessionId: request.sessionId,
      model: request.model,
      clientRef: request.clientRef,
      metadata: request.metadata,
      createdBy: request.createdBy,
    });
  },
  async getSession(request) {
    return requireSession(request);
  },
  async listSessions() {
    return [...sessions.values()].sort((a, b) => a.id.localeCompare(b.id));
  },
  async updateSession(request) {
    const session = requireSessionByID(request.sessionId);
    const updated = create(AgentSessionSchema, {
      id: session.id,
      providerName: session.providerName,
      model: session.model,
      ...(request.clientRef !== undefined
        ? { clientRef: request.clientRef }
        : session.clientRef !== undefined
          ? { clientRef: session.clientRef }
          : {}),
      state: request.state || session.state,
      ...(request.metadata !== undefined
        ? { metadata: request.metadata }
        : session.metadata !== undefined
          ? { metadata: session.metadata }
          : {}),
      ...(session.createdBy !== undefined ? { createdBy: session.createdBy } : {}),
      ...(session.createdAt ? { createdAt: session.createdAt } : {}),
      updatedAt: timestampNow(),
      ...(session.lastTurnAt ? { lastTurnAt: session.lastTurnAt } : {}),
    });
    sessions.set(updated.id, updated);
    return updated;
  },
  async createTurn(request) {
    return createCanonicalTurn(request);
  },
  async getTurn(request) {
    return requireTurnByID(request.turnId);
  },
  async listTurns(request) {
    return [...turns.values()]
      .filter((turn) => !request.sessionId || turn.sessionId === request.sessionId)
      .sort((a, b) => a.id.localeCompare(b.id));
  },
  async cancelTurn(request) {
    return cancelCanonicalTurn({
      turnId: request.turnId,
      reason: request.reason,
    });
  },
  async listTurnEvents(request) {
    return (turnEvents.get(request.turnId) || [])
      .filter((event) => event.seq > request.afterSeq)
      .slice(0, request.limit > 0 ? request.limit : undefined);
  },
  async getInteraction(request) {
    return requireInteraction(request);
  },
  async listInteractions(request) {
    return [...interactions.values()]
      .filter((interaction) => !request.turnId || interaction.turnId === request.turnId)
      .sort((a, b) => a.id.localeCompare(b.id));
  },
  async resolveInteraction(request) {
    return resolveCanonicalInteraction({
      interactionId: request.interactionId,
      resolution: request.resolution,
    });
  },
  async getCapabilities() {
    return {
      streamingText: true,
      toolCalls: true,
      parallelToolCalls: false,
      structuredOutput: true,
      interactions: true,
      resumableTurns: true,
      reasoningSummaries: false,
    };
  },
  warnings() {
    return canceledTurns > 0 ? [`canceled-turns:${canceledTurns}`] : [];
  },
});

async function upsertSession(request: {
  sessionId: string;
  model: string;
  clientRef: string | undefined;
  metadata: JsonObject | undefined;
  createdBy: AgentSession["createdBy"] | undefined;
}): Promise<AgentSession> {
  const existing = sessions.get(request.sessionId);
  const session = create(AgentSessionSchema, {
    id: request.sessionId || `session-${sessions.size + 1}`,
    providerName: "fixture-agent",
    model: request.model,
    ...(request.clientRef !== undefined ? { clientRef: request.clientRef } : {}),
    state: existing?.state || AgentSessionState.ACTIVE,
    ...(request.metadata !== undefined ? { metadata: request.metadata } : {}),
    ...(request.createdBy !== undefined ? { createdBy: request.createdBy } : {}),
    ...(existing?.createdAt
      ? { createdAt: existing.createdAt }
      : { createdAt: timestampNow() }),
    updatedAt: timestampNow(),
    ...(existing?.lastTurnAt ? { lastTurnAt: existing.lastTurnAt } : {}),
  });
  sessions.set(session.id, session);
  return session;
}

async function createCanonicalTurn(
  request: CreateAgentProviderTurnRequest,
): Promise<AgentTurn> {
  const session = requireSessionByID(request.sessionId);
  const waitingForInput = Boolean(request.metadata?.requireInteraction);
  const status = waitingForInput
    ? AgentExecutionStatus.WAITING_FOR_INPUT
    : AgentExecutionStatus.SUCCEEDED;
  const outputText = request.messages.at(-1)?.text
    ? `echo:${request.messages.at(-1)!.text}`
    : "";
  const turn = create(AgentTurnSchema, {
    id: request.turnId || `turn-${turns.size + 1}`,
    sessionId: request.sessionId,
    providerName: session.providerName,
    model: request.model,
    status,
    messages: request.messages,
    outputText,
    statusMessage: waitingForInput ? "waiting for input" : "completed",
    ...(request.createdBy !== undefined ? { createdBy: request.createdBy } : {}),
    createdAt: timestampNow(),
    startedAt: timestampNow(),
    ...(waitingForInput ? {} : { completedAt: timestampNow() }),
    executionRef: request.executionRef,
  });
  turns.set(turn.id, turn);

  sessions.set(
    session.id,
    create(AgentSessionSchema, {
      id: session.id,
      providerName: session.providerName,
      model: session.model,
      ...(session.clientRef !== undefined ? { clientRef: session.clientRef } : {}),
      state: session.state,
      ...(session.metadata !== undefined ? { metadata: session.metadata } : {}),
      ...(session.createdBy !== undefined ? { createdBy: session.createdBy } : {}),
      ...(session.createdAt ? { createdAt: session.createdAt } : {}),
      updatedAt: timestampNow(),
      lastTurnAt: timestampNow(),
    }),
  );

  appendTurnEvent(turn.id, "turn.started", { sessionId: turn.sessionId });
  if (waitingForInput) {
    const interaction = create(AgentInteractionSchema, {
      id: `interaction-${interactions.size + 1}`,
      turnId: turn.id,
      sessionId: turn.sessionId,
      type: AgentInteractionType.APPROVAL,
      state: AgentInteractionState.PENDING,
      title: "Approve action",
      prompt: "Continue the agent turn?",
      request: { approved: true },
      createdAt: timestampNow(),
    });
    interactions.set(interaction.id, interaction);
    appendTurnEvent(turn.id, "interaction.requested", {
      interactionId: interaction.id,
    });
  } else {
    appendTurnEvent(turn.id, "assistant.completed", { sessionId: turn.sessionId });
    appendTurnEvent(turn.id, "turn.completed", { sessionId: turn.sessionId });
  }

  return turn;
}

async function cancelCanonicalTurn(request: {
  turnId: string;
  reason: string;
}): Promise<AgentTurn> {
  const turn = requireTurnByID(request.turnId);
  const updated = create(AgentTurnSchema, {
    id: turn.id,
    sessionId: turn.sessionId,
    providerName: turn.providerName,
    model: turn.model,
    status: AgentExecutionStatus.CANCELED,
    messages: turn.messages,
    outputText: turn.outputText,
    ...(turn.structuredOutput ? { structuredOutput: turn.structuredOutput } : {}),
    statusMessage: request.reason,
    ...(turn.createdBy ? { createdBy: turn.createdBy } : {}),
    ...(turn.createdAt ? { createdAt: turn.createdAt } : {}),
    ...(turn.startedAt ? { startedAt: turn.startedAt } : {}),
    completedAt: timestampNow(),
    executionRef: turn.executionRef,
  });
  turns.set(updated.id, updated);
  appendTurnEvent(updated.id, "turn.canceled", { reason: request.reason });
  canceledTurns += 1;
  return updated;
}

async function resolveCanonicalInteraction(request: {
  interactionId: string;
  resolution: JsonObject | undefined;
}): Promise<AgentInteraction> {
  const interaction = requireInteractionByID(request.interactionId);
  const resolved = create(AgentInteractionSchema, {
    id: interaction.id,
    turnId: interaction.turnId,
    sessionId: interaction.sessionId,
    type: interaction.type,
    state: AgentInteractionState.RESOLVED,
    title: interaction.title,
    prompt: interaction.prompt,
    ...(interaction.request ? { request: interaction.request } : {}),
    ...(request.resolution !== undefined ? { resolution: request.resolution } : {}),
    ...(interaction.createdAt ? { createdAt: interaction.createdAt } : {}),
    resolvedAt: timestampNow(),
  });
  interactions.set(resolved.id, resolved);

  const turn = requireTurnByID(resolved.turnId);
  const completedTurn = create(AgentTurnSchema, {
    id: turn.id,
    sessionId: turn.sessionId,
    providerName: turn.providerName,
    model: turn.model,
    status: AgentExecutionStatus.SUCCEEDED,
    messages: turn.messages,
    outputText: `resolved:${resolved.id}`,
    ...(turn.structuredOutput ? { structuredOutput: turn.structuredOutput } : {}),
    statusMessage: resolved.id,
    ...(turn.createdBy ? { createdBy: turn.createdBy } : {}),
    ...(turn.createdAt ? { createdAt: turn.createdAt } : {}),
    ...(turn.startedAt ? { startedAt: turn.startedAt } : {}),
    completedAt: timestampNow(),
    executionRef: turn.executionRef,
  });
  turns.set(completedTurn.id, completedTurn);

  appendTurnEvent(completedTurn.id, "interaction.resolved", {
    interactionId: resolved.id,
  });
  appendTurnEvent(completedTurn.id, "assistant.completed", {
    interactionId: resolved.id,
  });
  appendTurnEvent(completedTurn.id, "turn.completed", {
    interactionId: resolved.id,
  });
  return resolved;
}

function appendTurnEvent(turnId: string, type: string, data: JsonObject) {
  const events = turnEvents.get(turnId) || [];
  events.push(
    create(AgentTurnEventSchema, {
      id: `${turnId}-event-${events.length + 1}`,
      turnId,
      seq: BigInt(events.length + 1),
      type,
      source: "fixture-agent",
      visibility: "private",
      data,
      createdAt: timestampNow(),
    }),
  );
  turnEvents.set(turnId, events);
}

function requireSession(request: GetAgentProviderSessionRequest) {
  return requireSessionByID(request.sessionId);
}

function requireSessionByID(sessionId: string) {
  const session = sessions.get(sessionId);
  if (!session) {
    throw new Error(`unknown session ${sessionId}`);
  }
  return session;
}

function requireTurnByID(turnId: string) {
  const turn = turns.get(turnId);
  if (!turn) {
    throw new Error(`unknown turn ${turnId}`);
  }
  return turn;
}

function requireInteraction(request: GetAgentProviderInteractionRequest) {
  return requireInteractionByID(request.interactionId);
}

function requireInteractionByID(interactionId: string) {
  const interaction = interactions.get(interactionId);
  if (!interaction) {
    throw new Error(`unknown interaction ${interactionId}`);
  }
  return interaction;
}

function timestampNow() {
  return create(TimestampSchema, {
    seconds: BigInt(Math.trunc(Date.now() / 1000)),
    nanos: 0,
  });
}
