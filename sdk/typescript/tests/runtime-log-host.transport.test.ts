import { createServer } from "node:http2";
import type { AddressInfo } from "node:net";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  AppendPluginRuntimeLogsResponseSchema,
  type AppendPluginRuntimeLogsRequest,
  PluginRuntimeLogHost as PluginRuntimeLogHostService,
  PluginRuntimeLogStream,
} from "../src/internal/gen/v1/pluginruntime_pb.ts";
import {
  ENV_RUNTIME_LOG_HOST_SOCKET,
  ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN,
  ENV_RUNTIME_SESSION_ID,
  RuntimeLogHost,
} from "../src/index.ts";

test("RuntimeLogHost appends logs and forwards relay token env", async () => {
  const previousSocket = process.env[ENV_RUNTIME_LOG_HOST_SOCKET];
  const previousToken = process.env[ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN];
  const previousSession = process.env[ENV_RUNTIME_SESSION_ID];
  const calls: AppendPluginRuntimeLogsRequest[] = [];
  const seenTokens: string[] = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(PluginRuntimeLogHostService, {
        async appendLogs(input) {
          calls.push(input);
          return create(AppendPluginRuntimeLogsResponseSchema, {
            lastSeq: input.logs.at(-1)?.sourceSeq ?? 0n,
          });
        },
      } satisfies Partial<ServiceImpl<typeof PluginRuntimeLogHostService>>);
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
      server.listen(0, "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });
    const address = server.address() as AddressInfo;

    process.env[ENV_RUNTIME_LOG_HOST_SOCKET] = `tcp://127.0.0.1:${address.port}`;
    process.env[ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN] =
      "relay-token-typescript";
    process.env[ENV_RUNTIME_SESSION_ID] = "runtime-session-1";

    const host = new RuntimeLogHost();
    const observedAt = new Date("2026-04-30T12:00:00.000Z");
    const appended = await host.append({
      stream: "runtime",
      message: "runtime boot\n",
      observedAt,
      sourceSeq: 7n,
    });
    await host.append({
      stream: "runtime",
      message: "pre-epoch\n",
      observedAt: new Date(-1),
      sourceSeq: 8n,
    });
    const writer = host.writer({
      stream: "stderr",
      sourceSeqStart: 8n,
    });
    await new Promise<void>((resolve, reject) => {
      writer.write("stderr line\n", (error) => {
        if (error) {
          reject(error);
          return;
        }
        resolve();
      });
    });
    writer.destroy();

    expect(appended.lastSeq).toBe(7n);
    expect(seenTokens).toEqual([
      "relay-token-typescript",
      "relay-token-typescript",
      "relay-token-typescript",
    ]);
    expect(calls.map((call) => call.sessionId)).toEqual([
      "runtime-session-1",
      "runtime-session-1",
      "runtime-session-1",
    ]);
    expect(calls[0]?.logs[0]?.stream).toBe(PluginRuntimeLogStream.RUNTIME);
    expect(calls[0]?.logs[0]?.message).toBe("runtime boot\n");
    expect(calls[0]?.logs[0]?.sourceSeq).toBe(7n);
    expect(calls[0]?.logs[0]?.observedAt?.seconds).toBe(1777550400n);
    expect(calls[1]?.logs[0]?.stream).toBe(PluginRuntimeLogStream.RUNTIME);
    expect(calls[1]?.logs[0]?.message).toBe("pre-epoch\n");
    expect(calls[1]?.logs[0]?.sourceSeq).toBe(8n);
    expect(calls[1]?.logs[0]?.observedAt?.seconds).toBe(-1n);
    expect(calls[1]?.logs[0]?.observedAt?.nanos).toBe(999_000_000);
    expect(calls[2]?.logs[0]?.stream).toBe(PluginRuntimeLogStream.STDERR);
    expect(calls[2]?.logs[0]?.message).toBe("stderr line\n");
    expect(calls[2]?.logs[0]?.sourceSeq).toBe(9n);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_RUNTIME_LOG_HOST_SOCKET];
    } else {
      process.env[ENV_RUNTIME_LOG_HOST_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN];
    } else {
      process.env[ENV_RUNTIME_LOG_HOST_SOCKET_TOKEN] = previousToken;
    }
    if (previousSession === undefined) {
      delete process.env[ENV_RUNTIME_SESSION_ID];
    } else {
      process.env[ENV_RUNTIME_SESSION_ID] = previousSession;
    }
    if (server.listening) {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  }
});
