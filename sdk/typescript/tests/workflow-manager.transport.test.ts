import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  BoundWorkflowScheduleSchema,
  ManagedWorkflowScheduleSchema,
  WorkflowManagerHost as WorkflowManagerHostService,
} from "../gen/v1/workflow_pb.ts";
import {
  ENV_WORKFLOW_MANAGER_SOCKET,
  WorkflowManager,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("WorkflowManager forwards invocation tokens from strings and Request objects", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-workflow-manager-"));
  const socketPath = join(tempDir, "workflow-manager.sock");
  const previousSocket = process.env[ENV_WORKFLOW_MANAGER_SOCKET];
  const calls: Array<{ method: string; invocationToken: string; scheduleId?: string }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(
        WorkflowManagerHostService,
        {
          async createSchedule(input) {
            calls.push({
              method: "create",
              invocationToken: input.invocationToken,
            });
            return create(ManagedWorkflowScheduleSchema, {
              providerName: input.providerName || "basic",
              schedule: create(BoundWorkflowScheduleSchema, {
                id: "sched-1",
                cron: input.cron,
                timezone: input.timezone,
                paused: input.paused,
                ...(input.target ? { target: input.target } : {}),
              }),
            });
          },
          async getSchedule(input) {
            calls.push({
              method: "get",
              invocationToken: input.invocationToken,
              scheduleId: input.scheduleId,
            });
            return create(ManagedWorkflowScheduleSchema, {
              providerName: "basic",
              schedule: create(BoundWorkflowScheduleSchema, {
                id: input.scheduleId,
              }),
            });
          },
          async updateSchedule(input) {
            calls.push({
              method: "update",
              invocationToken: input.invocationToken,
              scheduleId: input.scheduleId,
            });
            return create(ManagedWorkflowScheduleSchema, {
              providerName: input.providerName || "basic",
              schedule: create(BoundWorkflowScheduleSchema, {
                id: input.scheduleId,
                cron: input.cron,
                timezone: input.timezone,
                paused: input.paused,
                ...(input.target ? { target: input.target } : {}),
              }),
            });
          },
          async deleteSchedule(input) {
            calls.push({
              method: "delete",
              invocationToken: input.invocationToken,
              scheduleId: input.scheduleId,
            });
            return create(EmptySchema, {});
          },
          async pauseSchedule(input) {
            calls.push({
              method: "pause",
              invocationToken: input.invocationToken,
              scheduleId: input.scheduleId,
            });
            return create(ManagedWorkflowScheduleSchema, {
              providerName: "basic",
              schedule: create(BoundWorkflowScheduleSchema, {
                id: input.scheduleId,
                paused: true,
              }),
            });
          },
          async resumeSchedule(input) {
            calls.push({
              method: "resume",
              invocationToken: input.invocationToken,
              scheduleId: input.scheduleId,
            });
            return create(ManagedWorkflowScheduleSchema, {
              providerName: "basic",
              schedule: create(BoundWorkflowScheduleSchema, {
                id: input.scheduleId,
                paused: false,
              }),
            });
          },
        } satisfies Partial<ServiceImpl<typeof WorkflowManagerHostService>>,
      );
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
    const created = await fromHandle.createSchedule({
      providerName: "basic",
      cron: "*/5 * * * *",
      timezone: "UTC",
      target: {
        pluginName: "roadmap",
        operation: "sync",
      },
      paused: false,
    });

    expect(created.providerName).toBe("basic");
    expect(created.schedule?.id).toBe("sched-1");

    const fromRequest = new WorkflowManager(
      request("tok", {}, {}, {}, {}, {}, "invocation-token-456"),
    );
    const fetched = await fromRequest.getSchedule({ scheduleId: "sched-1" });
    const updated = await fromRequest.updateSchedule({
      scheduleId: "sched-1",
      providerName: "secondary",
      cron: "0 * * * *",
      timezone: "America/New_York",
      target: {
        pluginName: "roadmap",
        operation: "status",
      },
      paused: true,
    });
    const paused = await fromRequest.pauseSchedule({ scheduleId: "sched-1" });
    const resumed = await fromRequest.resumeSchedule({ scheduleId: "sched-1" });
    await fromRequest.deleteSchedule({ scheduleId: "sched-1" });

    expect(fetched.schedule?.id).toBe("sched-1");
    expect(updated.providerName).toBe("secondary");
    expect(updated.schedule?.paused).toBe(true);
    expect(paused.schedule?.paused).toBe(true);
    expect(resumed.schedule?.paused).toBe(false);
    expect(calls).toEqual([
      { method: "create", invocationToken: "invocation-token-123" },
      { method: "get", invocationToken: "invocation-token-456", scheduleId: "sched-1" },
      { method: "update", invocationToken: "invocation-token-456", scheduleId: "sched-1" },
      { method: "pause", invocationToken: "invocation-token-456", scheduleId: "sched-1" },
      { method: "resume", invocationToken: "invocation-token-456", scheduleId: "sched-1" },
      { method: "delete", invocationToken: "invocation-token-456", scheduleId: "sched-1" },
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
