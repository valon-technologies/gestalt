import {
  WorkflowRunStatus,
  WorkflowStepStatus,
  boundWorkflowTarget,
  defineWorkflowProvider,
  workflowDefinition,
  workflowEvent,
  workflowRun,
  workflowRunEvent,
  type BoundWorkflowTarget,
  type GetWorkflowProviderDefinitionRequest,
  type GetWorkflowProviderRunRequest,
  type WorkflowDefinition,
  type WorkflowRun,
} from "../../../src/index.ts";

const runs = new Map<string, WorkflowRun>();
const definitions = new Map<string, WorkflowDefinition>();
const runEvents = new Map<string, ReturnType<typeof workflowRunEvent>[]>();
let deliverCount = 0;
let generation = 0n;

function appStepTarget(appName: string, operation: string): BoundWorkflowTarget {
  return boundWorkflowTarget({
    steps: [{ id: operation, app: { name: appName, operation } }],
  });
}

export const provider = defineWorkflowProvider({
  displayName: "Fixture Workflow",
  description: "Workflow provider fixture used by SDK tests",
  configure() {
    runs.clear();
    definitions.clear();
    runEvents.clear();
    deliverCount = 0;
    generation = 0n;
  },
  async applyDefinition(request) {
    const spec = request.spec ?? {};
    const id = spec.id || request.idempotencyKey || `definition:${definitions.size + 1}`;
    generation += 1n;
    const definition = workflowDefinition({
      id,
      generation,
      target: spec.target,
      activations: spec.activations,
      paused: spec.paused,
      createdBySubjectId: request.requestedBySubjectId,
      updatedAt: new Date("2026-05-08T12:00:00.000Z"),
    });
    definitions.set(id, definition);
    return definition;
  },
  async getDefinition(request) {
    return requireDefinition(request);
  },
  async listDefinitions() {
    return [...definitions.values()];
  },
  async setDefinitionPaused(request) {
    const existing = requireDefinition(request);
    const updated = workflowDefinition({ ...existing, paused: request.paused });
    definitions.set(request.definitionId, updated);
    return updated;
  },
  async setActivationPaused(request) {
    const existing = requireDefinition(request);
    const updated = workflowDefinition({
      ...existing,
      activations: existing.activations?.map((activation) =>
        activation.id === request.activationId
          ? { ...activation, paused: request.paused }
          : activation
      ),
    });
    definitions.set(request.definitionId, updated);
    return updated;
  },
  async deleteDefinition(request) {
    if (!definitions.delete(request.definitionId)) {
      throw new Error(`unknown definition ${request.definitionId}`);
    }
  },
  async startRun(request) {
    const run = createRun(
      `${request.definitionId || "definition"}:${runs.size + 1}`,
      request.definitionId,
      request.workflowKey ?? "",
      request.input,
      request.createdBySubjectId,
      request.idempotencyKey ? `idempotency:${request.idempotencyKey}` : "",
    );
    runs.set(run.id ?? "", run);
    runEvents.set(run.id ?? "", [
      workflowRunEvent({
        id: `${run.id}:started`,
        runId: run.id,
        stepId: run.currentStepId,
        type: "run.started",
        data: { definitionId: request.definitionId ?? "" },
        createdAt: new Date("2026-05-08T12:00:00.000Z"),
      }),
    ]);
    return run;
  },
  async getRun(request) {
    return requireRun(request);
  },
  async listRuns() {
    return [...runs.values()];
  },
  async getRunEvents(request) {
    return runEvents.get(request.runId) ?? [];
  },
  async getRunOutput(request) {
    const run = requireRun(request);
    return run.output ?? {};
  },
  async cancelRun(request) {
    const run = requireRunByID(request.runId);
    const updated = workflowRun({
      ...run,
      status: WorkflowRunStatus.CANCELED,
      statusMessage: request.reason,
      completedAt: new Date("2026-05-08T12:05:00.000Z"),
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
    const run = createRun(
      `${request.workflowKey || request.definitionId || "workflow"}:${runs.size + 1}`,
      request.definitionId,
      request.workflowKey,
      request.input,
      request.createdBySubjectId,
      request.idempotencyKey ? `idempotency:${request.idempotencyKey}` : "",
    );
    runs.set(run.id ?? "", run);
    return {
      run,
      signal: request.signal,
      startedRun: true,
      workflowKey: request.workflowKey ?? "",
    };
  },
  async deliverEvent(request) {
    deliverCount += 1;
    return workflowEvent({
      id: `delivered:${deliverCount}`,
      type: request.event?.type ?? "",
      source: request.event?.source ?? request.appName ?? "",
    });
  },
  warnings() {
    return deliverCount > 0 ? [`delivered-events:${deliverCount}`] : [];
  },
});

function requireRun(request: GetWorkflowProviderRunRequest) {
  return requireRunByID(request.runId);
}

function requireDefinition(
  request: GetWorkflowProviderDefinitionRequest | { definitionId: string },
) {
  const definition = definitions.get(request.definitionId);
  if (!definition) {
    throw new Error(`unknown definition ${request.definitionId}`);
  }
  return definition;
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
  definitionId: string | undefined,
  workflowKey: string | undefined,
  input: WorkflowRun["input"],
  createdBySubjectId: WorkflowRun["createdBySubjectId"],
  statusMessage: string,
) {
  const definition = definitionId ? definitions.get(definitionId) : undefined;
  const target = definition?.target ?? appStepTarget("roadmap", "sync");
  const firstStep = target.steps?.[0];
  return workflowRun({
    id,
    status: WorkflowRunStatus.PENDING,
    statusMessage,
    target,
    createdBySubjectId,
    workflowKey,
    definitionId,
    definitionGeneration: definition?.generation,
    input,
    currentStepId: firstStep?.id ?? "",
    steps: firstStep === undefined
      ? []
      : [{
        stepId: firstStep.id,
        status: WorkflowStepStatus.PENDING,
        input,
      }],
    output: { fixture: true, runId: id },
  });
}
