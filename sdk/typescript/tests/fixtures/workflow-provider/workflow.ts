import {
  WorkflowDeploymentStatus,
  WorkflowRunStatus,
  defineWorkflowProvider,
  deliverWorkflowEventResponse,
  planWorkflowResponse,
  workflowDeployment,
  workflowRun,
  workflowRunOutput,
  workflowRunSignal,
  type WorkflowDeployment,
  type WorkflowRun,
} from "../../../src/index.ts";

const deployments = new Map<string, WorkflowDeployment>();
const runs = new Map<string, WorkflowRun>();
let deliveredEventCount = 0;

export const provider = defineWorkflowProvider({
  displayName: "Fixture Workflow",
  description: "Workflow provider fixture used by SDK tests",
  configure() {
    deployments.clear();
    runs.clear();
    deliveredEventCount = 0;
  },
  async planWorkflow(request) {
    return planWorkflowResponse({
      acceptedSpecDigest: request.specDigest,
      providerPlanId: "fixture-plan",
      providerPlanDigest: `fixture-plan:${request.specDigest}`,
      providerPlanFormatVersion: "workflow-plan-v1",
    });
  },
  async applyDeployment(request) {
    const deployment = workflowDeployment({
      spec: request.spec,
      status: request.spec?.paused
        ? WorkflowDeploymentStatus.PAUSED
        : WorkflowDeploymentStatus.ACTIVE,
      appliedGeneration: request.spec?.generation ?? 0n,
      providerPlanId: request.plan?.providerPlanId ?? "",
      providerPlanDigest: request.plan?.providerPlanDigest ?? "",
      binding: request.binding,
    });
    deployments.set(deploymentID(deployment), deployment);
    return deployment;
  },
  async getDeployment(request) {
    return requireDeployment(request.deploymentId);
  },
  async listDeployments() {
    return [...deployments.values()];
  },
  async deleteDeployment(request) {
    deployments.delete(request.deploymentId);
  },
  async setDeploymentPaused(request) {
    const deployment = requireDeployment(request.deploymentId);
    const updated = workflowDeployment({
      ...deployment,
      status: request.paused
        ? WorkflowDeploymentStatus.PAUSED
        : WorkflowDeploymentStatus.ACTIVE,
    });
    deployments.set(request.deploymentId, updated);
    return updated;
  },
  async setActivationPaused(request) {
    const deployment = requireDeployment(request.deploymentId);
    const spec = deployment.spec === undefined
      ? undefined
      : {
        ...deployment.spec,
        activations: deployment.spec.activations.map((activation) =>
          activation.id === request.activationId
            ? { ...activation, paused: request.paused }
            : activation
        ),
      };
    const updated = workflowDeployment({ ...deployment, spec });
    deployments.set(request.deploymentId, updated);
    return updated;
  },
  async startRun(request) {
    const run = workflowRun({
      id: `${request.deploymentId}:${runs.size + 1}`,
      deploymentId: request.deploymentId,
      deploymentGeneration: request.deploymentGeneration,
      workflowKey: request.workflowKey,
      trigger: {
        deploymentId: request.deploymentId,
        deploymentGeneration: request.deploymentGeneration,
        activationId: request.activationId,
        kind: { case: "manual", value: {} },
      },
      input: request.input,
      status: WorkflowRunStatus.PENDING,
      createdBy: request.createdBy,
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
      signal: request.signal,
      startedRun: false,
      workflowKey: run.workflowKey,
    });
  },
  async signalOrStartRun(request) {
    const run = workflowRun({
      id: `${request.workflowKey || "workflow"}:${runs.size + 1}`,
      deploymentId: request.deploymentId,
      deploymentGeneration: request.deploymentGeneration,
      workflowKey: request.workflowKey,
      trigger: {
        deploymentId: request.deploymentId,
        deploymentGeneration: request.deploymentGeneration,
        activationId: request.activationId,
        kind: { case: "manual", value: {} },
      },
      input: request.input,
      status: WorkflowRunStatus.PENDING,
      createdBy: request.createdBy,
    });
    runs.set(run.id, run);
    return workflowRunSignal({
      run,
      signal: request.signal,
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
      deploymentId: "event-deployment",
      workflowKey: request.event?.subject ?? "",
      trigger: {
        deploymentId: "event-deployment",
        activationId: "event",
        kind: {
          case: "event",
          value: {
            activationId: "event",
            event: request.event,
          },
        },
      },
      status: WorkflowRunStatus.PENDING,
      createdBy: request.publishedBy,
    });
    runs.set(run.id, run);
    return deliverWorkflowEventResponse({
      results: [{
        deploymentId: run.deploymentId,
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

function deploymentID(deployment: WorkflowDeployment): string {
  return deployment.spec?.id || deployment.binding?.deploymentId || "deployment";
}

function requireDeployment(deploymentId: string): WorkflowDeployment {
  const deployment = deployments.get(deploymentId);
  if (!deployment) {
    throw new Error(`unknown deployment ${deploymentId}`);
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
