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

/** Environment variable containing the workflow-manager host-service target. */
export const ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET";
/** Environment variable containing the optional workflow-manager relay token. */
export const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN =
  `${ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN`;
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

/** Managed workflow schedule message returned by the host manager. */
export type ManagedWorkflowScheduleMessage = ManagedWorkflowSchedule;
/** Managed workflow event-trigger message returned by the host manager. */
export type ManagedWorkflowEventTriggerMessage = ManagedWorkflowEventTrigger;
/** Workflow event message returned after publishing an event. */
export type WorkflowEventMessage = WorkflowEvent;
/** Shape accepted when creating a workflow schedule. */
export type WorkflowManagerCreateScheduleInput = MessageInitShape<
  typeof WorkflowManagerCreateScheduleRequestSchema
>;
/** Shape accepted when creating an event trigger. */
export type WorkflowManagerCreateTriggerInput = MessageInitShape<
  typeof WorkflowManagerCreateEventTriggerRequestSchema
>;
/** Shape accepted when fetching a workflow schedule. */
export type WorkflowManagerGetScheduleInput = MessageInitShape<
  typeof WorkflowManagerGetScheduleRequestSchema
>;
/** Shape accepted when fetching an event trigger. */
export type WorkflowManagerGetTriggerInput = MessageInitShape<
  typeof WorkflowManagerGetEventTriggerRequestSchema
>;
/** Shape accepted when updating a workflow schedule. */
export type WorkflowManagerUpdateScheduleInput = MessageInitShape<
  typeof WorkflowManagerUpdateScheduleRequestSchema
>;
/** Shape accepted when updating an event trigger. */
export type WorkflowManagerUpdateTriggerInput = MessageInitShape<
  typeof WorkflowManagerUpdateEventTriggerRequestSchema
>;
/** Shape accepted when deleting a workflow schedule. */
export type WorkflowManagerDeleteScheduleInput = MessageInitShape<
  typeof WorkflowManagerDeleteScheduleRequestSchema
>;
/** Shape accepted when deleting an event trigger. */
export type WorkflowManagerDeleteTriggerInput = MessageInitShape<
  typeof WorkflowManagerDeleteEventTriggerRequestSchema
>;
/** Shape accepted when pausing a workflow schedule. */
export type WorkflowManagerPauseScheduleInput = MessageInitShape<
  typeof WorkflowManagerPauseScheduleRequestSchema
>;
/** Shape accepted when pausing an event trigger. */
export type WorkflowManagerPauseTriggerInput = MessageInitShape<
  typeof WorkflowManagerPauseEventTriggerRequestSchema
>;
/** Shape accepted when resuming a workflow schedule. */
export type WorkflowManagerResumeScheduleInput = MessageInitShape<
  typeof WorkflowManagerResumeScheduleRequestSchema
>;
/** Shape accepted when resuming an event trigger. */
export type WorkflowManagerResumeTriggerInput = MessageInitShape<
  typeof WorkflowManagerResumeEventTriggerRequestSchema
>;
/** Shape accepted when publishing a workflow event. */
export type WorkflowManagerPublishEventInput = MessageInitShape<
  typeof WorkflowManagerPublishEventRequestSchema
>;

/**
 * Client for creating and controlling workflow schedules and event triggers.
 *
 * The constructor accepts either a Gestalt request or an invocation token. Each
 * manager call forwards that token to the host service. When constructed from a
 * request, create operations reuse the request idempotency key unless the call
 * provides one explicitly.
 */
export class WorkflowManager {
  private readonly client: Client<typeof WorkflowManagerHostService>;
  private readonly invocationToken: string;
  private readonly idempotencyKey: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);
    this.idempotencyKey = normalizeIdempotencyKey(requestOrToken);

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

  /** Creates a workflow schedule. */
  async createSchedule(
    request: WorkflowManagerCreateScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.createSchedule({
      ...request,
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    });
  }

  /** Fetches one workflow schedule. */
  async getSchedule(
    request: WorkflowManagerGetScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.getSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Updates a workflow schedule. */
  async updateSchedule(
    request: WorkflowManagerUpdateScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.updateSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Deletes a workflow schedule. */
  async deleteSchedule(
    request: WorkflowManagerDeleteScheduleInput,
  ): Promise<void> {
    await this.client.deleteSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses a workflow schedule. */
  async pauseSchedule(
    request: WorkflowManagerPauseScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.pauseSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Resumes a workflow schedule. */
  async resumeSchedule(
    request: WorkflowManagerResumeScheduleInput,
  ): Promise<ManagedWorkflowScheduleMessage> {
    return await this.client.resumeSchedule({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Creates an event trigger. */
  async createTrigger(
    request: WorkflowManagerCreateTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.createEventTrigger({
      ...request,
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    });
  }

  /** Fetches one event trigger. */
  async getTrigger(
    request: WorkflowManagerGetTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.getEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Updates an event trigger. */
  async updateTrigger(
    request: WorkflowManagerUpdateTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.updateEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Deletes an event trigger. */
  async deleteTrigger(
    request: WorkflowManagerDeleteTriggerInput,
  ): Promise<void> {
    await this.client.deleteEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses an event trigger. */
  async pauseTrigger(
    request: WorkflowManagerPauseTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.pauseEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Resumes an event trigger. */
  async resumeTrigger(
    request: WorkflowManagerResumeTriggerInput,
  ): Promise<ManagedWorkflowEventTriggerMessage> {
    return await this.client.resumeEventTrigger({
      ...request,
      invocationToken: this.invocationToken,
    });
  }

  /** Publishes an event into the workflow manager. */
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

function normalizeIdempotencyKey(requestOrToken: Request | string): string {
  if (typeof requestOrToken === "string") {
    return "";
  }
  return requestOrToken.idempotencyKey.trim();
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
