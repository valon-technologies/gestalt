import { create } from "@bufbuild/protobuf";

import {
  BoundWorkflowEventTriggerSchema,
  BoundWorkflowRunSchema,
  BoundWorkflowScheduleSchema,
  type DeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest,
  type GetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest,
  type PauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest,
  type ResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest,
  type StartWorkflowProviderRunRequest,
  type UpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest,
  WorkflowRunStatus,
} from "../../../src/internal/gen/v1/workflow_pb.ts";
import {
  defineWorkflowProvider,
  type PublishWorkflowProviderEventRequest,
} from "../../../src/index.ts";

const runs = new Map<string, ReturnType<typeof createRun>>();
const schedules = new Map<string, ReturnType<typeof createSchedule>>();
const triggers = new Map<string, ReturnType<typeof createTrigger>>();
let publishCount = 0;

function pluginTarget(pluginName: string, operation: string) {
  return {
    kind: {
      case: "plugin" as const,
      value: { pluginName, operation },
    },
  };
}

export const provider = defineWorkflowProvider({
  displayName: "Fixture Workflow",
  description: "Workflow provider fixture used by SDK tests",
  configure() {
    runs.clear();
    schedules.clear();
    triggers.clear();
    publishCount = 0;
  },
  async startRun(request) {
    const plugin =
      request.target?.kind.case === "plugin"
        ? request.target.kind.value
        : undefined;
    const run = createRun(
      `${plugin?.pluginName ?? "plugin"}:${plugin?.operation ?? "operation"}:${runs.size + 1}`,
      request,
      WorkflowRunStatus.PENDING,
      request.idempotencyKey ? `idempotency:${request.idempotencyKey}` : "",
    );
    runs.set(run.id, run);
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
    const updated = create(BoundWorkflowRunSchema, {
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
    runs.set(updated.id, updated);
    return updated;
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
    const trigger = create(BoundWorkflowEventTriggerSchema, {
      id: triggerId,
      ...(existing?.match ? { match: existing.match } : {}),
      target: existing?.target ?? pluginTarget(request.pluginName, "published"),
      paused: false,
    });
    triggers.set(triggerId, trigger);
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
  const updated = create(BoundWorkflowScheduleSchema, {
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
  const updated = create(BoundWorkflowEventTriggerSchema, {
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
  return create(BoundWorkflowRunSchema, {
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
  return create(BoundWorkflowScheduleSchema, {
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
  return create(BoundWorkflowEventTriggerSchema, {
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
