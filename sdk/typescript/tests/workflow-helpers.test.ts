import { expect, test } from "bun:test";

import {
  evaluateWorkflowValue,
  pathValue,
  renderWorkflowTemplate,
  workflowInvocationContext,
  type WorkflowEvalContext,
} from "../src/index.ts";

test("workflow execution helpers evaluate templates and paths", () => {
  const ctx: WorkflowEvalContext = {
    request: {
      providerName: "indexeddb",
      runId: "run-1",
      input: { customer: { id: "cust_1" } },
      signals: [{ id: "sig-1", payload: { thread: { ts: "123.456" } } }],
    },
    inputs: { thread: "123.456" },
    allowInputs: true,
  };

  expect(
    renderWorkflowTemplate(
      ctx,
      "customer=${runInput.customer.id}; thread=${signalPayload.thread.ts}; input=${inputs.thread}; literal=$${x}",
    ),
  ).toBe("customer=cust_1; thread=123.456; input=123.456; literal=${x}");

  expect(evaluateWorkflowValue(ctx, { runInput: "customer.id" })).toEqual({
    value: "cust_1",
    ok: true,
  });
  expect(pathValue({ "quote'key": { value: 42 } }, "['quote\\'key'].value")).toEqual({
    value: 42,
    ok: true,
  });
});

test("workflow invocation context matches runtime shape", () => {
  const createdAt = new Date("2026-05-08T12:00:00.000Z");
  const ctx = workflowInvocationContext({
    providerName: "indexeddb",
    runId: "run-1",
    target: {
      steps: [{
        id: "notify",
        app: {
          name: "slack",
          operation: "chat.postMessage",
          credentialMode: "user",
        },
      }],
    },
    trigger: { manual: true },
    createdBy: { subjectId: "user-1", subjectKind: "user" },
    signals: [{
      id: "sig-1",
      name: "ready",
      payload: {
        delivery_id: "delivery-1",
        payload: { large: true },
        extra: "kept",
      },
      createdAt,
    }],
  });

  expect(ctx.target).toEqual({
    kind: "steps",
    steps: [{
      id: "notify",
      kind: "app",
      app: "slack",
      operation: "chat.postMessage",
      credentialMode: "user",
    }],
  });
  expect(ctx.trigger).toEqual({ kind: "manual" });
  expect(ctx.createdBy).toEqual({ subjectId: "user-1", subjectKind: "user" });
  expect(ctx.signals).toEqual([{
    id: "sig-1",
    name: "ready",
    payload: {
      delivery_id: "delivery-1",
      fields: { extra: "kept" },
      payloadOmitted: true,
    },
    createdAt: "2026-05-08T12:00:00.000Z",
  }]);
});
