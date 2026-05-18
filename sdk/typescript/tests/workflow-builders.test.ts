import { expect, test } from "bun:test";

import {
  WorkflowRunStatus,
  boundWorkflowRun,
  boundWorkflowTarget,
  boundWorkflowTargetFromTarget,
  workflowSignal,
  workflowStepWhen,
  workflowStepWhenToProto,
  workflowValue,
  type WorkflowValue,
} from "../src/index.ts";

test("workflow builders accept native JSON objects and Dates", () => {
  const createdAt = new Date("2026-05-08T12:00:00.000Z");
  const target = boundWorkflowTarget({
    steps: [
      {
        id: "run",
        plugin: {
          name: "plugin",
          operation: "run",
          input: { literal: { ok: false, count: 0 } },
        },
      },
    ],
  });
  const signal = workflowSignal({
    name: "ready",
    payload: { ok: true, count: 1 },
    createdAt,
    sequence: 0,
  });
  const run = boundWorkflowRun({
    id: "run-1",
    status: WorkflowRunStatus.PENDING,
    target,
    trigger: { manual: true },
    createdAt,
  });

  expect(target.steps?.[0]?.plugin?.name).toBe("plugin");
  expect(target.steps?.[0]?.plugin?.input?.kind).toEqual({
    case: "literal",
    value: { ok: false, count: 0 },
  });
  expect(signal.payload).toEqual({ ok: true, count: 1 });
  expect(signal.sequence).toBe(0n);
  expect(run.createdAt?.toISOString()).toBe("2026-05-08T12:00:00.000Z");
});

test("workflow copy helpers do not alias nested payloads", () => {
  const target = boundWorkflowTarget({
    steps: [
      {
        id: "run",
        plugin: {
          name: "plugin",
          operation: "run",
          input: {
            object: {
              nested: { object: { value: { literal: "original" } } },
            },
          },
        },
      },
    ],
  });
  const copied = boundWorkflowTargetFromTarget(target);

  const nested = target.steps?.[0]?.plugin?.input?.kind;
  if (nested?.case !== "object") {
    throw new Error("expected object input");
  }
  const inner = nested.value.nested?.kind;
  if (inner?.case !== "object") {
    throw new Error("expected nested object input");
  }
  inner.value.value = { kind: { case: "literal", value: "changed" } };

  const copiedNested = copied.steps?.[0]?.plugin?.input?.kind;
  if (copiedNested?.case !== "object") {
    throw new Error("expected copied object input");
  }
  expect(copiedNested.value.nested?.kind).toEqual({
    case: "object",
    value: { value: { kind: { case: "literal", value: "original" } } },
  });
});

test("workflow values preserve null literals and empty collections", () => {
  const value = workflowValue({
    object: {
      emptyObject: { object: {} },
      emptyArray: { array: [] },
      nullLiteral: { literal: null },
      condition: {
        stepOutput: {
          stepId: "diagnosis",
          path: "agent.structuredOutput.actionableForPr",
        },
      },
    },
  });

  const object = value.kind;
  if (object?.case !== "object") {
    throw new Error("expected object value");
  }
  expect(object.value.emptyObject?.kind).toEqual({ case: "object", value: {} });
  expect(object.value.emptyArray?.kind).toEqual({ case: "array", value: [] });
  expect(object.value.nullLiteral?.kind).toEqual({ case: "literal", value: null });
  expect(object.value.condition?.kind).toEqual({
    case: "stepOutput",
    value: {
      stepId: "diagnosis",
      path: "agent.structuredOutput.actionableForPr",
    },
  });
});

test("workflow step when preserves omitted and explicit null equals", () => {
  const omitted = workflowStepWhen({ value: { literal: true } });
  expect(Object.prototype.hasOwnProperty.call(omitted, "equals")).toBe(false);
  expect(workflowStepWhenToProto(omitted)?.equals).toBeUndefined();

  const explicitNull = workflowStepWhen({
    value: { literal: true },
    equals: null,
  });
  expect(Object.prototype.hasOwnProperty.call(explicitNull, "equals")).toBe(true);
  expect(workflowStepWhenToProto(explicitNull)?.equals?.kind.case).toBe("nullValue");
});

test("agent and plugin workflow steps round-trip through copy helpers", () => {
  const target = boundWorkflowTarget({
    steps: [
      {
        id: "diagnosis",
        inputs: {
          thread: { signalPayload: "event.thread_ts" },
        },
        agent: {
          provider: "agent",
          model: "claude",
          prompt: "Diagnose the alert.",
          messages: [{ role: "system", text: "Use concise replies." }],
          tools: [{ plugin: "datadog", operation: "queryLogs" }],
          responseSchema: { type: "object" },
          modelOptions: { temperature: 0 },
        },
        timeoutSeconds: 45,
        metadata: { kind: "diagnosis" },
        outputDelivery: {
          plugin: {
            name: "slack",
            operation: "reply",
            input: {
              object: {
                text: { stepOutput: { stepId: "diagnosis", path: "agent.text" } },
              },
            },
          },
        },
      },
      {
        id: "pr_fix",
        agent: {
          provider: "agent",
          prompt: "Open a PR.",
          tools: [{ plugin: "github", operation: "createPullRequest" }],
        },
        when: {
          value: {
            stepOutput: {
              stepId: "diagnosis",
              path: "agent.structuredOutput.actionableForPr",
            },
          },
          equals: true,
        },
      },
    ],
  });

  const steps = target.steps ?? [];
  expect(steps).toHaveLength(2);
  expect(steps[0]?.agent?.tools?.[0]?.plugin).toBe("datadog");
  expect((steps[1]?.when?.value as WorkflowValue).kind?.case).toBe("stepOutput");
  expect(steps[1]?.when?.equals).toBe(true);

  const copied = boundWorkflowTargetFromTarget(target);
  const copiedSteps = copied.steps ?? [];
  expect(copiedSteps[0]?.agent?.responseSchema).toEqual({ type: "object" });
  expect(copiedSteps[0]?.outputDelivery?.plugin?.name).toBe("slack");
  expect(copiedSteps[1]?.when?.value?.kind).toEqual({
    case: "stepOutput",
    value: {
      stepId: "diagnosis",
      path: "agent.structuredOutput.actionableForPr",
    },
  });
});
