import { create } from "@bufbuild/protobuf";

import {
  AgentHost as AgentHostService,
  AgentProvider as AgentProviderService,
  CancelAgentProviderTurnRequestSchema,
  CreateAgentProviderSessionRequestSchema,
  CreateAgentProviderTurnRequestSchema,
  ExecuteAgentToolResponseSchema,
  GetAgentProviderCapabilitiesRequestSchema,
  GetAgentProviderSessionRequestSchema,
  GetAgentProviderTurnRequestSchema,
  ListAgentProviderTurnEventsRequestSchema,
  ListAgentToolsResponseSchema,
  ListedAgentToolSchema,
} from "./internal/gen/v1/agent_pb.ts";

export const agentContractServices: Record<string, unknown> = {
  AgentHost: AgentHostService,
  AgentHostService,
  AgentProvider: AgentProviderService,
  AgentProviderService,
};

export const agentContractSchemas: Record<string, unknown> = {
  CancelAgentProviderTurnRequestSchema,
  CreateAgentProviderSessionRequestSchema,
  CreateAgentProviderTurnRequestSchema,
  ExecuteAgentToolResponseSchema,
  GetAgentProviderCapabilitiesRequestSchema,
  GetAgentProviderSessionRequestSchema,
  GetAgentProviderTurnRequestSchema,
  ListAgentProviderTurnEventsRequestSchema,
  ListAgentToolsResponseSchema,
  ListedAgentToolSchema,
};

export function createAgentContractMessage<T = unknown>(
  schema: unknown,
  input: Record<string, unknown>,
): T {
  return create(schema as never, input as never) as T;
}
