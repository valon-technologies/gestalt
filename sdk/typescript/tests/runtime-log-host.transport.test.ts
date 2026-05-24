import { createServer } from "node:http2";
import type { AddressInfo } from "node:net";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  AppendRuntimeLogsResponseSchema,
  type AppendRuntimeLogsRequest,
  RuntimeLogHost as RuntimeLogHostService,
  RuntimeLogStream,
} from "../src/internal/gen/v1/runtime_provider_pb.ts";
import {
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
  ENV_RUNTIME_SESSION_ID,
  RuntimeLogHost,
} from "../src/index.ts";

test("RuntimeLogHost appends logs and forwards relay token env", async () => {
  const previousSocket = process.env[ENV_HOST_SERVICE_SOCKET];
  const previousToken = process.env[ENV_HOST_SERVICE_TOKEN];
  const previousSession = process.env[ENV_RUNTIME_SESSION_ID];
  const calls: AppendRuntimeLogsRequest[] = [];
  const seenTokens: string[] = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(RuntimeLogHostService, {
        async appendLogs(input) {
          calls.push(input);
          return create(AppendRuntimeLogsResponseSchema, {
            lastSeq: input.logs.at(-1)?.sourceSeq ?? 0n,
          });
        },
      } satisfies Partial<ServiceImpl<typeof RuntimeLogHostService>>);
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

    process.env[ENV_HOST_SERVICE_SOCKET] = `tcp://127.0.0.1:${address.port}`;
    process.env[ENV_HOST_SERVICE_TOKEN] =
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
    await host.appendLogs({
      logs: [
        {
          sessionId: "runtime-session-batch",
          stream: "stdout",
          message: "batch line\n",
          observedAt,
          sourceSeq: 10n,
        },
      ],
    });
    await host.append({
      stream: "runtime",
      message: "pre-epoch\n",
      observedAt: new Date(-1),
      sourceSeq: 8n,
    });
    const writer = host.writer({
      stream: "stderr",
      sourceSeqStart: 10n,
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
      "relay-token-typescript",
    ]);
    expect(calls.map((call) => call.sessionId)).toEqual([
      "runtime-session-1",
      "runtime-session-batch",
      "runtime-session-1",
      "runtime-session-1",
    ]);
    expect(calls[0]?.logs[0]?.stream).toBe(RuntimeLogStream.RUNTIME);
    expect(calls[0]?.logs[0]?.message).toBe("runtime boot\n");
    expect(calls[0]?.logs[0]?.sourceSeq).toBe(7n);
    expect(calls[0]?.logs[0]?.observedAt?.seconds).toBe(1777550400n);
    expect(calls[1]?.logs[0]?.stream).toBe(RuntimeLogStream.STDOUT);
    expect(calls[1]?.logs[0]?.message).toBe("batch line\n");
    expect(calls[1]?.logs[0]?.sourceSeq).toBe(10n);
    expect(calls[2]?.logs[0]?.stream).toBe(RuntimeLogStream.RUNTIME);
    expect(calls[2]?.logs[0]?.message).toBe("pre-epoch\n");
    expect(calls[2]?.logs[0]?.sourceSeq).toBe(8n);
    expect(calls[2]?.logs[0]?.observedAt?.seconds).toBe(-1n);
    expect(calls[2]?.logs[0]?.observedAt?.nanos).toBe(999_000_000);
    expect(calls[3]?.logs[0]?.stream).toBe(RuntimeLogStream.STDERR);
    expect(calls[3]?.logs[0]?.message).toBe("stderr line\n");
    expect(calls[3]?.logs[0]?.sourceSeq).toBe(11n);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_HOST_SERVICE_SOCKET];
    } else {
      process.env[ENV_HOST_SERVICE_SOCKET] = previousSocket;
    }
    if (previousToken === undefined) {
      delete process.env[ENV_HOST_SERVICE_TOKEN];
    } else {
      process.env[ENV_HOST_SERVICE_TOKEN] = previousToken;
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
