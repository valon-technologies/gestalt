import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  ManagedWorkflowDeploymentSchema,
  ManagedWorkflowRunSchema,
  ManagedWorkflowRunSignalSchema,
  PlanWorkflowResponseSchema,
  WorkflowDeploymentSchema,
  WorkflowDeploymentStatus,
  WorkflowManagerDeliverEventResponseSchema,
  WorkflowManagerHost as WorkflowManagerHostService,
  WorkflowManagerListDeploymentsResponseSchema,
  WorkflowRunSchema,
  WorkflowRunStatus,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  ENV_WORKFLOW_MANAGER_SOCKET,
  ENV_WORKFLOW_MANAGER_SOCKET_TOKEN,
  WorkflowManager,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("WorkflowManager forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-workflow-manager-"));
  const socketPath = join(tempDir, "workflow-manager.sock");
  const previousSocket = process.env[ENV_WORKFLOW_MANAGER_SOCKET];
  const calls: Array<{
    method: string;
    invocationToken: string;
    idempotencyKey?: string;
    providerName?: string;
    deploymentId?: string | undefined;
    runId?: string;
    signalName?: string | undefined;
    workflowKey?: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowManagerHostService, {
        async planDeployment(input) {
          calls.push({
            method: "plan-deployment",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            deploymentId: input.spec?.id,
          });
          return create(PlanWorkflowResponseSchema, {
            acceptedSpecDigest: "spec-digest",
            providerPlanId: "plan-1",
          });
        },
        async applyDeployment(input) {
          calls.push({
            method: "apply-deployment",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            deploymentId: input.spec?.id,
          });
          return create(ManagedWorkflowDeploymentSchema, {
            providerName: input.providerName || "basic",
            deployment: create(WorkflowDeploymentSchema, {
              spec: input.spec,
              status: WorkflowDeploymentStatus.ACTIVE,
            }),
          });
        },
        async getDeployment(input) {
          calls.push({
            method: "get-deployment",
            invocationToken: input.invocationToken,
            deploymentId: input.deploymentId,
          });
          return create(ManagedWorkflowDeploymentSchema, {
            providerName: "basic",
            deployment: create(WorkflowDeploymentSchema, {
              spec: { id: input.deploymentId },
              status: WorkflowDeploymentStatus.ACTIVE,
            }),
          });
        },
        async listDeployments(input) {
          calls.push({
            method: "list-deployments",
            invocationToken: input.invocationToken,
            providerName: input.providerName,
          });
          return create(WorkflowManagerListDeploymentsResponseSchema, {
            deployments: [create(ManagedWorkflowDeploymentSchema, {
              providerName: input.providerName,
              deployment: create(WorkflowDeploymentSchema, {
                spec: { id: "deployment-1" },
              }),
            })],
          });
        },
        async deleteDeployment(input) {
          calls.push({
            method: "delete-deployment",
            invocationToken: input.invocationToken,
            deploymentId: input.deploymentId,
          });
          return create(EmptySchema, {});
        },
        async setDeploymentPaused(input) {
          calls.push({
            method: "set-deployment-paused",
            invocationToken: input.invocationToken,
            deploymentId: input.deploymentId,
          });
          return create(ManagedWorkflowDeploymentSchema, {
            providerName: "basic",
            deployment: create(WorkflowDeploymentSchema, {
              spec: { id: input.deploymentId },
              status: input.paused
                ? WorkflowDeploymentStatus.PAUSED
                : WorkflowDeploymentStatus.ACTIVE,
            }),
          });
        },
        async setActivationPaused(input) {
          calls.push({
            method: "set-activation-paused",
            invocationToken: input.invocationToken,
            deploymentId: input.deploymentId,
          });
          return create(ManagedWorkflowDeploymentSchema, {
            providerName: "basic",
            deployment: create(WorkflowDeploymentSchema, {
              spec: { id: input.deploymentId },
            }),
          });
        },
        async startRun(input) {
          calls.push({
            method: "start-run",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            deploymentId: input.deploymentId,
            workflowKey: input.workflowKey,
          });
          return create(ManagedWorkflowRunSchema, {
            providerName: input.providerName || "basic",
            run: create(WorkflowRunSchema, {
              id: "run-1",
              deploymentId: input.deploymentId,
              workflowKey: input.workflowKey,
              status: WorkflowRunStatus.PENDING,
            }),
          });
        },
        async signalRun(input) {
          calls.push({
            method: "signal-run",
            invocationToken: input.invocationToken,
            runId: input.runId,
            signalName: input.signal?.name,
          });
          return create(ManagedWorkflowRunSignalSchema, {
            providerName: "basic",
            run: create(WorkflowRunSchema, { id: input.runId }),
            signal: input.signal,
          });
        },
        async signalOrStartRun(input) {
          calls.push({
            method: "signal-or-start-run",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
            deploymentId: input.deploymentId,
            signalName: input.signal?.name,
            workflowKey: input.workflowKey,
          });
          return create(ManagedWorkflowRunSignalSchema, {
            providerName: input.providerName || "basic",
            run: create(WorkflowRunSchema, {
              id: "run-2",
              deploymentId: input.deploymentId,
            }),
            signal: input.signal,
            startedRun: true,
            workflowKey: input.workflowKey,
          });
        },
        async cancelRun(input) {
          calls.push({
            method: "cancel-run",
            invocationToken: input.invocationToken,
            runId: input.runId,
          });
          return create(ManagedWorkflowRunSchema, {
            providerName: "basic",
            run: create(WorkflowRunSchema, {
              id: input.runId,
              status: WorkflowRunStatus.CANCELED,
            }),
          });
        },
        async deliverEvent(input) {
          calls.push({
            method: "deliver-event",
            invocationToken: input.invocationToken,
            idempotencyKey: input.idempotencyKey,
            providerName: input.providerName,
          });
          return create(WorkflowManagerDeliverEventResponseSchema, {
            results: [{
              deploymentId: "deployment-1",
              activationId: "event",
              run: create(WorkflowRunSchema, { id: "run-event" }),
            }],
          });
        },
      } satisfies Partial<ServiceImpl<typeof WorkflowManagerHostService>>);
    },
  });
  const server = createServer(handler);

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(socketPath, () => {
        server.off("error", reject);
        resolve();
      });
    });

    process.env[ENV_WORKFLOW_MANAGER_SOCKET] = socketPath;

    const fromHandle = new WorkflowManager("invocation-token-123");
    const plan = await fromHandle.planDeployment({
      providerName: "basic",
      spec: { id: "deployment-1" } as any,
      idempotencyKey: "workflow-plan-key-ts",
    });
    expect(plan.providerPlanId).toBe("plan-1");

    const fromRequest = new WorkflowManager(
      request(
        "tok",
        {},
        {},
        {},
        {},
        {},
        "invocation-token-456",
        "workflow-request-key-ts",
      ),
    );
    const applied = await fromRequest.applyDeployment({
      providerName: "basic",
      spec: { id: "deployment-1" } as any,
    });
    const fetched = await fromRequest.getDeployment({
      deploymentId: "deployment-1",
    });
    const listed = await fromRequest.listDeployments({ providerName: "basic" });
    const paused = await fromRequest.setDeploymentPaused({
      deploymentId: "deployment-1",
      paused: true,
    });
    await fromRequest.setActivationPaused({
      deploymentId: "deployment-1",
      activationId: "manual",
      paused: true,
    });
    const startedRun = await fromRequest.startRun({
      providerName: "basic",
      deploymentId: "deployment-1",
      workflowKey: "roadmap-summary:item-1",
    });
    const signaledRun = await fromRequest.signalRun({
      runId: "run-1",
      signal: {
        name: "roadmap.item.updated",
      } as any,
    });
    const signaledOrStartedRun = await fromRequest.signalOrStartRun({
      providerName: "basic",
      deploymentId: "deployment-1",
      workflowKey: "roadmap-summary:item-1",
      signal: {
        name: "roadmap.item.updated",
      } as any,
    });
    const canceled = await fromRequest.cancelRun({
      runId: "run-1",
      reason: "test",
    });
    const delivered = await fromRequest.deliverEvent({
      providerName: "basic",
      event: {
        type: "roadmap.item.updated",
        source: "roadmap",
      } as any,
    });
    await fromRequest.deleteDeployment({ deploymentId: "deployment-1" });

    expect(applied.deployment?.spec?.id).toBe("deployment-1");
    expect(fetched.deployment?.spec?.id).toBe("deployment-1");
    expect(listed).toHaveLength(1);
    expect(paused.deployment?.status).toBe(WorkflowDeploymentStatus.PAUSED);
    expect(startedRun.run?.id).toBe("run-1");
    expect(signaledRun.signal?.name).toBe("roadmap.item.updated");
    expect(signaledOrStartedRun.startedRun).toBe(true);
    expect(canceled.run?.status).toBe(WorkflowRunStatus.CANCELED);
    expect(delivered.results[0]?.run?.id).toBe("run-event");
    expect(calls).toEqual([
      {
        method: "plan-deployment",
        invocationToken: "invocation-token-123",
        idempotencyKey: "workflow-plan-key-ts",
        providerName: "basic",
        deploymentId: "deployment-1",
      },
      {
        method: "apply-deployment",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        deploymentId: "deployment-1",
      },
      {
        method: "get-deployment",
        invocationToken: "invocation-token-456",
        deploymentId: "deployment-1",
      },
      {
        method: "list-deployments",
        invocationToken: "invocation-token-456",
        providerName: "basic",
      },
      {
        method: "set-deployment-paused",
        invocationToken: "invocation-token-456",
        deploymentId: "deployment-1",
      },
      {
        method: "set-activation-paused",
        invocationToken: "invocation-token-456",
        deploymentId: "deployment-1",
      },
      {
        method: "start-run",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        deploymentId: "deployment-1",
        workflowKey: "roadmap-summary:item-1",
      },
      {
        method: "signal-run",
        invocationToken: "invocation-token-456",
        runId: "run-1",
        signalName: "roadmap.item.updated",
      },
      {
        method: "signal-or-start-run",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
        deploymentId: "deployment-1",
        signalName: "roadmap.item.updated",
        workflowKey: "roadmap-summary:item-1",
      },
      {
        method: "cancel-run",
        invocationToken: "invocation-token-456",
        runId: "run-1",
      },
      {
        method: "deliver-event",
        invocationToken: "invocation-token-456",
        idempotencyKey: "workflow-request-key-ts",
        providerName: "basic",
      },
      {
        method: "delete-deployment",
        invocationToken: "invocation-token-456",
        deploymentId: "deployment-1",
      },
    ]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    } else {
      process.env[ENV_WORKFLOW_MANAGER_SOCKET] = previousSocket;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
    removeTempDir(tempDir);
  }
});

test("WorkflowManager prioritizes invocation-token validation over socket configuration", () => {
  const previousSocket = process.env[ENV_WORKFLOW_MANAGER_SOCKET];

  try {
    delete process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    expect(() => new WorkflowManager("   ")).toThrow(
      "workflow manager: invocation token is not available",
    );
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    } else {
      process.env[ENV_WORKFLOW_MANAGER_SOCKET] = previousSocket;
    }
  }
});

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

test("WorkflowManager honors tcp target env and relay token env", async () => {
  const previousSocket = process.env[ENV_WORKFLOW_MANAGER_SOCKET];
  const previousToken = process.env[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN];
  const seenTokens: string[] = [];
  const address = await reserveTCPAddress();

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(WorkflowManagerHostService, {
        async applyDeployment(input) {
          return create(ManagedWorkflowDeploymentSchema, {
            providerName: input.providerName || "basic",
            deployment: create(WorkflowDeploymentSchema, {
              spec: input.spec,
              status: WorkflowDeploymentStatus.ACTIVE,
            }),
          });
        },
      } satisfies Partial<ServiceImpl<typeof WorkflowManagerHostService>>);
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

    process.env[ENV_WORKFLOW_MANAGER_SOCKET] = `tcp://${address}`;
    process.env[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN] = "relay-token-typescript";

    const manager = new WorkflowManager("invoke-token");
    const applied = await manager.applyDeployment({
      providerName: "basic",
      spec: { id: "deployment-1" } as any,
    });

    expect(applied.providerName).toBe("basic");
    expect(applied.deployment?.spec?.id).toBe("deployment-1");
    expect(seenTokens).toEqual(["relay-token-typescript"]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    } else {
      process.env[ENV_WORKFLOW_MANAGER_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN];
    } else {
      process.env[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN] = previousToken;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
