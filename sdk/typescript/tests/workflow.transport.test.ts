import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";

import { create } from "@bufbuild/protobuf";
import { createClient, type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter, createGrpcTransport } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  ApplyWorkflowDeploymentRequestSchema,
  DeliverWorkflowEventRequestSchema,
  InvokeWorkflowActionRequestSchema,
  WorkflowActionResultSchema,
  PlanWorkflowRequestSchema,
  StartWorkflowRunRequestSchema,
  WorkflowHost as WorkflowHostService,
  WorkflowProvider as WorkflowProviderService,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  ENV_WORKFLOW_HOST_SOCKET,
  ENV_WORKFLOW_HOST_SOCKET_TOKEN,
  WorkflowDeploymentStatus,
  WorkflowHost,
  WorkflowRunStatus,
  createWorkflowProviderService,
  defineWorkflowProvider,
  deliverWorkflowEventResponse,
  planWorkflowResponse,
  workflowActionResult,
  workflowDeployment,
  workflowRun,
  workflowRunOutput,
  workflowRunSignal,
} from "../src/index.ts";

async function reserveTCPAddress(): Promise<string> {
  return await new Promise((resolve, reject) => {
    const server = createNetServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close();
        reject(new Error("failed to reserve tcp address"));
        return;
      }
      const result = `${address.address}:${address.port}`;
      server.close((err) => {
        if (err) {
          reject(err);
          return;
        }
        resolve(result);
      });
    });
  });
}

test("WorkflowProvider service serves deployment, run, and event APIs", async () => {
  const address = await reserveTCPAddress();
  const calls: Array<{ method: string; detail: string }> = [];
  const provider = defineWorkflowProvider({
    displayName: "Workflow transport fixture",
    async planWorkflow(request) {
      calls.push({ method: "plan", detail: request.specDigest });
      return planWorkflowResponse({
        acceptedSpecDigest: request.specDigest,
        providerPlanId: "plan-typescript",
        providerPlanDigest: "plan-digest-typescript",
        providerPlanFormatVersion: "workflow-plan-v1",
      });
    },
    async applyDeployment(request) {
      calls.push({ method: "apply", detail: request.spec?.id ?? "" });
      return workflowDeployment({
        spec: request.spec,
        status: WorkflowDeploymentStatus.ACTIVE,
        appliedGeneration: request.spec?.generation ?? 0n,
        providerPlanId: request.plan?.providerPlanId ?? "",
      });
    },
    async getDeployment(request) {
      return workflowDeployment({
        spec: { id: request.deploymentId },
        status: WorkflowDeploymentStatus.ACTIVE,
      });
    },
    async listDeployments() {
      return [];
    },
    async deleteDeployment() {},
    async setDeploymentPaused(request) {
      return workflowDeployment({
        spec: { id: request.deploymentId },
        status: request.paused
          ? WorkflowDeploymentStatus.PAUSED
          : WorkflowDeploymentStatus.ACTIVE,
      });
    },
    async setActivationPaused(request) {
      return workflowDeployment({
        spec: { id: request.deploymentId },
        status: request.paused
          ? WorkflowDeploymentStatus.PAUSED
          : WorkflowDeploymentStatus.ACTIVE,
      });
    },
    async startRun(request) {
      calls.push({ method: "start", detail: request.deploymentId });
      return workflowRun({
        id: request.idempotencyKey || "run-1",
        deploymentId: request.deploymentId,
        deploymentGeneration: request.deploymentGeneration,
        workflowKey: request.workflowKey,
        status: WorkflowRunStatus.PENDING,
      });
    },
    async signalRun(request) {
      return workflowRunSignal({
        run: workflowRun({
          id: request.runId,
          status: WorkflowRunStatus.RUNNING,
        }),
        signal: request.signal,
      });
    },
    async signalOrStartRun(request) {
      return workflowRunSignal({
        run: workflowRun({
          id: request.workflowKey || "run-2",
          deploymentId: request.deploymentId,
          status: WorkflowRunStatus.PENDING,
        }),
        signal: request.signal,
        startedRun: true,
        workflowKey: request.workflowKey,
      });
    },
    async cancelRun(request) {
      return workflowRun({
        id: request.runId,
        status: WorkflowRunStatus.CANCELED,
        statusMessage: request.reason,
      });
    },
    async deliverEvent(request) {
      calls.push({ method: "deliver", detail: request.event?.type ?? "" });
      return deliverWorkflowEventResponse();
    },
    async getRun(request) {
      return workflowRun({ id: request.runId, status: WorkflowRunStatus.RUNNING });
    },
    async listRuns() {
      return [];
    },
    async getRunEvents() {
      return [];
    },
    async getRunOutput(request) {
      return workflowRunOutput({ outputRef: request.outputRef });
    },
  });

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowProviderService, createWorkflowProviderService(provider));
    },
  });
  const server = createServer((req, res) => handler(req, res));

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(Number(address.split(":").at(-1)), "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });

    const client = createClient(
      WorkflowProviderService,
      createGrpcTransport({ baseUrl: `http://${address}` }),
    );
    const plan = await client.planWorkflow(create(PlanWorkflowRequestSchema, {
      specDigest: "spec-digest-typescript",
    }));
    expect(plan.providerPlanId).toBe("plan-typescript");

    const deployment = await client.applyWorkflowDeployment(
      create(ApplyWorkflowDeploymentRequestSchema, {
        spec: {
          id: "deployment-typescript",
          generation: 2n,
        },
        plan,
      }),
    );
    expect(deployment.appliedGeneration).toBe(2n);

    const run = await client.startWorkflowRun(create(StartWorkflowRunRequestSchema, {
      deploymentId: "deployment-typescript",
      deploymentGeneration: 2n,
      idempotencyKey: "run-native-ts",
    }));
    expect(run.id).toBe("run-native-ts");
    expect(run.status).toBe(WorkflowRunStatus.PENDING);

    await client.deliverWorkflowEvent(create(DeliverWorkflowEventRequestSchema, {
      deliveryId: "delivery-typescript",
      event: { type: "demo.synced" },
    }));

    expect(calls).toEqual([
      { method: "plan", detail: "spec-digest-typescript" },
      { method: "apply", detail: "deployment-typescript" },
      { method: "start", detail: "deployment-typescript" },
      { method: "deliver", detail: "demo.synced" },
    ]);
  } finally {
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});

test("WorkflowHost honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_WORKFLOW_HOST_SOCKET];
  const previousToken = process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowHostService, {
        async invokeWorkflowAction(input) {
          return create(WorkflowActionResultSchema, {
            actionEventId: input.selector?.runId ?? "",
            status: 202,
            body: input.selector?.runId ?? "",
          });
        },
      } satisfies Partial<ServiceImpl<typeof WorkflowHostService>>);
    },
  });
  const server = createServer((req, res) => {
    const tokenHeader = req.headers["x-gestalt-host-service-relay-token"];
    if (typeof tokenHeader === "string") {
      seenTokens.push(tokenHeader);
    }
    handler(req, res);
  });

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(Number(address.split(":").at(-1)), "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_WORKFLOW_HOST_SOCKET] = `tcp://${address}`;
    process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN] = "relay-token-typescript";

    const host = new WorkflowHost();
    const response = await host.invokeWorkflowAction(
      create(InvokeWorkflowActionRequestSchema, {
        selector: { runId: "run-123" },
        action: { case: "plugin", value: {} },
      }),
    );

    expect(workflowActionResult(response).status).toBe(202);
    expect(response.body).toBe("run-123");
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_WORKFLOW_HOST_SOCKET];
    } else {
      process.env[ENV_WORKFLOW_HOST_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN];
    } else {
      process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
