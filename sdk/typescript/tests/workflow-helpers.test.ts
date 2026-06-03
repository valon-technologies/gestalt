import { expect, test } from "bun:test";

import {
  evaluateWorkflowValue,
  pathValue,
  parseWorkflowRunContext,
  renderWorkflowTemplate,
  workflowRunContext,
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
      "customer=${input.customer.id}; thread=${signal.payload.thread.ts}; input=${inputs.thread}; literal=$${x}",
    ),
  ).toBe("customer=cust_1; thread=123.456; input=123.456; literal=${x}");

  expect(evaluateWorkflowValue(ctx, { input: "customer.id" })).toEqual({
    value: "cust_1",
    ok: true,
  });
  expect(pathValue({ "quote'key": { value: 42 } }, "['quote\\'key'].value")).toEqual({
    value: 42,
    ok: true,
  });
});

test("workflow run context matches runtime shape", () => {
  const createdAt = new Date("2026-05-08T12:00:00.000Z");
  const ctx = workflowRunContext({
    providerName: "indexeddb",
    runId: "run-1",
    target: {
      steps: [{
        id: "notify",
        app: {
          name: "slack",
          operation: "chat.postMessage",
          credentialMode: "subject",
        },
      }],
    },
    trigger: { manual: true },
    createdBySubjectId: "user-1",
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
      credentialMode: "subject",
    }],
  });
  expect(ctx.trigger.kind).toBe("manual");
  expect(ctx.createdBySubjectId).toBe("user-1");
  expect(ctx.signals[0]?.id).toBe("sig-1");
  expect(ctx.signals[0]?.name).toBe("ready");
  expect(ctx.signals[0]?.payload).toEqual({
    delivery_id: "delivery-1",
    fields: { extra: "kept" },
    payloadOmitted: true,
  });
  expect(ctx.signals[0]?.metadata).toEqual({});
  expect(ctx.signals[0]?.createdAt).toBe("2026-05-08T12:00:00.000Z");
});

test("workflow run context parses request workflow metadata", () => {
  const ctx = parseWorkflowRunContext({
    workflow: {
      provider: "github",
      runId: "run-1",
      target: { kind: "steps", steps: [{ id: "review" }] },
      trigger: {
        kind: "schedule",
        activationId: "nightly",
        scheduledFor: "2026-05-08T12:00:00Z",
      },
      input: { repository: "valon/app" },
      metadata: { definitionId: "def-1" },
      createdBySubjectId: "user-1",
      signals: [
        "ignored",
        { id: "sig-1", name: "queued", payload: { state: "queued" } },
        {
          id: "sig-2",
          name: "github",
          payload: {
            github_event: "pull_request",
            delivery_id: "delivery-1",
            payloadOmitted: true,
          },
          metadata: { source: "webhook" },
          createdBySubjectId: "bot-1",
          createdAt: "2026-05-08T12:01:00Z",
          idempotencyKey: "idem-1",
          sequence: 2,
        },
      ],
    },
  });

  expect(ctx.provider).toBe("github");
  expect(ctx.runId).toBe("run-1");
  expect(ctx.target).toEqual({ kind: "steps", steps: [{ id: "review" }] });
  expect(ctx.trigger.kind).toBe("schedule");
  expect(ctx.trigger.activationId).toBe("nightly");
  expect(ctx.trigger.scheduledFor).toBe("2026-05-08T12:00:00Z");
  expect(ctx.input).toEqual({ repository: "valon/app" });
  expect(ctx.metadata).toEqual({ definitionId: "def-1" });
  expect(ctx.createdBySubjectId).toBe("user-1");
  expect(ctx.signals).toHaveLength(2);
  expect(ctx.latestSignal?.id).toBe("sig-2");
  expect(ctx.latestSignal?.payload.github_event).toBe("pull_request");
  expect(ctx.latestSignal?.metadata).toEqual({ source: "webhook" });
  expect(ctx.latestSignal?.createdBySubjectId).toBe("bot-1");
  expect(ctx.latestSignal?.sequence).toBe(2);
});

test("workflow run context parser tolerates malformed values", () => {
  const ctx = parseWorkflowRunContext({
    provider: 123,
    runId: null,
    target: [],
    trigger: {
      kind: "event",
      activationId: "github_pr",
      event: {
        type: "github.pull_request",
        specVersion: "1.0",
      },
    },
    input: "bad",
    metadata: ["bad"],
    createdBySubjectId: {},
    signals: [
      { sequence: true, payload: "bad", metadata: { ok: true } },
      null,
    ],
  });

  expect(ctx.provider).toBe("");
  expect(ctx.runId).toBe("");
  expect(ctx.target).toBeUndefined();
  expect(ctx.trigger.kind).toBe("event");
  expect(ctx.trigger.activationId).toBe("github_pr");
  expect(ctx.trigger.event).toEqual({
    type: "github.pull_request",
    specVersion: "1.0",
  });
  expect(ctx.input).toEqual({});
  expect(ctx.metadata).toEqual({});
  expect(ctx.createdBySubjectId).toBe("");
  expect(ctx.signals).toHaveLength(1);
  expect(ctx.signals[0]?.payload).toEqual({});
  expect(ctx.signals[0]?.metadata).toEqual({ ok: true });
  expect(ctx.signals[0]?.sequence).toBeUndefined();
});
