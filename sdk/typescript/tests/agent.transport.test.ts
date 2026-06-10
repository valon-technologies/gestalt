import { create, toJson } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { expect, test } from "bun:test";

import {
  AgentTurnEventSchema,
  CreateAgentProviderSessionRequestSchema,
  CreateAgentProviderTurnRequestSchema,
  ListAgentProviderTurnEventsRequestSchema,
} from "../src/internal/gen/v1/agent_pb.ts";
import {
  RequestContextSchema,
  SubjectContextSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import {
  createAgentProviderService,
  defineAgentProvider,
} from "../src/index.ts";

test("AgentProvider accepts JSON display payloads for turn events", async () => {
  const provider = defineAgentProvider({
    async listTurnEvents() {
      return [
        {
          id: "event-1",
          turnId: "turn-1",
          seq: 1n,
          type: "tool.started",
          source: "fixture-agent",
          visibility: "public",
          display: {
            phase: "started",
            label: "Lookup fixture",
            ref: "call-1",
            action: "Running",
            format: "json",
            language: "json",
            input: {
              query: "fixture",
            },
            output: ["hit-1"],
            error: "none",
          },
        },
      ];
    },
  });
  const service = createAgentProviderService(provider);
  const response = await (service.listTurnEvents as any)(
    create(ListAgentProviderTurnEventsRequestSchema, {
      turnId: "turn-1",
      afterSeq: 0n,
      limit: 10,
    }),
  );

  const event = response.events[0]!;
  expect(event?.display?.input?.kind.case).toBe("structValue");
  expect(event?.display?.output?.kind.case).toBe("listValue");
  expect(event?.display?.error?.kind.case).toBe("stringValue");
  expect(toJson(AgentTurnEventSchema, event!)).toEqual({
    id: "event-1",
    turnId: "turn-1",
    seq: "1",
    type: "tool.started",
    source: "fixture-agent",
    visibility: "public",
    display: {
      phase: "started",
      label: "Lookup fixture",
      ref: "call-1",
      action: "Running",
      format: "json",
      language: "json",
      input: {
        query: "fixture",
      },
      output: ["hit-1"],
      error: "none",
    },
  });
});

test("AgentProvider rejects structured output without a schema", async () => {
  const provider = defineAgentProvider({
    async createTurn() {
      throw new Error("provider should not receive invalid output");
    },
  });
  const service = createAgentProviderService(provider);

  try {
    await (service.createTurn as any)(
      create(CreateAgentProviderTurnRequestSchema, {
        turnId: "turn-1",
        sessionId: "session-1",
        timeoutSeconds: 120,
        output: {
          kind: {
            case: "structured",
            value: {},
          },
        },
      }),
    );
    throw new Error("createTurn succeeded, want InvalidArgument");
  } catch (error) {
    expect(error).toBeInstanceOf(ConnectError);
    expect((error as ConnectError).code).toBe(Code.InvalidArgument);
  }
});

test("AgentProvider forwards request context subjects to handlers", async () => {
  const seenSubjectIds: string[] = [];
  const seenContextSubjectIds: string[] = [];
  const seenSessionTools: Array<{
    toolRefOperation?: string | undefined;
    listedToolMcpName?: string | undefined;
  }> = [];
  const provider = defineAgentProvider({
    createSession(request) {
      seenSubjectIds.push(request.subject?.id ?? "");
      seenContextSubjectIds.push(request.context?.subject?.id ?? "");
      seenSessionTools.push({
        toolRefOperation: request.tools?.catalog?.refs?.[0]?.operation,
        listedToolMcpName: request.tools?.catalog?.tools?.[0]?.mcpName,
      });
      return {
        id: "session-1",
        model: request.model,
      };
    },
    createTurn(request) {
      seenSubjectIds.push(request.subject?.id ?? "");
      seenContextSubjectIds.push(request.context?.subject?.id ?? "");
      return {
        id: request.turnId,
        sessionId: request.sessionId,
        model: request.model,
      };
    },
  });
  const service = createAgentProviderService(provider);

  await (service.createSession as any)(
    create(CreateAgentProviderSessionRequestSchema, {
      model: "gpt-test",
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "user:session",
        }),
      }),
      tools: {
        source: {
          case: "catalog",
          value: {
            refs: [{ app: "slack", operation: "chat.postMessage" }],
            tools: [{
              id: "tool-slack",
              mcpName: "slack__chat_post_message",
              title: "Send Slack message",
              description: "Post a Slack message",
              inputSchema: "{\"type\":\"object\"}",
              ref: { app: "slack", operation: "chat.postMessage" },
            }],
          },
        },
      },
    }),
  );
  await (service.createTurn as any)(
    create(CreateAgentProviderTurnRequestSchema, {
      turnId: "turn-1",
      sessionId: "session-1",
      model: "gpt-test",
      output: { kind: { case: "text", value: {} } },
      timeoutSeconds: 120,
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "user:turn",
        }),
      }),
    }),
  );

  expect(seenSubjectIds).toEqual(["user:session", "user:turn"]);
  expect(seenContextSubjectIds).toEqual(["user:session", "user:turn"]);
  expect(seenSessionTools).toEqual([{
    toolRefOperation: "chat.postMessage",
    listedToolMcpName: "slack__chat_post_message",
  }]);
});
