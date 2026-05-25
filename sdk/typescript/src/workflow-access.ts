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
  workflowDefinitionFromProto,
  workflowEventTriggerFromProto,
  workflowRunFromProto,
  workflowRunSignalFromProto,
  workflowScheduleFromProto,
  workflowEventFromProto,
  workflowEventMatchToProto,
  workflowEventToProto,
  workflowSignalToProto,
  type BoundWorkflowTarget,
  type WorkflowDefinition,
  type WorkflowEventTrigger,
  type WorkflowRun,
  type WorkflowRunSignal,
  type WorkflowSchedule,
  type WorkflowEvent,
  type WorkflowEventMatch,
  type WorkflowSignal,
} from "./workflow.ts";

/**
 * Environment variable containing the gestaltd workflow-provider facade target.
 *
 * Workflow calls call the facade. Provider runtimes still listen on
 * GESTALT_PROVIDER_SOCKET.
 */
/** Shape accepted when starting a workflow run. */
export interface WorkflowStartRun {
  providerName: string;
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
  workflowKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when signaling an existing workflow run. */
export interface WorkflowSignalRun {
  runId: string;
  signal?: WorkflowSignal | undefined;
}

/** Shape accepted when signaling a run or starting it if missing. */
export interface WorkflowSignalOrStartRun {
  providerName: string;
  workflowKey: string;
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
  signal?: WorkflowSignal | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating a workflow definition. */
export interface WorkflowCreateDefinition {
  providerName: string;
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey?: string | undefined;
}

/** Shape accepted when fetching a workflow definition. */
export interface WorkflowGetDefinition {
  definitionId: string;
}

/** Shape accepted when updating a workflow definition. */
export interface WorkflowUpdateDefinition {
  definitionId: string;
  providerName?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
}

/** Shape accepted when deleting a workflow definition. */
export interface WorkflowDeleteDefinition {
  definitionId: string;
}

/** Shape accepted when creating a workflow schedule. */
export interface WorkflowCreateSchedule {
  providerName: string;
  cron: string;
  timezone?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when creating an event trigger. */
export interface WorkflowCreateTrigger {
  providerName: string;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when fetching a workflow schedule. */
export interface WorkflowGetSchedule {
  scheduleId: string;
}

/** Shape accepted when fetching an event trigger. */
export interface WorkflowGetTrigger {
  triggerId: string;
}

/** Shape accepted when updating a workflow schedule. */
export interface WorkflowUpdateSchedule {
  scheduleId: string;
  providerName?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when updating an event trigger. */
export interface WorkflowUpdateTrigger {
  triggerId: string;
  providerName?: string | undefined;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  definitionId?: string | undefined;
}

/** Shape accepted when deleting a workflow schedule. */
export interface WorkflowDeleteSchedule {
  scheduleId: string;
}

/** Shape accepted when deleting an event trigger. */
export interface WorkflowDeleteTrigger {
  triggerId: string;
}

/** Shape accepted when pausing a workflow schedule. */
export interface WorkflowPauseSchedule {
  scheduleId: string;
}

/** Shape accepted when pausing an event trigger. */
export interface WorkflowPauseTrigger {
  triggerId: string;
}

/** Shape accepted when resuming a workflow schedule. */
export interface WorkflowResumeSchedule {
  scheduleId: string;
}

/** Shape accepted when resuming an event trigger. */
export interface WorkflowResumeTrigger {
  triggerId: string;
}

/** Shape accepted when publishing a workflow event. */
export interface WorkflowPublishEvent {
  event?: WorkflowEvent | undefined;
  providerName?: string | undefined;
}

/** Fakeable client contract for workflow calls. */
export interface Workflow {
  startRun(request: WorkflowStartRun): Promise<WorkflowRun>;
  signalRun(
    request: WorkflowSignalRun,
  ): Promise<WorkflowRunSignal>;
  signalOrStartRun(
    request: WorkflowSignalOrStartRun,
  ): Promise<WorkflowRunSignal>;
  createDefinition(
    request: WorkflowCreateDefinition,
  ): Promise<WorkflowDefinition>;
  getDefinition(
    request: WorkflowGetDefinition,
  ): Promise<WorkflowDefinition>;
  updateDefinition(
    request: WorkflowUpdateDefinition,
  ): Promise<WorkflowDefinition>;
  deleteDefinition(request: WorkflowDeleteDefinition): Promise<void>;
  createSchedule(
    request: WorkflowCreateSchedule,
  ): Promise<WorkflowSchedule>;
  getSchedule(
    request: WorkflowGetSchedule,
  ): Promise<WorkflowSchedule>;
  updateSchedule(
    request: WorkflowUpdateSchedule,
  ): Promise<WorkflowSchedule>;
  deleteSchedule(request: WorkflowDeleteSchedule): Promise<void>;
  pauseSchedule(
    request: WorkflowPauseSchedule,
  ): Promise<WorkflowSchedule>;
  resumeSchedule(
    request: WorkflowResumeSchedule,
  ): Promise<WorkflowSchedule>;
  createTrigger(
    request: WorkflowCreateTrigger,
  ): Promise<WorkflowEventTrigger>;
  getTrigger(
    request: WorkflowGetTrigger,
  ): Promise<WorkflowEventTrigger>;
  updateTrigger(
    request: WorkflowUpdateTrigger,
  ): Promise<WorkflowEventTrigger>;
  deleteTrigger(request: WorkflowDeleteTrigger): Promise<void>;
  pauseTrigger(
    request: WorkflowPauseTrigger,
  ): Promise<WorkflowEventTrigger>;
  resumeTrigger(
    request: WorkflowResumeTrigger,
  ): Promise<WorkflowEventTrigger>;
  publishEvent(request: WorkflowPublishEvent): Promise<WorkflowEvent>;
}

/**
 * Client for creating and controlling workflow schedules and event triggers.
 *
 * The constructor accepts either a Gestalt request or an invocation token. Each
 * agent call forwards that token to the workflow-provider facade. When
 * constructed from a request, create operations reuse the request idempotency
 * key unless the call provides one explicitly.
 */
class WorkflowImpl implements Workflow {
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
        `workflow: ${ENV_HOST_SERVICE_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";

    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("workflow", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(WorkflowProviderService, transport);
  }

  /** Starts a workflow run immediately. */
  async startRun(
    request: WorkflowStartRun,
  ): Promise<WorkflowRun> {
    return workflowRunFromProto(
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
    request: WorkflowSignalRun,
  ): Promise<WorkflowRunSignal> {
    return workflowRunSignalFromProto(
      await this.client.signalRun({
        runId: request.runId,
        signal: workflowSignalToProto(request.signal),
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Signals a workflow run, or starts it when no run exists for the key. */
  async signalOrStartRun(
    request: WorkflowSignalOrStartRun,
  ): Promise<WorkflowRunSignal> {
    return workflowRunSignalFromProto(
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
    request: WorkflowCreateDefinition,
  ): Promise<WorkflowDefinition> {
    return workflowDefinitionFromProto(
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
    request: WorkflowGetDefinition,
  ): Promise<WorkflowDefinition> {
    return workflowDefinitionFromProto(
      await this.client.getDefinition({
        definitionId: request.definitionId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates a workflow definition. */
  async updateDefinition(
    request: WorkflowUpdateDefinition,
  ): Promise<WorkflowDefinition> {
    return workflowDefinitionFromProto(
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
    request: WorkflowDeleteDefinition,
  ): Promise<void> {
    await this.client.deleteDefinition({
      definitionId: request.definitionId,
      invocationToken: this.invocationToken,
    });
  }

  /** Creates a workflow schedule. */
  async createSchedule(
    request: WorkflowCreateSchedule,
  ): Promise<WorkflowSchedule> {
    return workflowScheduleFromProto(
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
    request: WorkflowGetSchedule,
  ): Promise<WorkflowSchedule> {
    return workflowScheduleFromProto(
      await this.client.getSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates a workflow schedule. */
  async updateSchedule(
    request: WorkflowUpdateSchedule,
  ): Promise<WorkflowSchedule> {
    return workflowScheduleFromProto(
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
    request: WorkflowDeleteSchedule,
  ): Promise<void> {
    await this.client.deleteSchedule({
      scheduleId: request.scheduleId,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses a workflow schedule. */
  async pauseSchedule(
    request: WorkflowPauseSchedule,
  ): Promise<WorkflowSchedule> {
    return workflowScheduleFromProto(
      await this.client.pauseSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Resumes a workflow schedule. */
  async resumeSchedule(
    request: WorkflowResumeSchedule,
  ): Promise<WorkflowSchedule> {
    return workflowScheduleFromProto(
      await this.client.resumeSchedule({
        scheduleId: request.scheduleId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Creates an event trigger. */
  async createTrigger(
    request: WorkflowCreateTrigger,
  ): Promise<WorkflowEventTrigger> {
    return workflowEventTriggerFromProto(
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
    request: WorkflowGetTrigger,
  ): Promise<WorkflowEventTrigger> {
    return workflowEventTriggerFromProto(
      await this.client.getEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Updates an event trigger. */
  async updateTrigger(
    request: WorkflowUpdateTrigger,
  ): Promise<WorkflowEventTrigger> {
    return workflowEventTriggerFromProto(
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
    request: WorkflowDeleteTrigger,
  ): Promise<void> {
    await this.client.deleteEventTrigger({
      triggerId: request.triggerId,
      invocationToken: this.invocationToken,
    });
  }

  /** Pauses an event trigger. */
  async pauseTrigger(
    request: WorkflowPauseTrigger,
  ): Promise<WorkflowEventTrigger> {
    return workflowEventTriggerFromProto(
      await this.client.pauseEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Resumes an event trigger. */
  async resumeTrigger(
    request: WorkflowResumeTrigger,
  ): Promise<WorkflowEventTrigger> {
    return workflowEventTriggerFromProto(
      await this.client.resumeEventTrigger({
        triggerId: request.triggerId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  /** Publishes an event into the workflow. */
  async publishEvent(
    request: WorkflowPublishEvent,
  ): Promise<WorkflowEvent> {
    const event = workflowEventFromProto(
      await this.client.publishEvent({
        event: workflowEventToProto(request.event),
        invocationToken: this.invocationToken,
        providerName: request.providerName ?? "",
      }),
    );
    if (event === undefined) {
      throw new Error("Workflow.publishEvent returned no event");
    }
    return event;
  }
}

export const Workflow = WorkflowImpl;

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("workflow: invocation token is not available");
  }
  return trimmed;
}

function normalizeIdempotencyKey(requestOrToken: Request | string): string {
  if (typeof requestOrToken === "string") {
    return "";
  }
  return requestOrToken.idempotencyKey.trim();
}
