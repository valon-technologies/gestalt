import { beforeEach, expect, test } from "bun:test";
import {
  metrics,
  type Attributes,
  type Histogram,
  type Meter,
  type MeterProvider,
} from "@opentelemetry/api";

import {
  GENAI_OPERATION_CHAT,
  withAgentInvocation,
  withModelOperation,
  withToolExecution,
} from "../src/telemetry.ts";

const histogramRecords: Array<{
  name: string;
  value: number;
  attributes: Attributes | undefined;
}> = [];

metrics.disable();
metrics.setGlobalMeterProvider({
  getMeter: () =>
    ({
      createHistogram: (name: string) =>
        ({
          record: (value: number, attributes?: Attributes) => {
            histogramRecords.push({ name, value, attributes });
          },
        }) as Histogram,
    }) as Meter,
} as MeterProvider);

beforeEach(() => {
  histogramRecords.length = 0;
});

test("GenAI telemetry helpers run callbacks", async () => {
  const result = await withModelOperation(
    {
      providerName: "openai",
      requestModel: "gpt-4.1",
      requestOptions: { max_tokens: 128, temperature: 0.2 },
      requestAttributes: { "gen_ai.request.service_tier": "default" },
    },
    (operation) => {
      operation.setResponseMetadata({
        id: "resp-123",
        model: "gpt-4.1",
        finishReasons: ["stop"],
      });
      operation.recordUsage({ inputTokens: 12, outputTokens: 34 });
      return GENAI_OPERATION_CHAT;
    },
  );

  expect(result).toBe("chat");
});

test("GenAI telemetry ignores negative token counts", async () => {
  await withModelOperation(
    {
      providerName: "openai",
      requestModel: "gpt-4.1",
    },
    (operation) => {
      operation.recordUsage({
        inputTokens: -12,
        outputTokens: 34,
        cacheCreationInputTokens: -1,
        cacheReadInputTokens: 0,
        reasoningOutputTokens: -5,
      });
    },
  );

  const tokenRecords = histogramRecords.filter(
    (record) => record.name === "gen_ai.client.token.usage",
  );
  expect(tokenRecords.map((record) => record.value)).toEqual([34]);
  expect(tokenRecords.every((record) => record.value >= 0)).toBe(true);
});

test("GenAI telemetry helpers mark failures", async () => {
  await expect(
    withAgentInvocation(
      {
        agentName: "simple",
        sessionId: "session-123",
        turnId: "turn-123",
        model: "claude-opus-4-1",
      },
      () => {
        throw new Error("agent failed");
      },
    ),
  ).rejects.toThrow("agent failed");

  const result = await withToolExecution(
    {
      toolName: "github.search",
      toolCallId: "call-123",
    },
    (operation) => {
      operation.markError("tool_error", "tool failed");
      return "done";
    },
  );

  expect(result).toBe("done");
});
