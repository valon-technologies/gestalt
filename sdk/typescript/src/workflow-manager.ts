import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  WorkflowManagerHost as WorkflowManagerHostService,
  type BoundWorkflowDefinition,
  type BoundWorkflowEventTrigger,
  type BoundWorkflowRun,
  type BoundWorkflowSchedule,
  type BoundWorkflowTarget,
  type ManagedWorkflowDefinition,
  type ManagedWorkflowRun,
  type ManagedWorkflowRunSignal,
  type ManagedWorkflowSchedule,
  type ManagedWorkflowEventTrigger,
  type WorkflowEvent,
  type WorkflowEventMatch,
  type WorkflowSignal,
} from "./internal/gen/v1/workflow_pb.ts";
import type { Request } from "./api.ts";
import {
  WorkflowRunStatus,
  boundWorkflowDefinitionInputFromDefinition,
  boundWorkflowRunInputFromRun,
  boundWorkflowEventTriggerInputFromTrigger,
  boundWorkflowScheduleInputFromSchedule,
  boundWorkflowTarget,
  workflowEvent,
  workflowEventInputFromEvent,
  workflowEventMatch,
  workflowSignal,
  workflowSignalInputFromSignal,
  type BoundWorkflowRunInput,
  type BoundWorkflowTargetInput,
  type WorkflowActorInput,
  type WorkflowEventInput,
  type WorkflowEventMatchInput,
  type WorkflowRunTriggerInput,
  type WorkflowSignalInput,
} from "./workflow.ts";

/** Environment variable containing the workflow-manager host-service target. */
export const ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET";
/** Environment variable containing the optional workflow-manager relay token. */
export const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN =
  `${ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN`;
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

/** Shape accepted when starting a workflow run. */
export interface WorkflowManagerStartRunInput {
  providerName?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  idempotencyKey?: string | undefined;
  workflowKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when signaling an existing workflow run. */
export interface WorkflowManagerSignalRunInput {
  runId: string;
  signal?: WorkflowSignalInput | undefined;
}

/** Shape accepted when signaling a run or starting it if missing. */
export interface WorkflowManagerSignalOrStartRunInput {
  providerName?: string | undefined;
  workflowKey?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  idempotencyKey?: string | undefined;
  signal?: WorkflowSignalInput | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating a workflow definition. */
export interface WorkflowManagerCreateDefinitionInput {
  providerName?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  idempotencyKey?: string | undefined;
}

/** Shape accepted when fetching a workflow definition. */
export interface WorkflowManagerGetDefinitionInput {
  definitionId: string;
}

/** Shape accepted when updating a workflow definition. */
export interface WorkflowManagerUpdateDefinitionInput {
  definitionId: string;
  providerName?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
}

/** Shape accepted when deleting a workflow definition. */
export interface WorkflowManagerDeleteDefinitionInput {
  definitionId: string;
}

/** Shape accepted when creating a workflow schedule. */
export interface WorkflowManagerCreateScheduleInput {
  providerName?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating an event trigger. */
export interface WorkflowManagerCreateTriggerInput {
  providerName?: string | undefined;
  match?: WorkflowEventMatchInput | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when fetching a workflow schedule. */
export interface WorkflowManagerGetScheduleInput {
  scheduleId: string;
}

/** Shape accepted when fetching an event trigger. */
export interface WorkflowManagerGetTriggerInput {
  triggerId: string;
}

/** Shape accepted when updating a workflow schedule. */
export interface WorkflowManagerUpdateScheduleInput {
  scheduleId: string;
  providerName?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when updating an event trigger. */
export interface WorkflowManagerUpdateTriggerInput {
  triggerId: string;
  providerName?: string | undefined;
  match?: WorkflowEventMatchInput | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when deleting a workflow schedule. */
export interface WorkflowManagerDeleteScheduleInput {
  scheduleId: string;
}

/** Shape accepted when deleting an event trigger. */
export interface WorkflowManagerDeleteTriggerInput {
  triggerId: string;
}

/** Shape accepted when pausing a workflow schedule. */
export interface WorkflowManagerPauseScheduleInput {
  scheduleId: string;
}

/** Shape accepted when pausing an event trigger. */
export interface WorkflowManagerPauseTriggerInput {
  triggerId: string;
}

/** Shape accepted when resuming a workflow schedule. */
export interface WorkflowManagerResumeScheduleInput {
  scheduleId: string;
}

/** Shape accepted when resuming an event trigger. */
export interface WorkflowManagerResumeTriggerInput {
  triggerId: string;
}

/** Shape accepted when publishing a workflow event. */
export interface WorkflowManagerPublishEventInput {
  providerName?: string | undefined;
  event?: WorkflowEventInput | undefined;
}

export interface WorkflowManagerBoundRun {
  id?: string | undefined;
  status?: WorkflowRunStatus | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  trigger?: WorkflowRunTriggerInput | undefined;
  createdAt?: Date | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
  statusMessage?: string | undefined;
  resultBody?: string | undefined;
  createdBy?: WorkflowActorInput | undefined;
  executionRef?: string | undefined;
  workflowKey?: string | undefined;
}

export interface WorkflowManagerBoundDefinition {
  id?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  createdBy?: WorkflowActorInput | undefined;
  createdAt?: Date | undefined;
}

export interface WorkflowManagerBoundSchedule {
  id?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  paused?: boolean | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  nextRunAt?: Date | undefined;
  createdBy?: WorkflowActorInput | undefined;
  executionRef?: string | undefined;
}

export interface WorkflowManagerBoundEventTrigger {
  id?: string | undefined;
  match?: WorkflowEventMatchInput | undefined;
  target?: BoundWorkflowTargetInput | undefined;
  paused?: boolean | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  createdBy?: WorkflowActorInput | undefined;
  executionRef?: string | undefined;
}

export interface WorkflowManagerRun {
  providerName?: string | undefined;
  run?: WorkflowManagerBoundRun | undefined;
}

export interface WorkflowManagerRunSignal {
  providerName?: string | undefined;
  run?: WorkflowManagerBoundRun | undefined;
  signal?: WorkflowSignalInput | undefined;
  startedRun?: boolean | undefined;
  workflowKey?: string | undefined;
}

export interface WorkflowManagerDefinition {
  providerName?: string | undefined;
  definition?: WorkflowManagerBoundDefinition | undefined;
}

export interface WorkflowManagerSchedule {
  providerName?: string | undefined;
  schedule?: WorkflowManagerBoundSchedule | undefined;
}

export interface WorkflowManagerEventTrigger {
  providerName?: string | undefined;
  trigger?: WorkflowManagerBoundEventTrigger | undefined;
}

export type WorkflowManagerPublishedEvent = WorkflowEventInput;

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
    request: WorkflowManagerStartRunInput,
  ): Promise<WorkflowManagerRun> {
    return workflowManagerRunFromProto(await this.client.startRun({
      ...workflowManagerStartRunRequest(request),
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    }));
  }

  /** Signals an existing workflow run. */
  async signalRun(
    request: WorkflowManagerSignalRunInput,
  ): Promise<WorkflowManagerRunSignal> {
    return workflowManagerRunSignalFromProto(await this.client.signalRun({
      runId: request.runId,
      signal: request.signal === undefined ? undefined : workflowSignal(request.signal),
      invocationToken: this.invocationToken,
    }));
  }

  /** Signals a workflow run, or starts it when no run exists for the key. */
  async signalOrStartRun(
    request: WorkflowManagerSignalOrStartRunInput,
  ): Promise<WorkflowManagerRunSignal> {
    return workflowManagerRunSignalFromProto(await this.client.signalOrStartRun({
      ...workflowManagerSignalOrStartRunRequest(request),
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    }));
  }

  /** Creates a reusable workflow definition. */
  async createDefinition(
    request: WorkflowManagerCreateDefinitionInput,
  ): Promise<WorkflowManagerDefinition> {
    return workflowManagerDefinitionFromProto(await this.client.createDefinition({
      providerName: request.providerName ?? "",
      target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    }));
  }

  /** Fetches one workflow definition. */
  async getDefinition(
    request: WorkflowManagerGetDefinitionInput,
  ): Promise<WorkflowManagerDefinition> {
    return workflowManagerDefinitionFromProto(await this.client.getDefinition({
      definitionId: request.definitionId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Updates a workflow definition. */
  async updateDefinition(
    request: WorkflowManagerUpdateDefinitionInput,
  ): Promise<WorkflowManagerDefinition> {
    return workflowManagerDefinitionFromProto(await this.client.updateDefinition({
      definitionId: request.definitionId,
      providerName: request.providerName ?? "",
      target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
      invocationToken: this.invocationToken,
    }));
  }

  /** Deletes a workflow definition. */
  async deleteDefinition(
    request: WorkflowManagerDeleteDefinitionInput,
  ): Promise<void> {
    await this.client.deleteDefinition({
      definitionId: request.definitionId,
      invocationToken: this.invocationToken,
    });
  }

  /** Creates a workflow schedule. */
  async createSchedule(
    request: WorkflowManagerCreateScheduleInput,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(await this.client.createSchedule({
      ...workflowManagerCreateScheduleRequest(request),
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    }));
  }

  /** Fetches one workflow schedule. */
  async getSchedule(
    request: WorkflowManagerGetScheduleInput,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(await this.client.getSchedule({
      scheduleId: request.scheduleId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Updates a workflow schedule. */
  async updateSchedule(
    request: WorkflowManagerUpdateScheduleInput,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(await this.client.updateSchedule({
      ...workflowManagerUpdateScheduleRequest(request),
      invocationToken: this.invocationToken,
    }));
  }

  /** Deletes a workflow schedule. */
  async deleteSchedule(
    request: WorkflowManagerDeleteScheduleInput,
  ): Promise<void> {
    await this.client.deleteSchedule({
      scheduleId: request.scheduleId,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses a workflow schedule. */
  async pauseSchedule(
    request: WorkflowManagerPauseScheduleInput,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(await this.client.pauseSchedule({
      scheduleId: request.scheduleId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Resumes a workflow schedule. */
  async resumeSchedule(
    request: WorkflowManagerResumeScheduleInput,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(await this.client.resumeSchedule({
      scheduleId: request.scheduleId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Creates an event trigger. */
  async createTrigger(
    request: WorkflowManagerCreateTriggerInput,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(await this.client.createEventTrigger({
      ...workflowManagerCreateTriggerRequest(request),
      idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      invocationToken: this.invocationToken,
    }));
  }

  /** Fetches one event trigger. */
  async getTrigger(
    request: WorkflowManagerGetTriggerInput,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(await this.client.getEventTrigger({
      triggerId: request.triggerId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Updates an event trigger. */
  async updateTrigger(
    request: WorkflowManagerUpdateTriggerInput,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(await this.client.updateEventTrigger({
      ...workflowManagerUpdateTriggerRequest(request),
      invocationToken: this.invocationToken,
    }));
  }

  /** Deletes an event trigger. */
  async deleteTrigger(
    request: WorkflowManagerDeleteTriggerInput,
  ): Promise<void> {
    await this.client.deleteEventTrigger({
      triggerId: request.triggerId,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses an event trigger. */
  async pauseTrigger(
    request: WorkflowManagerPauseTriggerInput,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(await this.client.pauseEventTrigger({
      triggerId: request.triggerId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Resumes an event trigger. */
  async resumeTrigger(
    request: WorkflowManagerResumeTriggerInput,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(await this.client.resumeEventTrigger({
      triggerId: request.triggerId,
      invocationToken: this.invocationToken,
    }));
  }

  /** Publishes an event into the workflow manager. */
  async publishEvent(
    request: WorkflowManagerPublishEventInput,
  ): Promise<WorkflowManagerPublishedEvent> {
    const response = await this.client.publishEvent({
      providerName: request.providerName ?? "",
      event: request.event === undefined ? undefined : workflowEvent(request.event),
      invocationToken: this.invocationToken,
    });
    return workflowEventInputFromEvent(response)!;
  }
}

function workflowManagerStartRunRequest(request: WorkflowManagerStartRunInput) {
  return {
    providerName: request.providerName ?? "",
    target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
    idempotencyKey: request.idempotencyKey ?? "",
    workflowKey: request.workflowKey ?? "",
    definitionId: request.definitionId ?? "",
  };
}

function workflowManagerSignalOrStartRunRequest(
  request: WorkflowManagerSignalOrStartRunInput,
) {
  return {
    providerName: request.providerName ?? "",
    workflowKey: request.workflowKey ?? "",
    target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
    idempotencyKey: request.idempotencyKey ?? "",
    signal: request.signal === undefined ? undefined : workflowSignal(request.signal),
    definitionId: request.definitionId ?? "",
  };
}

function workflowManagerCreateScheduleRequest(
  request: WorkflowManagerCreateScheduleInput,
) {
  return {
    providerName: request.providerName ?? "",
    cron: request.cron ?? "",
    timezone: request.timezone ?? "",
    target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
    paused: request.paused ?? false,
    idempotencyKey: request.idempotencyKey ?? "",
    definitionId: request.definitionId ?? "",
  };
}

function workflowManagerUpdateScheduleRequest(
  request: WorkflowManagerUpdateScheduleInput,
) {
  return {
    scheduleId: request.scheduleId,
    providerName: request.providerName ?? "",
    cron: request.cron ?? "",
    timezone: request.timezone ?? "",
    target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
    paused: request.paused ?? false,
    definitionId: request.definitionId ?? "",
  };
}

function workflowManagerCreateTriggerRequest(
  request: WorkflowManagerCreateTriggerInput,
) {
  return {
    providerName: request.providerName ?? "",
    match: request.match === undefined ? undefined : workflowEventMatch(request.match),
    target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
    paused: request.paused ?? false,
    idempotencyKey: request.idempotencyKey ?? "",
    definitionId: request.definitionId ?? "",
  };
}

function workflowManagerUpdateTriggerRequest(
  request: WorkflowManagerUpdateTriggerInput,
) {
  return {
    triggerId: request.triggerId,
    providerName: request.providerName ?? "",
    match: request.match === undefined ? undefined : workflowEventMatch(request.match),
    target: request.target === undefined ? undefined : boundWorkflowTarget(request.target),
    paused: request.paused ?? false,
    definitionId: request.definitionId ?? "",
  };
}

function workflowManagerRunFromProto(input: ManagedWorkflowRun): WorkflowManagerRun {
  return {
    providerName: input.providerName,
    run: workflowManagerBoundRunFromRun(input.run),
  };
}

function workflowManagerRunSignalFromProto(
  input: ManagedWorkflowRunSignal,
): WorkflowManagerRunSignal {
  return {
    providerName: input.providerName,
    run: workflowManagerBoundRunFromRun(input.run),
    signal: workflowSignalInputFromSignal(input.signal),
    startedRun: input.startedRun,
    workflowKey: input.workflowKey,
  };
}

function workflowManagerDefinitionFromProto(
  input: ManagedWorkflowDefinition,
): WorkflowManagerDefinition {
  return {
    providerName: input.providerName,
    definition: workflowManagerBoundDefinitionFromDefinition(input.definition),
  };
}

function workflowManagerScheduleFromProto(
  input: ManagedWorkflowSchedule,
): WorkflowManagerSchedule {
  return {
    providerName: input.providerName,
    schedule: workflowManagerBoundScheduleFromSchedule(input.schedule),
  };
}

function workflowManagerEventTriggerFromProto(
  input: ManagedWorkflowEventTrigger,
): WorkflowManagerEventTrigger {
  return {
    providerName: input.providerName,
    trigger: workflowManagerBoundEventTriggerFromTrigger(input.trigger),
  };
}

function workflowManagerBoundRunFromRun(
  input?: BoundWorkflowRun,
): WorkflowManagerBoundRun | undefined {
  const value: BoundWorkflowRunInput | undefined = boundWorkflowRunInputFromRun(input);
  return value === undefined
    ? undefined
    : {
      id: value.id,
      status: value.status,
      target: value.target as BoundWorkflowTargetInput | undefined,
      trigger: value.trigger as WorkflowRunTriggerInput | undefined,
      createdAt: value.createdAt as Date | undefined,
      startedAt: value.startedAt as Date | undefined,
      completedAt: value.completedAt as Date | undefined,
      statusMessage: value.statusMessage,
      resultBody: value.resultBody,
      createdBy: value.createdBy as WorkflowActorInput | undefined,
      executionRef: value.executionRef,
      workflowKey: value.workflowKey,
    };
}

function workflowManagerBoundDefinitionFromDefinition(
  input?: BoundWorkflowDefinition,
): WorkflowManagerBoundDefinition | undefined {
  const value = boundWorkflowDefinitionInputFromDefinition(input);
  return value === undefined
    ? undefined
    : {
      id: value.id,
      target: value.target as BoundWorkflowTargetInput | undefined,
      createdBy: value.createdBy as WorkflowActorInput | undefined,
      createdAt: value.createdAt as Date | undefined,
    };
}

function workflowManagerBoundScheduleFromSchedule(
  input?: BoundWorkflowSchedule,
): WorkflowManagerBoundSchedule | undefined {
  const value = boundWorkflowScheduleInputFromSchedule(input);
  return value === undefined
    ? undefined
    : {
      id: value.id,
      cron: value.cron,
      timezone: value.timezone,
      target: value.target as BoundWorkflowTargetInput | undefined,
      paused: value.paused,
      createdAt: value.createdAt as Date | undefined,
      updatedAt: value.updatedAt as Date | undefined,
      nextRunAt: value.nextRunAt as Date | undefined,
      createdBy: value.createdBy as WorkflowActorInput | undefined,
      executionRef: value.executionRef,
    };
}

function workflowManagerBoundEventTriggerFromTrigger(
  input?: BoundWorkflowEventTrigger,
): WorkflowManagerBoundEventTrigger | undefined {
  const value = boundWorkflowEventTriggerInputFromTrigger(input);
  return value === undefined
    ? undefined
    : {
      id: value.id,
      match: value.match,
      target: value.target as BoundWorkflowTargetInput | undefined,
      paused: value.paused,
      createdAt: value.createdAt as Date | undefined,
      updatedAt: value.updatedAt as Date | undefined,
      createdBy: value.createdBy as WorkflowActorInput | undefined,
      executionRef: value.executionRef,
    };
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
