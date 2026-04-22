import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  WorkflowManagerCreateScheduleRequestSchema,
  WorkflowManagerCreateEventTriggerRequestSchema,
  WorkflowManagerDeleteScheduleRequestSchema,
  WorkflowManagerDeleteEventTriggerRequestSchema,
  WorkflowManagerGetScheduleRequestSchema,
  WorkflowManagerGetEventTriggerRequestSchema,
  WorkflowManagerHost as WorkflowManagerHostService,
  WorkflowManagerPauseScheduleRequestSchema,
  WorkflowManagerPauseEventTriggerRequestSchema,
  WorkflowManagerPublishEventRequestSchema,
  WorkflowManagerResumeScheduleRequestSchema,
  WorkflowManagerResumeEventTriggerRequestSchema,
  WorkflowManagerUpdateScheduleRequestSchema,
  WorkflowManagerUpdateEventTriggerRequestSchema,
  type ManagedWorkflowSchedule,
  type ManagedWorkflowEventTrigger,
  type WorkflowEvent,
} from "../gen/v1/workflow_pb.ts";
import type { Request } from "./api.ts";

export const ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET";
export const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN = `${ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN`;
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

export type ManagedWorkflowScheduleMessage = ManagedWorkflowSchedule;
export type ManagedWorkflowEventTriggerMessage = ManagedWorkflowEventTrigger;
export type WorkflowEventMessage = WorkflowEvent;
export type WorkflowManagerCreateScheduleInput = MessageInitShape<
  typeof WorkflowManagerCreateScheduleRequestSchema
>;
export type WorkflowManagerCreateTriggerInput = MessageInitShape<
  typeof WorkflowManagerCreateEventTriggerRequestSchema
>;
export type WorkflowManagerGetScheduleInput = MessageInitShape<
  typeof WorkflowManagerGetScheduleRequestSchema
>;
export type WorkflowManagerGetTriggerInput = MessageInitShape<
  typeof WorkflowManagerGetEventTriggerRequestSchema
>;
export type WorkflowManagerUpdateScheduleInput = MessageInitShape<
  typeof WorkflowManagerUpdateScheduleRequestSchema
>;
export type WorkflowManagerUpdateTriggerInput = MessageInitShape<
  typeof WorkflowManagerUpdateEventTriggerRequestSchema
>;
export type WorkflowManagerDeleteScheduleInput = MessageInitShape<
  typeof WorkflowManagerDeleteScheduleRequestSchema
>;
export type WorkflowManagerDeleteTriggerInput = MessageInitShape<
  typeof WorkflowManagerDeleteEventTriggerRequestSchema
>;
export type WorkflowManagerPauseScheduleInput = MessageInitShape<
  typeof WorkflowManagerPauseScheduleRequestSchema
>;
export type WorkflowManagerPauseTriggerInput = MessageInitShape<
  typeof WorkflowManagerPauseEventTriggerRequestSchema
>;
export type WorkflowManagerResumeScheduleInput = MessageInitShape<
  typeof WorkflowManagerResumeScheduleRequestSchema
>;
export type WorkflowManagerResumeTriggerInput = MessageInitShape<
  typeof WorkflowManagerResumeEventTriggerRequestSchema
>;
export type WorkflowManagerPublishEventInput = MessageInitShape<
  typeof WorkflowManagerPublishEventRequestSchema
>;

export class WorkflowManager {
  private readonly client: Client<typeof WorkflowManagerHostService>;
  private readonly invocationToken: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);

    const target = process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    if (!target) {
      throw new Error(
        `workflow manager: ${ENV_WORKFLOW_MANAGER_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN]?.trim() ?? "";

    const transport = createGrpcTransport({
      ...workflowManagerTransportOptions(target),
      interceptors: relayToken
        ? [workflowManagerRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(WorkflowManagerHostService, transport);
  }

  async createSchedule(
    request: WorkflowManagerCreateScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.createSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async getSchedule(
    request: WorkflowManagerGetScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.getSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async updateSchedule(
    request: WorkflowManagerUpdateScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.updateSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async deleteSchedule(
    request: WorkflowManagerDeleteScheduleInput,
  ): Promise<void> {
    await this.client.deleteSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async pauseSchedule(
    request: WorkflowManagerPauseScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.pauseSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async resumeSchedule(
    request: WorkflowManagerResumeScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.resumeSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async createTrigger(
    request: WorkflowManagerCreateTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.createEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async getTrigger(
    request: WorkflowManagerGetTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.getEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async updateTrigger(
    request: WorkflowManagerUpdateTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.updateEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async deleteTrigger(
    request: WorkflowManagerDeleteTriggerInput,
  ): Promise<void> {
    await this.client.deleteEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async pauseTrigger(
    request: WorkflowManagerPauseTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.pauseEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async resumeTrigger(
    request: WorkflowManagerResumeTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.resumeEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  async publishEvent(
    request: WorkflowManagerPublishEventInput,
  ): Promise<WorkflowEventMessage> {
    return await this.client.publishEvent({
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
    throw new Error("workflow manager: invocation token is not available");
  }
  return trimmed;
}

function workflowManagerTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("workflow manager: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `workflow manager: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `workflow manager: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `workflow manager: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `workflow manager: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function workflowManagerRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(WORKFLOW_MANAGER_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}
