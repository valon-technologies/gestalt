import { mkdtempSync } from "node:fs";
import { createServer as createHTTPServer, type IncomingMessage } from "node:http";
import { createServer } from "node:http2";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { create } from "@bufbuild/protobuf";
import { type ServiceImpl } from "@connectrpc/connect";
import { connectNodeAdapter } from "@connectrpc/connect-node";
import { expect, test } from "bun:test";

import {
  AgentHost as AgentHostService,
  AgentProvider as AgentProviderService,
  AgentRunStatus,
  ExecuteAgentToolResponseSchema,
  GetAgentProviderRunRequestSchema,
  StartAgentProviderRunRequestSchema,
} from "../gen/v1/agent_pb.ts";
import {
  ENV_AGENT_HOST_SOCKET,
  createAgentProviderService,
  isAgentProvider,
  loadProviderFromTarget,
} from "../src/index.ts";
import { createUnixGrpcClient, removeTempDir } from "./helpers.ts";

test("OpenAI agent provider runs the Responses tool loop through host callbacks", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-openai-agent-"));
  const hostSocketPath = join(tempDir, "agent-host.sock");
  const providerSocketPath = join(tempDir, "agent-provider.sock");
  const previousHostSocket = process.env[ENV_AGENT_HOST_SOCKET];
  const openAIRequests: Array<{
    path: string;
    authorization: string | undefined;
    body: Record<string, unknown>;
  }> = [];
  const toolCalls: Array<{
    runId: string;
    toolCallId: string;
    toolId: string;
    arguments: unknown;
  }> = [];
  const events: Array<{
    runId: string;
    type: string;
    data: unknown;
  }> = [];
  const provider = await loadProviderFromTarget(
    resolve(import.meta.dir, "../../../providers/agent/openai"),
  );
  if (!isAgentProvider(provider)) {
    throw new Error("OpenAI package did not load an agent provider");
  }

  const openAIServer = createHTTPServer(async (request, response) => {
    const body = JSON.parse(await readRequestBody(request)) as Record<string, unknown>;
    openAIRequests.push({
      path: request.url ?? "",
      authorization: request.headers.authorization,
      body,
    });

    response.writeHead(200, { "Content-Type": "application/json" });
    switch (openAIRequests.length) {
    case 1:
      response.end(
        JSON.stringify({
          id: "resp-tool",
          object: "response",
          status: "completed",
          output: [
            {
              id: "fc_1",
              type: "function_call",
              call_id: "call_lookup",
              name: "lookup_status",
              arguments: JSON.stringify({ ticket: "INC-1" }),
              status: "completed",
            },
          ],
        }),
      );
      return;
    case 2:
      response.end(
        JSON.stringify({
          id: "resp-final",
          object: "response",
          status: "completed",
          output_text: "Deployment is healthy.",
          output: [
            {
              id: "msg_1",
              type: "message",
              status: "completed",
              role: "assistant",
              content: [
                {
                  type: "output_text",
                  text: "Deployment is healthy.",
                  annotations: [],
                },
              ],
            },
          ],
        }),
      );
      return;
    default:
      response.end(
        JSON.stringify({
          id: "resp-followup",
          object: "response",
          status: "completed",
          output_text: "Still healthy.",
          output: [
            {
              id: "msg_2",
              type: "message",
              status: "completed",
              role: "assistant",
              content: [
                {
                  type: "output_text",
                  text: "Still healthy.",
                  annotations: [],
                },
              ],
            },
          ],
        }),
      );
      return;
    }
  });

  const hostHandler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentHostService, {
        async executeTool(input) {
          toolCalls.push({
            runId: input.runId,
            toolCallId: input.toolCallId,
            toolId: input.toolId,
            arguments: input.arguments,
          });
          return create(ExecuteAgentToolResponseSchema, {
            status: 200,
            body: "",
          });
        },
        async emitEvent(input) {
          events.push({
            runId: input.runId,
            type: input.type,
            data: input.data,
          });
          return {};
        },
      } satisfies Partial<ServiceImpl<typeof AgentHostService>>);
    },
  });
  const hostServer = createServer(hostHandler);

  const providerHandler = connectNodeAdapter({
    grpc: true,
    grpcWeb: false,
    connect: false,
    routes(router) {
      router.service(AgentProviderService, createAgentProviderService(provider));
    },
  });
  const providerServer = createServer(providerHandler);

  try {
    await listenHTTP(openAIServer);
    await listenUnix(hostServer, hostSocketPath);
    process.env[ENV_AGENT_HOST_SOCKET] = hostSocketPath;

    const address = openAIServer.address();
    if (!address || typeof address === "string") {
      throw new Error("OpenAI test server did not bind to TCP");
    }
    await provider.configureProvider("openai", {
      apiKey: "test-api-key",
      baseURL: `http://127.0.0.1:${address.port}/v1`,
      model: "gpt-config",
      timeoutMs: 5_000,
    });
    await listenUnix(providerServer, providerSocketPath);

    const client = createUnixGrpcClient(AgentProviderService, providerSocketPath);
    const started = await client.startRun(
      create(StartAgentProviderRunRequestSchema, {
        runId: "run-123",
        providerName: "openai",
        model: "gpt-test",
        messages: [
          {
            role: "system",
            text: "Be concise.",
          },
          {
            role: "user",
            text: "Check deployment status for INC-1.",
          },
        ],
        tools: [
          {
            id: "roadmap/lookup-status",
            name: "lookup_status",
            description: "Look up deployment status.",
            parametersSchema: {
              type: "object",
              properties: {
                ticket: { type: "string" },
              },
              required: ["ticket"],
            },
          },
        ],
        providerOptions: {
          temperature: 0.2,
          metadata: {
            source: "transport-test",
          },
        },
        sessionRef: "session-123",
        executionRef: "exec-123",
      }),
    );
    expect(started.status).toBe(AgentRunStatus.RUNNING);

    const run = await waitForRunStatus(client, "run-123", AgentRunStatus.SUCCEEDED);
    expect(run.id).toBe("run-123");
    expect(run.status).toBe(AgentRunStatus.SUCCEEDED);
    expect(run.providerName).toBe("openai");
    expect(run.model).toBe("gpt-test");
    expect(run.outputText).toBe("Deployment is healthy.");

    expect(toolCalls).toEqual([
      {
        runId: "run-123",
        toolCallId: "call_lookup",
        toolId: "roadmap/lookup-status",
        arguments: { ticket: "INC-1" },
      },
    ]);

    expect(openAIRequests).toHaveLength(2);
    expect(openAIRequests[0]?.path).toBe("/v1/responses");
    expect(openAIRequests[0]?.authorization).toBe("Bearer test-api-key");
    expect(openAIRequests[0]?.body).toMatchObject({
      model: "gpt-test",
      input: [
        { role: "system", content: "Be concise." },
        { role: "user", content: "Check deployment status for INC-1." },
      ],
      tool_choice: "auto",
      temperature: 0.2,
      metadata: {
        source: "transport-test",
      },
    });
    expect(openAIRequests[0]?.body.tools).toEqual([
      {
        type: "function",
        name: "lookup_status",
        description: "Look up deployment status.",
        parameters: {
          type: "object",
          properties: {
            ticket: { type: "string" },
          },
          required: ["ticket"],
        },
        strict: false,
      },
    ]);
    expect(openAIRequests[1]?.body).toMatchObject({
      model: "gpt-test",
      previous_response_id: "resp-tool",
      input: [
        {
          type: "function_call_output",
          call_id: "call_lookup",
          output: "",
        },
      ],
    });

    expect(events.map((event) => event.type)).toEqual([
      "agent.run.started",
      "agent.model.response",
      "agent.tool_call.started",
      "agent.tool_call.completed",
      "agent.model.response",
      "agent.run.completed",
    ]);
    expect(events.every((event) => event.runId === "run-123")).toBe(true);

    await client.startRun(
      create(StartAgentProviderRunRequestSchema, {
        runId: "run-456",
        providerName: "openai",
        model: "gpt-test",
        messages: [
          {
            role: "user",
            text: "Check it again.",
          },
        ],
        sessionRef: "session-123",
        executionRef: "exec-456",
      }),
    );
    const followup = await waitForRunStatus(client, "run-456", AgentRunStatus.SUCCEEDED);
    expect(followup.outputText).toBe("Still healthy.");
    expect(openAIRequests[2]?.body).toMatchObject({
      model: "gpt-test",
      previous_response_id: "resp-final",
      input: [{ role: "user", content: "Check it again." }],
    });
  } finally {
    if (previousHostSocket === undefined) {
      delete process.env[ENV_AGENT_HOST_SOCKET];
    } else {
      process.env[ENV_AGENT_HOST_SOCKET] = previousHostSocket;
    }
    await closeServer(providerServer);
    await closeServer(hostServer);
    await closeServer(openAIServer);
    removeTempDir(tempDir);
  }
});

test("OpenAI agent provider fails structured runs when output is not a JSON object", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "gts-openai-structured-"));
  const hostSocketPath = join(tempDir, "agent-host.sock");
  const providerSocketPath = join(tempDir, "agent-provider.sock");
  const previousHostSocket = process.env[ENV_AGENT_HOST_SOCKET];
  const events: string[] = [];
  const provider = await loadProviderFromTarget(
    resolve(import.meta.dir, "../../../providers/agent/openai"),
  );
  if (!isAgentProvider(provider)) {
    throw new Error("OpenAI package did not load an agent provider");
  }

  const openAIServer = createHTTPServer(async (_request, response) => {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        id: "resp-invalid-json",
        object: "response",
        status: "completed",
        output_text: "not json",
      }),
    );
  });
  const hostServer = createServer(
    connectNodeAdapter({
      grpc: true,
      grpcWeb: false,
      connect: false,
      routes(router) {
        router.service(AgentHostService, {
          async emitEvent(input) {
            events.push(input.type);
            return {};
          },
        } satisfies Partial<ServiceImpl<typeof AgentHostService>>);
      },
    }),
  );
  const providerServer = createServer(
    connectNodeAdapter({
      grpc: true,
      grpcWeb: false,
      connect: false,
      routes(router) {
        router.service(AgentProviderService, createAgentProviderService(provider));
      },
    }),
  );

  try {
    await listenHTTP(openAIServer);
    await listenUnix(hostServer, hostSocketPath);
    process.env[ENV_AGENT_HOST_SOCKET] = hostSocketPath;

    const address = openAIServer.address();
    if (!address || typeof address === "string") {
      throw new Error("OpenAI test server did not bind to TCP");
    }
    await provider.configureProvider("openai", {
      apiKey: "test-api-key",
      baseURL: `http://127.0.0.1:${address.port}/v1`,
      timeoutMs: 5_000,
    });
    await listenUnix(providerServer, providerSocketPath);

    const client = createUnixGrpcClient(AgentProviderService, providerSocketPath);
    const started = await client.startRun(
      create(StartAgentProviderRunRequestSchema, {
        runId: "run-structured",
        providerName: "openai",
        messages: [{ role: "user", text: "Return a summary." }],
        responseSchema: {
          type: "object",
          properties: { summary: { type: "string" } },
          required: ["summary"],
        },
      }),
    );
    expect(started.status).toBe(AgentRunStatus.RUNNING);

    const failed = await waitForRunStatus(
      client,
      "run-structured",
      AgentRunStatus.FAILED,
    );
    expect(failed.statusMessage).toContain("parse JSON");
    expect(events).toContain("agent.run.failed");
  } finally {
    if (previousHostSocket === undefined) {
      delete process.env[ENV_AGENT_HOST_SOCKET];
    } else {
      process.env[ENV_AGENT_HOST_SOCKET] = previousHostSocket;
    }
    await closeServer(providerServer);
    await closeServer(hostServer);
    await closeServer(openAIServer);
    removeTempDir(tempDir);
  }
});

async function readRequestBody(request: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) {
    chunks.push(Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function listenHTTP(server: ReturnType<typeof createHTTPServer>): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
}

async function listenUnix(server: ReturnType<typeof createServer>, socketPath: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(socketPath, () => {
      server.off("error", reject);
      resolve();
    });
  });
}

async function closeServer(server: { listening: boolean; close: (cb: () => void) => void }): Promise<void> {
  if (!server.listening) {
    return;
  }
  await new Promise<void>((resolve) => {
    server.close(() => resolve());
  });
}

async function waitForRunStatus(
  client: ReturnType<typeof createUnixGrpcClient<typeof AgentProviderService>>,
  runId: string,
  status: AgentRunStatus,
) {
  const deadline = Date.now() + 2_000;
  let lastRun;
  while (Date.now() < deadline) {
    lastRun = await client.getRun(create(GetAgentProviderRunRequestSchema, { runId }));
    if (lastRun.status === status) {
      return lastRun;
    }
    await Bun.sleep(10);
  }
  throw new Error(`timed out waiting for ${runId} status ${status}; last=${lastRun?.status}`);
}
