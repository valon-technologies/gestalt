import {
  WorkflowDefinitionStatus,
  WorkflowRunStatus,
  defineWorkflowProvider,
  deliverWorkflowEventResponse,
  workflowDefinition,
  workflowRun,
  workflowRunOutput,
  workflowRunSignal,
  type WorkflowDefinition,
  type WorkflowRun,
} from "../../../src/index.ts";

const definitions = new Map<string, WorkflowDefinition>();
const runs = new Map<string, WorkflowRun>();
let deliveredEventCount = 0;

export const provider = defineWorkflowProvider({
  displayName: "Fixture Workflow",
  description: "Workflow provider fixture used by SDK tests",
  configure() {
    definitions.clear();
    runs.clear();
    deliveredEventCount = 0;
  },
  async applyDefinition(request) {
    const deployment = workflowDefinition({
      ...(request.spec !== undefined ? { spec: request.spec } : {}),
      status: request.spec?.paused
        ? WorkflowDefinitionStatus.PAUSED
        : WorkflowDefinitionStatus.ACTIVE,
      appliedGeneration: request.spec?.generation ?? 0n,
      providerPlanId: "fixture-plan",
      providerPlanDigest: `fixture-plan:${request.spec?.id ?? ""}`,
      ...(request.binding !== undefined ? { binding: request.binding } : {}),
    });
    definitions.set(deploymentID(deployment), deployment);
    return deployment;
  },
  async getDefinition(request) {
    return requireDeployment(request.definitionId);
  },
  async listDefinitions() {
    return [...definitions.values()];
  },
  async deleteDefinition(request) {
    definitions.delete(request.definitionId);
  },
  async setDefinitionPaused(request) {
    const deployment = requireDeployment(request.definitionId);
    const updated = workflowDefinition({
      ...deployment,
      status: request.paused
        ? WorkflowDefinitionStatus.PAUSED
        : WorkflowDefinitionStatus.ACTIVE,
    });
    definitions.set(request.definitionId, updated);
    return updated;
  },
  async setActivationPaused(request) {
    const deployment = requireDeployment(request.definitionId);
    if (deployment.spec === undefined) {
      return deployment;
    }
    const spec = {
      ...deployment.spec,
      activations: deployment.spec.activations.map((activation) =>
        activation.id === request.activationId
          ? { ...activation, paused: request.paused }
          : activation
      ),
    };
    const updated = workflowDefinition({ ...deployment, spec });
    definitions.set(request.definitionId, updated);
    return updated;
  },
  async startRun(request) {
    const run = workflowRun({
      id: `${request.definitionId}:${runs.size + 1}`,
      definitionId: request.definitionId,
      definitionGeneration: request.definitionGeneration,
      workflowKey: request.workflowKey,
      trigger: {
        definitionId: request.definitionId,
        definitionGeneration: request.definitionGeneration,
        activationId: request.activationId,
        kind: { case: "manual", value: {} },
      },
      ...(request.input !== undefined ? { input: request.input } : {}),
      status: WorkflowRunStatus.PENDING,
      ...(request.createdBy !== undefined ? { createdBy: request.createdBy } : {}),
      statusMessage: request.idempotencyKey
        ? `idempotency:${request.idempotencyKey}`
        : "",
    });
    runs.set(run.id, run);
    return run;
  },
  async signalRun(request) {
    const run = requireRun(request.runId);
    return workflowRunSignal({
      run,
      ...(request.signal !== undefined ? { signal: request.signal } : {}),
      startedRun: false,
      workflowKey: run.workflowKey,
    });
  },
  async signalOrStartRun(request) {
    const run = workflowRun({
      id: `${request.workflowKey || "workflow"}:${runs.size + 1}`,
      definitionId: request.definitionId,
      definitionGeneration: request.definitionGeneration,
      workflowKey: request.workflowKey,
      trigger: {
        definitionId: request.definitionId,
        definitionGeneration: request.definitionGeneration,
        activationId: request.activationId,
        kind: { case: "manual", value: {} },
      },
      ...(request.input !== undefined ? { input: request.input } : {}),
      status: WorkflowRunStatus.PENDING,
      ...(request.createdBy !== undefined ? { createdBy: request.createdBy } : {}),
    });
    runs.set(run.id, run);
    return workflowRunSignal({
      run,
      ...(request.signal !== undefined ? { signal: request.signal } : {}),
      startedRun: true,
      workflowKey: request.workflowKey,
    });
  },
  async cancelRun(request) {
    const run = requireRun(request.runId);
    const updated = workflowRun({
      ...run,
      status: WorkflowRunStatus.CANCELED,
      statusMessage: request.reason,
    });
    runs.set(updated.id, updated);
    return updated;
  },
  async deliverEvent(request) {
    deliveredEventCount += 1;
    const run = workflowRun({
      id: `${request.deliveryId || "event"}:${runs.size + 1}`,
      definitionId: "event-deployment",
      workflowKey: request.event?.subject ?? "",
      trigger: {
        definitionId: "event-deployment",
        activationId: "event",
        kind: {
          case: "event",
          value: {
            activationId: "event",
            ...(request.event !== undefined ? { event: request.event } : {}),
          },
        },
      },
      status: WorkflowRunStatus.PENDING,
      ...(request.publishedBy !== undefined ? { createdBy: request.publishedBy } : {}),
    });
    runs.set(run.id, run);
    return deliverWorkflowEventResponse({
      results: [{
        definitionId: run.definitionId,
        activationId: "event",
        run,
        startedRun: true,
      }],
    });
  },
  async getRun(request) {
    return requireRun(request.runId);
  },
  async listRuns() {
    return [...runs.values()];
  },
  async getRunEvents() {
    return [];
  },
  async getRunOutput(request) {
    return workflowRunOutput({ outputRef: request.outputRef });
  },
  warnings() {
    return deliveredEventCount > 0
      ? [`delivered-events:${deliveredEventCount}`]
      : [];
  },
});

function deploymentID(deployment: WorkflowDefinition): string {
  return deployment.spec?.id || deployment.binding?.definitionId || "deployment";
}

function requireDeployment(definitionId: string): WorkflowDefinition {
  const deployment = definitions.get(definitionId);
  if (!deployment) {
    throw new Error(`unknown deployment ${definitionId}`);
  }
  return deployment;
}

function requireRun(runId: string): WorkflowRun {
  const run = runs.get(runId);
  if (!run) {
    throw new Error(`unknown run ${runId}`);
  }
  return run;
}
