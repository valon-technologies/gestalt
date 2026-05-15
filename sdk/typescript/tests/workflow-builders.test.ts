import { expect, test } from "bun:test";

import {
  WorkflowRunStatus,
  boundWorkflowRun,
  boundWorkflowTarget,
  boundWorkflowTargetFromTarget,
  workflowSignal,
} from "../src/index.ts";

test("workflow builders accept native JSON objects and Dates", () => {
  const createdAt = new Date("2026-05-08T12:00:00.000Z");
  const target = boundWorkflowTarget({
    plugin: {
      pluginName: "plugin",
      operation: "run",
      input: { ok: false, count: 0 },
    },
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

  if (target.kind?.case !== "plugin") {
    throw new Error("expected plugin target");
  }
  expect(target.kind.value.input).toEqual({ ok: false, count: 0 });
  expect(signal.payload).toEqual({ ok: true, count: 1 });
  expect(signal.sequence).toBe(0n);
  expect(run.createdAt?.toISOString()).toBe("2026-05-08T12:00:00.000Z");
});

test("workflow copy helpers do not alias nested payloads", () => {
  const target = boundWorkflowTarget({
    plugin: {
      pluginName: "plugin",
      operation: "run",
      input: { nested: { value: "original" } },
    },
  });
  const copied = boundWorkflowTargetFromTarget(target);

  if (target.kind?.case !== "plugin" || copied.kind?.case !== "plugin") {
    throw new Error("expected plugin targets");
  }
  const input = target.kind.value.input as { nested: { value: string } };
  input.nested.value = "changed";

  expect((copied.kind.value.input as { nested: unknown }).nested).toEqual({
    value: "original",
  });
});

test("agent workflow tool refs carry runAs subjects", () => {
  const target = boundWorkflowTarget({
    agent: {
      providerName: "agent",
      toolRefs: [
        {
          plugin: "notion",
          operation: "search",
          runAs: {
            subjectId: "service_account:gestalt-support-notion",
            subjectKind: "service_account",
            credentialSubjectId: "service_account:notion-credential",
            displayName: "Gestalt Support Notion",
            authSource: "notion_service_account",
          },
          runAsExternalIdentity: {
            type: "notion_workspace",
            id: "valon-support",
          },
        },
      ],
    },
  });

  if (target.kind?.case !== "agent") {
    throw new Error("expected agent target");
  }
  const refs = target.kind.value.toolRefs ?? [];
  expect(refs).toHaveLength(1);
  const ref = refs[0];
  if (ref === undefined) {
    throw new Error("expected agent tool ref");
  }
  expect(ref.runAs?.subjectId).toBe("service_account:gestalt-support-notion");
  expect(ref.runAsExternalIdentity?.id).toBe("valon-support");

  const copied = boundWorkflowTargetFromTarget(target);
  if (copied.kind?.case !== "agent") {
    throw new Error("expected copied agent target");
  }
  const copiedRefs = copied.kind.value.toolRefs ?? [];
  expect(copiedRefs).toHaveLength(1);
  const copiedRef = copiedRefs[0];
  if (copiedRef === undefined) {
    throw new Error("expected copied agent tool ref");
  }
  expect(copiedRef.runAs?.displayName).toBe("Gestalt Support Notion");
  expect(copiedRef.runAsExternalIdentity?.type).toBe("notion_workspace");
});
