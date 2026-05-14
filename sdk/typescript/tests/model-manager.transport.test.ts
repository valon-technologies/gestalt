import { mkdtempSync } from "node:fs";
import { createServer } from "node:http2";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  GenerateModelResponseSchema,
  ModelManagerHost as ModelManagerHostService,
} from "../src/internal/gen/v1/model_pb.ts";
import {
  ENV_MODEL_MANAGER_SOCKET,
  ENV_MODEL_MANAGER_SOCKET_TOKEN,
  ModelManager,
  request,
} from "../src/index.ts";
import { removeTempDir } from "./helpers.ts";

test("ModelManager forwards invocation token and structured model requests", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-model-manager-"));
  const socketPath = join(tempDir, "model-manager.sock");
  const previousSocket = process.env[ENV_MODEL_MANAGER_SOCKET];
  const previousRelayToken = process.env[ENV_MODEL_MANAGER_SOCKET_TOKEN];
  const calls: Array<{
    invocationToken: string;
    relayToken: string;
    providerName: string;
    model: string;
    prompt: string;
  }> = [];

  const handler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(ModelManagerHostService, {
        async generate(input, context) {
          calls.push({
            invocationToken: input.invocationToken,
            relayToken:
              context.requestHeader.get("x-gestalt-host-service-relay-token") ?? "",
            providerName: input.providerName,
            model: input.model,
            prompt: input.messages[0]?.text ?? "",
          });
          return create(GenerateModelResponseSchema, {
            outputText: "graded",
            structuredOutput: { score: 1, reasoning: "matches" },
            finishReason: "tool_use",
            usage: {
              inputTokens: 3n,
              outputTokens: 2n,
              totalTokens: 5n,
            },
          });
        },
      } satisfies Partial<ServiceImpl<typeof ModelManagerHostService>>);
    },
  });
  const server = createServer(handler);
  await new Promise<void>((resolve, reject) => {
    const netServer = createNetServer();
    netServer.once("error", reject);
    netServer.listen(socketPath, () => {
      netServer.close();
      server.listen(socketPath, resolve);
    });
  });

  process.env[ENV_MODEL_MANAGER_SOCKET] = socketPath;
  process.env[ENV_MODEL_MANAGER_SOCKET_TOKEN] = "relay-token-model";
  try {
    const manager = new ModelManager(
      request("", {}, {}, {}, {}, {}, "invocation-token-model"),
    );
    const response = await manager.generate({
      providerName: "anthropic",
      model: "claude-test",
      messages: [{ role: "user", text: "grade question" }],
      responseSchema: {
        type: "object",
        required: ["score", "reasoning"],
        properties: {
          score: { enum: [0, 0.5, 1] },
          reasoning: { type: "string" },
        },
      },
    });
    expect(response.structuredOutput).toEqual({ score: 1, reasoning: "matches" });
    expect(response.usage?.totalTokens).toBe(5n);
    expect(calls).toEqual([
      {
        invocationToken: "invocation-token-model",
        relayToken: "relay-token-model",
        providerName: "anthropic",
        model: "claude-test",
        prompt: "grade question",
      },
    ]);
  } finally {
    if (previousSocket === undefined) {
      delete process.env[ENV_MODEL_MANAGER_SOCKET];
    } else {
      process.env[ENV_MODEL_MANAGER_SOCKET] = previousSocket;
    }
    if (previousRelayToken === undefined) {
      delete process.env[ENV_MODEL_MANAGER_SOCKET_TOKEN];
    } else {
      process.env[ENV_MODEL_MANAGER_SOCKET_TOKEN] = previousRelayToken;
    }
    await new Promise<void>((resolve) => server.close(() => resolve()));
    removeTempDir(tempDir);
  }
});
