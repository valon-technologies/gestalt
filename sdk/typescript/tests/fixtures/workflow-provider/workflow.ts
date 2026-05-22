import {
  WorkflowRunStatus,
  boundWorkflowDefinition,
  boundWorkflowEventTrigger,
  boundWorkflowRun,
  boundWorkflowSchedule,
  boundWorkflowTarget,
  defineWorkflowProvider,
  workflowEvent,
  type BoundWorkflowDefinition,
  type BoundWorkflowTarget,
  type DeleteWorkflowProviderDefinitionRequest,
  type DeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest,
  type GetWorkflowProviderDefinitionRequest,
  type GetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest,
  type PauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest,
  type ResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest,
  type StartWorkflowProviderRunRequest,
  type UpdateWorkflowProviderDefinitionRequest,
  type UpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest,
  type PublishWorkflowProviderEventRequest,
} from "../../../src/index.ts";

const runs = new Map<string, ReturnType<typeof createRun>>();
const schedules = new Map<string, ReturnType<typeof createSchedule>>();
const triggers = new Map<string, ReturnType<typeof createTrigger>>();
const definitions = new Map<string, BoundWorkflowDefinition>();
let publishCount = 0;

function pluginTarget(pluginName: string, operation: string): BoundWorkflowTarget {
  return boundWorkflowTarget({ plugin: { pluginName, operation } });
}

export const provider = defineWorkflowProvider({
  displayName: "Fixture Workflow",
  description: "Workflow provider fixture used by SDK tests",
  configure() {
    runs.clear();
    schedules.clear();
    triggers.clear();
    definitions.clear();
    publishCount = 0;
  },
  async createDefinition(request) {
    const definition = boundWorkflowDefinition({
      id: request.idempotencyKey || `definition:${definitions.size + 1}`,
      target: request.target,
    });
    definitions.set(definition.id ?? "", definition);
    return definition;
  },
  async getDefinition(request) {
    return requireDefinition(request);
  },
  async updateDefinition(request) {
    const existing = requireDefinition(request);
    const definition = boundWorkflowDefinition({
      ...existing,
      target: request.target,
    });
    definitions.set(request.definitionId, definition);
    return definition;
  },
  async deleteDefinition(request) {
    if (!definitions.delete(request.definitionId)) {
      throw new Error(`unknown definition ${request.definitionId}`);
    }
  },
  async startRun(request) {
    const plugin =
      request.target?.kind?.case === "plugin"
        ? request.target.kind.value
        : undefined;
    const run = createRun(
      `${plugin?.pluginName ?? "plugin"}:${plugin?.operation ?? "operation"}:${runs.size + 1}`,
      request,
      WorkflowRunStatus.PENDING,
      request.idempotencyKey ? `idempotency:${request.idempotencyKey}` : "",
    );
    runs.set(run.id ?? "", run);
    return run;
  },
  async getRun(request) {
    return requireRun(request);
  },
  async listRuns() {
    return [...runs.values()];
  },
  async cancelRun(request) {
    const run = requireRunByID(request.runId);
    const updated = boundWorkflowRun({
      id: run.id,
      status: WorkflowRunStatus.CANCELED,
      statusMessage: request.reason,
      ...(run.target ? { target: run.target } : {}),
      ...(run.trigger ? { trigger: run.trigger } : {}),
      ...(run.createdAt ? { createdAt: run.createdAt } : {}),
      ...(run.startedAt ? { startedAt: run.startedAt } : {}),
      ...(run.completedAt ? { completedAt: run.completedAt } : {}),
      ...(run.resultBody ? { resultBody: run.resultBody } : {}),
    });
    runs.set(updated.id ?? "", updated);
    return updated;
  },
  async signalRun(request) {
    const run = requireRunByID(request.runId);
    return {
      run,
      signal: request.signal,
      startedRun: false,
      workflowKey: run.workflowKey ?? "",
    };
  },
  async signalOrStartRun(request) {
    const run = boundWorkflowRun({
      id: `${request.workflowKey || "workflow"}:${runs.size + 1}`,
      status: WorkflowRunStatus.PENDING,
      target: request.target,
      createdBy: request.createdBy,
      executionRef: request.executionRef,
      workflowKey: request.workflowKey,
    });
    runs.set(run.id ?? "", run);
    return {
      run,
      signal: request.signal,
      startedRun: true,
      workflowKey: request.workflowKey ?? "",
    };
  },
  async upsertSchedule(request) {
    const existing = schedules.get(scheduleKey(request));
    const schedule = createSchedule(request, existing);
    schedules.set(scheduleKey(request), schedule);
    return schedule;
  },
  async getSchedule(request) {
    return requireSchedule(request);
  },
  async listSchedules() {
    return [...schedules.values()];
  },
  async deleteSchedule(request) {
    if (!schedules.delete(request.scheduleId)) {
      throw new Error(`unknown schedule ${request.scheduleId}`);
    }
  },
  async pauseSchedule(request) {
    return updateSchedule(request, true);
  },
  async resumeSchedule(request) {
    return updateSchedule(request, false);
  },
  async upsertEventTrigger(request) {
    const existing = triggers.get(triggerKey(request));
    const trigger = createTrigger(request, existing);
    triggers.set(triggerKey(request), trigger);
    return trigger;
  },
  async getEventTrigger(request) {
    return requireTrigger(request);
  },
  async listEventTriggers() {
    return [...triggers.values()];
  },
  async deleteEventTrigger(request) {
    if (!triggers.delete(request.triggerId)) {
      throw new Error(`unknown trigger ${request.triggerId}`);
    }
  },
  async pauseEventTrigger(request) {
    return updateTrigger(request, true);
  },
  async resumeEventTrigger(request) {
    return updateTrigger(request, false);
  },
  async publishEvent(request: PublishWorkflowProviderEventRequest) {
    publishCount += 1;
    const triggerId = publishedTriggerID(request.pluginName);
    const existing = triggers.get(triggerId);
    const trigger = boundWorkflowEventTrigger({
      id: triggerId,
      ...(existing?.match ? { match: existing.match } : {}),
      target: existing?.target ?? pluginTarget(request.pluginName, "published"),
      paused: false,
    });
    triggers.set(triggerId, trigger);
    return workflowEvent({
      id: `published:${publishCount}`,
      type: request.event?.type ?? "",
      source: request.event?.source ?? "",
    });
  },
  warnings() {
    return publishCount > 0 ? [`published-events:${publishCount}`] : [];
  },
});

function scheduleKey(request: UpsertWorkflowProviderScheduleRequest): string {
  return request.scheduleId;
}

function requireRun(request: GetWorkflowProviderRunRequest) {
  return requireRunByID(request.runId);
}

function requireDefinition(
  request: GetWorkflowProviderDefinitionRequest | UpdateWorkflowProviderDefinitionRequest | DeleteWorkflowProviderDefinitionRequest,
) {
  const definition = definitions.get(request.definitionId);
  if (!definition) {
    throw new Error(`unknown definition ${request.definitionId}`);
  }
  return definition;
}

function requireSchedule(request: GetWorkflowProviderScheduleRequest) {
  const schedule = schedules.get(request.scheduleId);
  if (!schedule) {
    throw new Error(`unknown schedule ${request.scheduleId}`);
  }
  return schedule;
}

function requireTrigger(request: GetWorkflowProviderEventTriggerRequest) {
  const trigger = triggers.get(request.triggerId);
  if (!trigger) {
    throw new Error(`unknown trigger ${request.triggerId}`);
  }
  return trigger;
}

function updateSchedule(
  request:
    | PauseWorkflowProviderScheduleRequest
    | ResumeWorkflowProviderScheduleRequest,
  paused: boolean,
) {
  const schedule = schedules.get(request.scheduleId);
  if (!schedule) {
    throw new Error(`unknown schedule ${request.scheduleId}`);
  }
  const updated = boundWorkflowSchedule({
    id: schedule.id,
    cron: schedule.cron,
    timezone: schedule.timezone,
    paused,
    ...(schedule.createdBy ? { createdBy: schedule.createdBy } : {}),
    ...(schedule.target ? { target: schedule.target } : {}),
    ...(schedule.createdAt ? { createdAt: schedule.createdAt } : {}),
    ...(schedule.updatedAt ? { updatedAt: schedule.updatedAt } : {}),
    ...(schedule.nextRunAt ? { nextRunAt: schedule.nextRunAt } : {}),
  });
  schedules.set(request.scheduleId, updated);
  return updated;
}

function updateTrigger(
  request:
    | PauseWorkflowProviderEventTriggerRequest
    | ResumeWorkflowProviderEventTriggerRequest,
  paused: boolean,
) {
  const trigger = triggers.get(request.triggerId);
  if (!trigger) {
    throw new Error(`unknown trigger ${request.triggerId}`);
  }
  const updated = boundWorkflowEventTrigger({
    id: trigger.id,
    paused,
    ...(trigger.createdBy ? { createdBy: trigger.createdBy } : {}),
    ...(trigger.match ? { match: trigger.match } : {}),
    ...(trigger.target ? { target: trigger.target } : {}),
    ...(trigger.createdAt ? { createdAt: trigger.createdAt } : {}),
    ...(trigger.updatedAt ? { updatedAt: trigger.updatedAt } : {}),
  });
  triggers.set(request.triggerId, updated);
  return updated;
}

function triggerKey(request: UpsertWorkflowProviderEventTriggerRequest): string {
  return request.triggerId;
}

function publishedTriggerID(pluginName: string): string {
  return `published:${pluginName}`;
}

function requireRunByID(runId: string) {
  const run = runs.get(runId);
  if (!run) {
    throw new Error(`unknown run ${runId}`);
  }
  return run;
}

function createRun(
  id: string,
  request: StartWorkflowProviderRunRequest,
  status: WorkflowRunStatus,
  statusMessage: string,
) {
  return boundWorkflowRun({
    id,
    status,
    statusMessage,
    ...(request.createdBy ? { createdBy: request.createdBy } : {}),
    ...(request.target ? { target: request.target } : {}),
  });
}

function createSchedule(
  request: UpsertWorkflowProviderScheduleRequest,
  existing?: { createdBy?: UpsertWorkflowProviderScheduleRequest["requestedBy"] },
) {
  return boundWorkflowSchedule({
    id: request.scheduleId,
    cron: request.cron,
    timezone: request.timezone,
    paused: request.paused,
    ...(existing?.createdBy
      ? { createdBy: existing.createdBy }
      : request.requestedBy
        ? { createdBy: request.requestedBy }
        : {}),
    ...(request.target ? { target: request.target } : {}),
  });
}

function createTrigger(
  request: UpsertWorkflowProviderEventTriggerRequest,
  existing?: { createdBy?: UpsertWorkflowProviderEventTriggerRequest["requestedBy"] },
) {
  return boundWorkflowEventTrigger({
    id: request.triggerId,
    paused: request.paused,
    ...(existing?.createdBy
      ? { createdBy: existing.createdBy }
      : request.requestedBy
        ? { createdBy: request.requestedBy }
        : {}),
    ...(request.match ? { match: request.match } : {}),
    ...(request.target ? { target: request.target } : {}),
  });
}
