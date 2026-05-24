import {
  createClient,
  type Client,
} from "@connectrpc/connect";

import {
  WorkflowProvider as WorkflowProviderService,
} from "./internal/gen/v1/workflow_pb.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";
import type { Request } from "./api.ts";
import {
  boundWorkflowTargetToProto,
  workflowManagerDefinitionFromProto,
  workflowManagerEventTriggerFromProto,
  workflowManagerRunFromProto,
  workflowManagerRunSignalFromProto,
  workflowManagerScheduleFromProto,
  workflowEventFromProto,
  workflowEventMatchToProto,
  workflowEventToProto,
  workflowSignalToProto,
  type BoundWorkflowTarget,
  type WorkflowManagerDefinition,
  type WorkflowManagerEventTrigger,
  type WorkflowManagerRun,
  type WorkflowManagerRunSignal,
  type WorkflowManagerSchedule,
  type WorkflowEvent,
  type WorkflowEventMatch,
  type WorkflowSignal,
} from "./workflow.ts";

/**
 * Environment variable containing the gestaltd workflow-provider facade target.
 *
 * Manager clients call the facade. Provider runtimes still listen on
 * GESTALT_PROVIDER_SOCKET.
 */
/** Shape accepted when starting a workflow run. */
export interface WorkflowManagerStartRun {
  providerName: string;
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
  workflowKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when signaling an existing workflow run. */
export interface WorkflowManagerSignalRun {
  runId: string;
  signal?: WorkflowSignal | undefined;
}

/** Shape accepted when signaling a run or starting it if missing. */
export interface WorkflowManagerSignalOrStartRun {
  providerName: string;
  workflowKey: string;
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
  signal?: WorkflowSignal | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating a workflow definition. */
export interface WorkflowManagerCreateDefinition {
  providerName: string;
  target?: BoundWorkflowTarget | undefined;
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
  target?: BoundWorkflowTarget | undefined;
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
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating an event trigger. */
export interface WorkflowManagerCreateTrigger {
  providerName: string;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
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
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when updating an event trigger. */
export interface WorkflowManagerUpdateTrigger {
  triggerId: string;
  providerName?: string | undefined;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
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
  event?: WorkflowEvent | undefined;
  providerName?: string | undefined;
}

/** Fakeable client contract for workflow manager calls. */
export interface WorkflowManager {
  startRun(request: WorkflowManagerStartRun): Promise<WorkflowManagerRun>;
  signalRun(
    request: WorkflowManagerSignalRun,
  ): Promise<WorkflowManagerRunSignal>;
  signalOrStartRun(
    request: WorkflowManagerSignalOrStartRun,
  ): Promise<WorkflowManagerRunSignal>;
  createDefinition(
    request: WorkflowManagerCreateDefinition,
  ): Promise<WorkflowManagerDefinition>;
  getDefinition(
    request: WorkflowManagerGetDefinition,
  ): Promise<WorkflowManagerDefinition>;
  updateDefinition(
    request: WorkflowManagerUpdateDefinition,
  ): Promise<WorkflowManagerDefinition>;
  deleteDefinition(request: WorkflowManagerDeleteDefinition): Promise<void>;
  createSchedule(
    request: WorkflowManagerCreateSchedule,
  ): Promise<WorkflowManagerSchedule>;
  getSchedule(
    request: WorkflowManagerGetSchedule,
  ): Promise<WorkflowManagerSchedule>;
  updateSchedule(
    request: WorkflowManagerUpdateSchedule,
  ): Promise<WorkflowManagerSchedule>;
  deleteSchedule(request: WorkflowManagerDeleteSchedule): Promise<void>;
  pauseSchedule(
    request: WorkflowManagerPauseSchedule,
  ): Promise<WorkflowManagerSchedule>;
  resumeSchedule(
    request: WorkflowManagerResumeSchedule,
  ): Promise<WorkflowManagerSchedule>;
  createTrigger(
    request: WorkflowManagerCreateTrigger,
  ): Promise<WorkflowManagerEventTrigger>;
  getTrigger(
    request: WorkflowManagerGetTrigger,
  ): Promise<WorkflowManagerEventTrigger>;
  updateTrigger(
    request: WorkflowManagerUpdateTrigger,
  ): Promise<WorkflowManagerEventTrigger>;
  deleteTrigger(request: WorkflowManagerDeleteTrigger): Promise<void>;
  pauseTrigger(
    request: WorkflowManagerPauseTrigger,
  ): Promise<WorkflowManagerEventTrigger>;
  resumeTrigger(
    request: WorkflowManagerResumeTrigger,
  ): Promise<WorkflowManagerEventTrigger>;
  publishEvent(request: WorkflowManagerPublishEvent): Promise<WorkflowEvent>;
}

/**
 * Client for creating and controlling workflow schedules and event triggers.
 *
 * The constructor accepts either a Gestalt request or an invocation token. Each
 * manager call forwards that token to the workflow-provider facade. When
 * constructed from a request, create operations reuse the request idempotency
 * key unless the call provides one explicitly.
 */
class HostWorkflowManager implements WorkflowManager {
  private readonly client: Client<typeof WorkflowProviderService>;
  private readonly invocationToken: string;
  private readonly idempotencyKey: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);
    this.idempotencyKey = normalizeIdempotencyKey(requestOrToken);

    const target = process.env[ENV_HOST_SERVICE_SOCKET]?.trim();
    if (!target) {
      throw new Error(
        `workflow manager: ${ENV_HOST_SERVICE_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";

    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("workflow manager", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(WorkflowProviderService, transport);
  }

  /** Starts a workflow run immediately. */
  async startRun(
    request: WorkflowManagerStartRun,
  ): Promise<WorkflowManagerRun> {
    return workflowManagerRunFromProto(
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
  ): Promise<WorkflowManagerRunSignal> {
    return workflowManagerRunSignalFromProto(
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
  ): Promise<WorkflowManagerRunSignal> {
    return workflowManagerRunSignalFromProto(
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
  ): Promise<WorkflowManagerDefinition> {
    return workflowManagerDefinitionFromProto(
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
  ): Promise<WorkflowManagerDefinition> {
    return workflowManagerDefinitionFromProto(
      await this.client.getDefinition({
        definitionId: request.definitionId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates a workflow definition. */
  async updateDefinition(
    request: WorkflowManagerUpdateDefinition,
  ): Promise<WorkflowManagerDefinition> {
    return workflowManagerDefinitionFromProto(
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
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(
      await this.client.upsertSchedule({
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
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(
      await this.client.getSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates a workflow schedule. */
  async updateSchedule(
    request: WorkflowManagerUpdateSchedule,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(
      await this.client.upsertSchedule({
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
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(
      await this.client.pauseSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Resumes a workflow schedule. */
  async resumeSchedule(
    request: WorkflowManagerResumeSchedule,
  ): Promise<WorkflowManagerSchedule> {
    return workflowManagerScheduleFromProto(
      await this.client.resumeSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Creates an event trigger. */
  async createTrigger(
    request: WorkflowManagerCreateTrigger,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(
      await this.client.upsertEventTrigger({
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
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(
      await this.client.getEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates an event trigger. */
  async updateTrigger(
    request: WorkflowManagerUpdateTrigger,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(
      await this.client.upsertEventTrigger({
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
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(
      await this.client.pauseEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Resumes an event trigger. */
  async resumeTrigger(
    request: WorkflowManagerResumeTrigger,
  ): Promise<WorkflowManagerEventTrigger> {
    return workflowManagerEventTriggerFromProto(
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

export const WorkflowManager = HostWorkflowManager;

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
