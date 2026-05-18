import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";

import { create } from "@bufbuild/protobuf";
import { createClient, type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter, createGrpcTransport } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  InvokeWorkflowOperationResponseSchema,
  PublishWorkflowProviderEventRequestSchema,
  StartWorkflowProviderRunRequestSchema,
  WorkflowHost as WorkflowHostService,
  WorkflowProvider as WorkflowProviderService,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
  ENV_WORKFLOW_HOST_SOCKET,
  ENV_WORKFLOW_HOST_SOCKET_TOKEN,
  WorkflowHost,
  WorkflowRunStatus,
  createWorkflowProviderService,
  defineWorkflowProvider,
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

test("WorkflowProvider service converts transport messages to native callbacks", async () => {
  const address = await reserveTCPAddress();
  const calls: Array<{ method: string; detail: string }> = [];
  const provider = defineWorkflowProvider({
    displayName: "Workflow transport fixture",
    async startRun(request) {
      const firstStep = request.target?.steps?.[0];
      const detail = firstStep?.plugin?.operation ?? "";
      calls.push({ method: "start-run", detail });
      return {
        id: request.idempotencyKey,
        status: WorkflowRunStatus.PENDING,
        target: request.target,
        statusMessage: "",
        resultBody: "",
        executionRef: request.executionRef,
        workflowKey: request.workflowKey,
      };
    },
    async getRun(request) {
      return {
        id: request.runId,
        status: WorkflowRunStatus.RUNNING,
        statusMessage: "",
        resultBody: "",
        executionRef: "",
        workflowKey: "",
      };
    },
    async listRuns() {
      return [];
    },
    async cancelRun(request) {
      return {
        id: request.runId,
        status: WorkflowRunStatus.CANCELED,
        statusMessage: request.reason,
        resultBody: "",
        executionRef: "",
        workflowKey: "",
      };
    },
    async signalRun(request) {
      return {
        run: {
          id: request.runId,
          status: WorkflowRunStatus.RUNNING,
          statusMessage: "",
          resultBody: "",
          executionRef: "",
          workflowKey: "",
        },
        signal: request.signal,
        startedRun: false,
        workflowKey: "",
      };
    },
    async signalOrStartRun(request) {
      return {
        run: {
          id: request.workflowKey,
          status: WorkflowRunStatus.PENDING,
          target: request.target,
          statusMessage: "",
          resultBody: "",
          executionRef: request.executionRef,
          workflowKey: request.workflowKey,
        },
        signal: request.signal,
        startedRun: true,
        workflowKey: request.workflowKey,
      };
    },
    async upsertSchedule(request) {
      return {
        id: request.scheduleId,
        cron: request.cron,
        timezone: request.timezone,
        paused: request.paused,
        executionRef: request.executionRef,
      };
    },
    async getSchedule(request) {
      return { id: request.scheduleId, cron: "", timezone: "", paused: false, executionRef: "" };
    },
    async listSchedules() {
      return [];
    },
    async deleteSchedule() {},
    async pauseSchedule(request) {
      return { id: request.scheduleId, cron: "", timezone: "", paused: true, executionRef: "" };
    },
    async resumeSchedule(request) {
      return { id: request.scheduleId, cron: "", timezone: "", paused: false, executionRef: "" };
    },
    async upsertEventTrigger(request) {
      return {
        id: request.triggerId,
        match: request.match,
        paused: request.paused,
        executionRef: request.executionRef,
      };
    },
    async getEventTrigger(request) {
      return { id: request.triggerId, paused: false, executionRef: "" };
    },
    async listEventTriggers() {
      return [];
    },
    async deleteEventTrigger() {},
    async pauseEventTrigger(request) {
      return { id: request.triggerId, paused: true, executionRef: "" };
    },
    async resumeEventTrigger(request) {
      return { id: request.triggerId, paused: false, executionRef: "" };
    },
    async publishEvent(request) {
      calls.push({ method: "publish-event", detail: request.event?.type ?? "" });
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
    const run = await client.startRun(create(StartWorkflowProviderRunRequestSchema, {
      idempotencyKey: "run-native-ts",
      target: {
        steps: [{
          id: "sync",
          action: {
            case: "plugin",
            value: { name: "demo", operation: "sync" },
          },
        }],
      },
    }));
    expect(run.id).toBe("run-native-ts");
    expect(run.status).toBe(WorkflowRunStatus.PENDING);

    await client.publishEvent(create(PublishWorkflowProviderEventRequestSchema, {
      pluginName: "demo",
      event: { type: "demo.synced" },
    }));
    expect(calls).toEqual([
      { method: "start-run", detail: "sync" },
      { method: "publish-event", detail: "demo.synced" },
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
        async invokeOperation(input) {
          return create(InvokeWorkflowOperationResponseSchema, {
            status: 202,
            body: input.runId,
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
    const response = await host.invokeOperation({ runId: "run-123" });

    expect(response.status).toBe(202);
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
