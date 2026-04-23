import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  AgentManagerCancelRunRequestSchema,
  AgentManagerGetRunRequestSchema,
  AgentManagerHost as AgentManagerHostService,
  AgentManagerListRunsRequestSchema,
  AgentManagerRunRequestSchema,
  type ManagedAgentRun,
} from "../gen/v1/agent_pb.ts";
import type { Request } from "./api.ts";

export const ENV_AGENT_MANAGER_SOCKET = "GESTALT_AGENT_MANAGER_SOCKET";
export const ENV_AGENT_MANAGER_SOCKET_TOKEN = `${ENV_AGENT_MANAGER_SOCKET}_TOKEN`;
const AGENT_MANAGER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

export type ManagedAgentRunMessage = ManagedAgentRun;
export type AgentManagerRunInput = MessageInitShape<
  typeof AgentManagerRunRequestSchema
>;
export type AgentManagerGetRunInput = MessageInitShape<
  typeof AgentManagerGetRunRequestSchema
>;
export type AgentManagerListRunsInput = MessageInitShape<
  typeof AgentManagerListRunsRequestSchema
>;
export type AgentManagerCancelRunInput = MessageInitShape<
  typeof AgentManagerCancelRunRequestSchema
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

  async run(request: AgentManagerRunInput): Promise<ManagedAgentRunMessage> {
    return await this.client.run({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async getRun(
    request: AgentManagerGetRunInput,
  ): Promise<ManagedAgentRunMessage> {
    return await this.client.getRun({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async listRuns(
    request: AgentManagerListRunsInput = {},
  ): Promise<ManagedAgentRunMessage[]> {
    const response = await this.client.listRuns({
      ...request,
      invocationToken: this.invocationToken,
    });
    return [...response.runs];
  }

  async cancelRun(
    request: AgentManagerCancelRunInput,
  ): Promise<ManagedAgentRunMessage> {
    return await this.client.cancelRun({
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
