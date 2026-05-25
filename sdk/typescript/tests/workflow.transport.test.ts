import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";

import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { connectNodeAdapter, createGrpcTransport } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  PublishWorkflowProviderEventRequestSchema,
  StartWorkflowProviderRunRequestSchema,
  WorkflowProvider as WorkflowProviderService,
} from "../src/internal/gen/v1/workflow_pb.ts";
import {
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
    async createDefinition(request) {
      calls.push({ method: "create-definition", detail: request.createdBy?.subjectId ?? "" });
      return {
        id: request.idempotencyKey,
        target: request.target,
        createdBy: request.createdBy,
      };
    },
    async getDefinition(request) {
      return { id: request.definitionId };
    },
    async updateDefinition(request) {
      return {
        id: request.definitionId,
        target: request.target,
      };
    },
    async deleteDefinition() {},
    async startRun(request) {
      const firstStep = request.target?.steps?.[0];
      const detail = firstStep?.app?.operation ?? "";
      calls.push({ method: "start-run", detail });
      return {
        id: request.idempotencyKey,
        status: WorkflowRunStatus.PENDING,
        target: request.target,
        statusMessage: "",
        resultBody: "",
        createdBy: request.createdBy,
        workflowKey: request.workflowKey,
        definitionId: request.definitionId,
      };
    },
    async getRun(request) {
      return {
        id: request.runId,
        status: WorkflowRunStatus.RUNNING,
        statusMessage: "",
        resultBody: "",
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
          createdBy: request.createdBy,
          workflowKey: request.workflowKey,
          definitionId: request.definitionId,
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
        target: request.target,
        paused: request.paused,
        createdBy: request.requestedBy,
        definitionId: request.definitionId,
      };
    },
    async getSchedule(request) {
      return { id: request.scheduleId, cron: "", timezone: "", paused: false };
    },
    async listSchedules() {
      return [];
    },
    async deleteSchedule() {},
    async pauseSchedule(request) {
      return { id: request.scheduleId, cron: "", timezone: "", paused: true };
    },
    async resumeSchedule(request) {
      return { id: request.scheduleId, cron: "", timezone: "", paused: false };
    },
    async upsertEventTrigger(request) {
      return {
        id: request.triggerId,
        match: request.match,
        target: request.target,
        paused: request.paused,
        createdBy: request.requestedBy,
        definitionId: request.definitionId,
      };
    },
    async getEventTrigger(request) {
      return { id: request.triggerId, paused: false };
    },
    async listEventTriggers() {
      return [];
    },
    async deleteEventTrigger() {},
    async pauseEventTrigger(request) {
      return { id: request.triggerId, paused: true };
    },
    async resumeEventTrigger(request) {
      return { id: request.triggerId, paused: false };
    },
    async publishEvent(request) {
      calls.push({ method: "publish-event", detail: request.event?.type ?? "" });
      return { id: "published-ts", type: request.event?.type ?? "" };
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
            case: "app",
            value: { name: "demo", operation: "sync" },
          },
        }],
      },
    }));
    expect(run.id).toBe("run-native-ts");
    expect(run.status).toBe(WorkflowRunStatus.PENDING);

    const definition = await client.createDefinition({
      idempotencyKey: "definition-native-ts",
      createdBy: { subjectId: "user:ada" },
      target: {
        steps: [{
          id: "define",
          action: {
            case: "app",
            value: { name: "demo", operation: "define" },
          },
        }],
      },
    });
    expect(definition.id).toBe("definition-native-ts");
    expect(definition.createdBy?.subjectId).toBe("user:ada");

    const published = await client.publishEvent(create(PublishWorkflowProviderEventRequestSchema, {
      appName: "demo",
      event: { type: "demo.synced" },
    }));
    expect(published.id).toBe("published-ts");
    expect(calls).toEqual([
      { method: "start-run", detail: "sync" },
      { method: "create-definition", detail: "user:ada" },
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
