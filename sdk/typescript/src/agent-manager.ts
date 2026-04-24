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
} from "../gen/v1/agent_pb.ts";
import type { Request } from "./api.ts";

export const ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET";
export const ENV_AGENT_MANAGER_SOCKET_TOKEN = `${ENV_AGENT_MANAGER_SOCKET}_TOKEN`;
const AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export type AgentManagerCreateSessionInput = MessageInitShape<
  typeof AgentManagerCreateSessionRequestSchema
>;
export type AgentManagerGetSessionInput = MessageInitShape<
  typeof AgentManagerGetSessionRequestSchema
>;
export type AgentManagerListSessionsInput = MessageInitShape<
  typeof AgentManagerListSessionsRequestSchema
>;
export type AgentManagerUpdateSessionInput = MessageInitShape<
  typeof AgentManagerUpdateSessionRequestSchema
>;
export type AgentManagerCreateTurnInput = MessageInitShape<
  typeof AgentManagerCreateTurnRequestSchema
>;
export type AgentManagerGetTurnInput = MessageInitShape<
  typeof AgentManagerGetTurnRequestSchema
>;
export type AgentManagerListTurnsInput = MessageInitShape<
  typeof AgentManagerListTurnsRequestSchema
>;
export type AgentManagerCancelTurnInput = MessageInitShape<
  typeof AgentManagerCancelTurnRequestSchema
>;
export type AgentManagerListTurnEventsInput = MessageInitShape<
  typeof AgentManagerListTurnEventsRequestSchema
>;
export type AgentManagerListInteractionsInput = MessageInitShape<
  typeof AgentManagerListInteractionsRequestSchema
>;
export type AgentManagerResolveInteractionInput = MessageInitShape<
  typeof AgentManagerResolveInteractionRequestSchema
>;

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

  async createSession(
    request: AgentManagerCreateSessionInput,
  ): Promise<AgentSession> {
    return await this.client.createSession({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async getSession(request: AgentManagerGetSessionInput): Promise<AgentSession> {
    return await this.client.getSession({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async listSessions(
    request: AgentManagerListSessionsInput = {},
  ): Promise<AgentSession[]> {
    const response = await this.client.listSessions({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.sessions];
  }

  async updateSession(
    request: AgentManagerUpdateSessionInput,
  ): Promise<AgentSession> {
    return await this.client.updateSession({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async createTurn(request: AgentManagerCreateTurnInput): Promise<AgentTurn> {
    return await this.client.createTurn({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async getTurn(request: AgentManagerGetTurnInput): Promise<AgentTurn> {
    return await this.client.getTurn({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async listTurns(request: AgentManagerListTurnsInput): Promise<AgentTurn[]> {
    const response = await this.client.listTurns({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.turns];
  }

  async cancelTurn(request: AgentManagerCancelTurnInput): Promise<AgentTurn> {
    return await this.client.cancelTurn({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async listTurnEvents(
    request: AgentManagerListTurnEventsInput,
  ): Promise<AgentTurnEvent[]> {
    const response = await this.client.listTurnEvents({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.events];
  }

  async listInteractions(
    request: AgentManagerListInteractionsInput,
  ): Promise<AgentInteraction[]> {
    const response = await this.client.listInteractions({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.interactions];
  }

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
