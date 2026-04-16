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
} from "../../../gen/v1/workflow_pb.ts";
import {
  defineWorkflowProvider,
  type PublishWorkflowProviderEventRequest,
} from "../../../src/index.ts";

const runs = new Map<string, ReturnType<typeof createRun>>();
const schedules = new Map<string, ReturnType<typeof createSchedule>>();
const triggers = new Map<string, ReturnType<typeof createTrigger>>();
let publishCount = 0;

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
    const run = createRun(
      `${request.target?.pluginName ?? "plugin"}:${request.target?.operation ?? "operation"}:${runs.size + 1}`,
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
  async listRuns(request) {
    return [...runs.values()].filter(
      (run) => run.target?.pluginName === request.pluginName,
    );
  },
  async cancelRun(request) {
    const run = requireRunByID(request.pluginName, request.runId);
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
  async listSchedules(request) {
    return [...schedules.values()].filter(
      (schedule) => schedule.target?.pluginName === request.pluginName,
    );
  },
  async deleteSchedule(request) {
    if (!schedules.delete(scheduleKeyForPlugin(request.pluginName, request.scheduleId))) {
      throw new Error(`unknown schedule ${request.pluginName}/${request.scheduleId}`);
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
  async listEventTriggers(request) {
    return [...triggers.values()].filter(
      (trigger) => trigger.target?.pluginName === request.pluginName,
    );
  },
  async deleteEventTrigger(request) {
    if (!triggers.delete(triggerKeyForPlugin(request.pluginName, request.triggerId))) {
      throw new Error(`unknown trigger ${request.pluginName}/${request.triggerId}`);
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
    const existing = triggers.get(`${request.pluginName}:published`);
    const trigger = create(BoundWorkflowEventTriggerSchema, {
      id: "published",
      ...(existing?.match ? { match: existing.match } : {}),
      target: existing?.target ?? {
        pluginName: request.pluginName,
        operation: "published",
      },
      paused: false,
    });
    triggers.set(`${request.pluginName}:published`, trigger);
  },
  warnings() {
    return publishCount > 0 ? [`published-events:${publishCount}`] : [];
  },
});

function scheduleKey(request: UpsertWorkflowProviderScheduleRequest): string {
  return scheduleKeyForPlugin(request.target?.pluginName ?? "", request.scheduleId);
}

function scheduleKeyForPlugin(pluginName: string, scheduleId: string): string {
  return `${pluginName}:${scheduleId}`;
}

function requireRun(request: GetWorkflowProviderRunRequest) {
  return requireRunByID(request.pluginName, request.runId);
}

function requireSchedule(request: GetWorkflowProviderScheduleRequest) {
  const schedule = schedules.get(
    scheduleKeyForPlugin(request.pluginName, request.scheduleId),
  );
  if (!schedule) {
    throw new Error(`unknown schedule ${request.pluginName}/${request.scheduleId}`);
  }
  return schedule;
}

function requireTrigger(request: GetWorkflowProviderEventTriggerRequest) {
  const trigger = triggers.get(
    triggerKeyForPlugin(request.pluginName, request.triggerId),
  );
  if (!trigger) {
    throw new Error(`unknown trigger ${request.pluginName}/${request.triggerId}`);
  }
  return trigger;
}

function updateSchedule(
  request:
    | PauseWorkflowProviderScheduleRequest
    | ResumeWorkflowProviderScheduleRequest,
  paused: boolean,
) {
  const schedule = schedules.get(
    scheduleKeyForPlugin(request.pluginName, request.scheduleId),
  );
  if (!schedule) {
    throw new Error(`unknown schedule ${request.pluginName}/${request.scheduleId}`);
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
  schedules.set(
    scheduleKeyForPlugin(request.pluginName, request.scheduleId),
    updated,
  );
  return updated;
}

function updateTrigger(
  request:
    | PauseWorkflowProviderEventTriggerRequest
    | ResumeWorkflowProviderEventTriggerRequest,
  paused: boolean,
) {
  const trigger = triggers.get(
    triggerKeyForPlugin(request.pluginName, request.triggerId),
  );
  if (!trigger) {
    throw new Error(`unknown trigger ${request.pluginName}/${request.triggerId}`);
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
  triggers.set(
    triggerKeyForPlugin(request.pluginName, request.triggerId),
    updated,
  );
  return updated;
}

function triggerKey(request: UpsertWorkflowProviderEventTriggerRequest): string {
  return triggerKeyForPlugin(request.target?.pluginName ?? "", request.triggerId);
}

function triggerKeyForPlugin(pluginName: string, triggerId: string): string {
  return `${pluginName}:${triggerId}`;
}

function requireRunByID(pluginName: string, runId: string) {
  const run = runs.get(runId);
  if (!run || run.target?.pluginName !== pluginName) {
    throw new Error(`unknown run ${pluginName}/${runId}`);
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
