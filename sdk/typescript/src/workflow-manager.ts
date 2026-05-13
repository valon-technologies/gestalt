import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  WorkflowManagerHost as WorkflowManagerHostService,
} from "./internal/gen/v1/workflow_pb.ts";
import type { Request } from "./api.ts";
import {
  boundWorkflowTargetToProto,
  managedWorkflowDefinitionFromProto,
  managedWorkflowEventTriggerFromProto,
  managedWorkflowRunFromProto,
  managedWorkflowRunSignalFromProto,
  managedWorkflowScheduleFromProto,
  workflowEventFromProto,
  workflowEventMatchToProto,
  workflowEventToProto,
  workflowSignalToProto,
  type BoundWorkflowTarget,
  type ManagedWorkflowDefinition,
  type ManagedWorkflowEventTrigger,
  type ManagedWorkflowRun,
  type ManagedWorkflowRunSignal,
  type ManagedWorkflowSchedule,
  type WorkflowEvent,
  type WorkflowEventMatch,
  type WorkflowSignal,
} from "./workflow.ts";

/** Environment variable containing the workflow-manager host-service target. */
export const ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET";
/** Environment variable containing the optional workflow-manager relay token. */
export const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN =
  `${ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN`;
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

/** Shape accepted when starting a workflow run. */
export interface WorkflowManagerStartRun {
  providerName: string;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
  workflowKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when signaling an existing workflow run. */
export interface WorkflowManagerSignalRun {
  runId: string;
  signal?: WorkflowSignal | WorkflowSignal | undefined;
}

/** Shape accepted when signaling a run or starting it if missing. */
export interface WorkflowManagerSignalOrStartRun {
  providerName: string;
  workflowKey: string;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
  signal?: WorkflowSignal | WorkflowSignal | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating a workflow definition. */
export interface WorkflowManagerCreateDefinition {
  providerName: string;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
}

/** Shape accepted when fetching a workflow definition. */
export interface WorkflowManagerGetDefinition {
  definitionId: string;
}

/** Shape accepted when updating a workflow definition. */
export interface WorkflowManagerUpdateDefinition {
  definitionId: string;
  providerName?: string | undefined;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
}

/** Shape accepted when deleting a workflow definition. */
export interface WorkflowManagerDeleteDefinition {
  definitionId: string;
}

/** Shape accepted when creating a workflow schedule. */
export interface WorkflowManagerCreateSchedule {
  providerName: string;
  cron: string;
  timezone?: string | undefined;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating an event trigger. */
export interface WorkflowManagerCreateTrigger {
  providerName: string;
  match?: WorkflowEventMatch | WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when fetching a workflow schedule. */
export interface WorkflowManagerGetSchedule {
  scheduleId: string;
}

/** Shape accepted when fetching an event trigger. */
export interface WorkflowManagerGetTrigger {
  triggerId: string;
}

/** Shape accepted when updating a workflow schedule. */
export interface WorkflowManagerUpdateSchedule {
  scheduleId: string;
  providerName?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when updating an event trigger. */
export interface WorkflowManagerUpdateTrigger {
  triggerId: string;
  providerName?: string | undefined;
  match?: WorkflowEventMatch | WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when deleting a workflow schedule. */
export interface WorkflowManagerDeleteSchedule {
  scheduleId: string;
}

/** Shape accepted when deleting an event trigger. */
export interface WorkflowManagerDeleteTrigger {
  triggerId: string;
}

/** Shape accepted when pausing a workflow schedule. */
export interface WorkflowManagerPauseSchedule {
  scheduleId: string;
}

/** Shape accepted when pausing an event trigger. */
export interface WorkflowManagerPauseTrigger {
  triggerId: string;
}

/** Shape accepted when resuming a workflow schedule. */
export interface WorkflowManagerResumeSchedule {
  scheduleId: string;
}

/** Shape accepted when resuming an event trigger. */
export interface WorkflowManagerResumeTrigger {
  triggerId: string;
}

/** Shape accepted when publishing a workflow event. */
export interface WorkflowManagerPublishEvent {
  event?: WorkflowEvent | WorkflowEvent | undefined;
  providerName?: string | undefined;
}

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

  /** Starts a workflow run immediately. */
  async startRun(
    request: WorkflowManagerStartRun,
  ): Promise<ManagedWorkflowRun> {
    return managedWorkflowRunFromProto(
      await this.client.startRun({
        providerName: request.providerName,
        target: boundWorkflowTargetToProto(request.target),
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
        workflowKey: request.workflowKey ?? "",
        invocationToken: this.invocationToken,
        definitionId: request.definitionId ?? "",
      }),
    );
  }

  /** Signals an existing workflow run. */
  async signalRun(
    request: WorkflowManagerSignalRun,
  ): Promise<ManagedWorkflowRunSignal> {
    return managedWorkflowRunSignalFromProto(
      await this.client.signalRun({
        runId: request.runId,
        signal: workflowSignalToProto(request.signal),
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Signals a workflow run, or starts it when no run exists for the key. */
  async signalOrStartRun(
    request: WorkflowManagerSignalOrStartRun,
  ): Promise<ManagedWorkflowRunSignal> {
    return managedWorkflowRunSignalFromProto(
      await this.client.signalOrStartRun({
        providerName: request.providerName,
        workflowKey: request.workflowKey,
        target: boundWorkflowTargetToProto(request.target),
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
        signal: workflowSignalToProto(request.signal),
        invocationToken: this.invocationToken,
        definitionId: request.definitionId ?? "",
      }),
    );
  }

  /** Creates a reusable workflow definition. */
  async createDefinition(
    request: WorkflowManagerCreateDefinition,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.createDefinition({
        providerName: request.providerName,
        target: boundWorkflowTargetToProto(request.target),
        invocationToken: this.invocationToken,
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      }),
    );
  }

  /** Fetches one workflow definition. */
  async getDefinition(
    request: WorkflowManagerGetDefinition,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.getDefinition({
        definitionId: request.definitionId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates a workflow definition. */
  async updateDefinition(
    request: WorkflowManagerUpdateDefinition,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.updateDefinition({
        definitionId: request.definitionId,
        providerName: request.providerName ?? "",
        target: boundWorkflowTargetToProto(request.target),
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Deletes a workflow definition. */
  async deleteDefinition(
    request: WorkflowManagerDeleteDefinition,
  ): Promise<void> {
    await this.client.deleteDefinition({
      definitionId: request.definitionId,
      invocationToken: this.invocationToken,
    });
  }

  /** Creates a workflow schedule. */
  async createSchedule(
    request: WorkflowManagerCreateSchedule,
  ): Promise<ManagedWorkflowSchedule> {
    return managedWorkflowScheduleFromProto(
      await this.client.createSchedule({
        providerName: request.providerName,
        cron: request.cron,
        timezone: request.timezone ?? "",
        target: boundWorkflowTargetToProto(request.target),
        paused: request.paused ?? false,
        invocationToken: this.invocationToken,
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
        definitionId: request.definitionId ?? "",
      }),
    );
  }

  /** Fetches one workflow schedule. */
  async getSchedule(
    request: WorkflowManagerGetSchedule,
  ): Promise<ManagedWorkflowSchedule> {
    return managedWorkflowScheduleFromProto(
      await this.client.getSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates a workflow schedule. */
  async updateSchedule(
    request: WorkflowManagerUpdateSchedule,
  ): Promise<ManagedWorkflowSchedule> {
    return managedWorkflowScheduleFromProto(
      await this.client.updateSchedule({
        scheduleId: request.scheduleId,
        providerName: request.providerName ?? "",
        cron: request.cron ?? "",
        timezone: request.timezone ?? "",
        target: boundWorkflowTargetToProto(request.target),
        paused: request.paused ?? false,
        invocationToken: this.invocationToken,
        definitionId: request.definitionId ?? "",
      }),
    );
  }

  /** Deletes a workflow schedule. */
  async deleteSchedule(
    request: WorkflowManagerDeleteSchedule,
  ): Promise<void> {
    await this.client.deleteSchedule({
      scheduleId: request.scheduleId,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses a workflow schedule. */
  async pauseSchedule(
    request: WorkflowManagerPauseSchedule,
  ): Promise<ManagedWorkflowSchedule> {
    return managedWorkflowScheduleFromProto(
      await this.client.pauseSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Resumes a workflow schedule. */
  async resumeSchedule(
    request: WorkflowManagerResumeSchedule,
  ): Promise<ManagedWorkflowSchedule> {
    return managedWorkflowScheduleFromProto(
      await this.client.resumeSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Creates an event trigger. */
  async createTrigger(
    request: WorkflowManagerCreateTrigger,
  ): Promise<ManagedWorkflowEventTrigger> {
    return managedWorkflowEventTriggerFromProto(
      await this.client.createEventTrigger({
        providerName: request.providerName,
        match: workflowEventMatchToProto(request.match),
        target: boundWorkflowTargetToProto(request.target),
        paused: request.paused ?? false,
        invocationToken: this.invocationToken,
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
        definitionId: request.definitionId ?? "",
      }),
    );
  }

  /** Fetches one event trigger. */
  async getTrigger(
    request: WorkflowManagerGetTrigger,
  ): Promise<ManagedWorkflowEventTrigger> {
    return managedWorkflowEventTriggerFromProto(
      await this.client.getEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates an event trigger. */
  async updateTrigger(
    request: WorkflowManagerUpdateTrigger,
  ): Promise<ManagedWorkflowEventTrigger> {
    return managedWorkflowEventTriggerFromProto(
      await this.client.updateEventTrigger({
        triggerId: request.triggerId,
        providerName: request.providerName ?? "",
        match: workflowEventMatchToProto(request.match),
        target: boundWorkflowTargetToProto(request.target),
        paused: request.paused ?? false,
        invocationToken: this.invocationToken,
        definitionId: request.definitionId ?? "",
      }),
    );
  }

  /** Deletes an event trigger. */
  async deleteTrigger(
    request: WorkflowManagerDeleteTrigger,
  ): Promise<void> {
    await this.client.deleteEventTrigger({
      triggerId: request.triggerId,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses an event trigger. */
  async pauseTrigger(
    request: WorkflowManagerPauseTrigger,
  ): Promise<ManagedWorkflowEventTrigger> {
    return managedWorkflowEventTriggerFromProto(
      await this.client.pauseEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Resumes an event trigger. */
  async resumeTrigger(
    request: WorkflowManagerResumeTrigger,
  ): Promise<ManagedWorkflowEventTrigger> {
    return managedWorkflowEventTriggerFromProto(
      await this.client.resumeEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Publishes an event into the workflow manager. */
  async publishEvent(
    request: WorkflowManagerPublishEvent,
  ): Promise<WorkflowEvent> {
    const event = workflowEventFromProto(
      await this.client.publishEvent({
        event: workflowEventToProto(request.event),
        invocationToken: this.invocationToken,
        providerName: request.providerName ?? "",
      }),
    );
    if (event === undefined) {
      throw new Error("WorkflowManager.publishEvent returned no event");
    }
    return event;
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
