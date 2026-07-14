import { expect, test } from "bun:test";

import { fromWireWorkflowDefinitionSpec } from "../src/internal/codec/workflow.ts";
import {
  Workflow,
  defineWorkflow,
  event,
  schedule,
  text,
} from "../src/index.ts";
import { workflowDefinitionSpecToProto } from "../src/providers/workflow.ts";

test("workflow builder infers mapped inputs and accumulates typed step references", () => {
  const definition = defineWorkflow({
    id: "example",
    runAs: "service_account:workflow-runner",
  })
    .on(schedule("0 * * * *", (input) => ({ accountId: input.input.accountId })))
    .on(event("deal.updated", (eventInput) => ({ dealId: eventInput.data.dealId })))
    .step<"extract", { rowId: string }>("extract", {
      app: {
        name: "dealHub",
        operation: "extractRow",
        input: ({ input }) => ({ accountId: input.accountId }),
      },
    })
    .step("passThrough", {
      app: {
        name: "dealHub",
        operation: "passThrough",
        input: ({ input }) => input,
      },
    })
    .step("summarize", {
      agent: {
        provider: "openai",
        prompt: ({ steps }) => text("Extracted ", steps.extract.outputs.rowId),
      },
    });

  const spec = definition.toSpec();
  expect(spec.id).toBe("example");
  expect(spec.runAs).toBe("service_account:workflow-runner");
  expect(spec.activations).toHaveLength(2);
  expect(spec.activations?.[0]?.input?.kind).toEqual({
    case: "object",
    value: {
      accountId: { kind: { case: "input", value: "accountId" } },
    },
  });
  expect(spec.activations?.[1]?.input?.kind).toEqual({
    case: "object",
    value: {
      dealId: { kind: { case: "signal", value: "data.dealId" } },
    },
  });
  expect(spec.target?.steps).toHaveLength(3);
  expect(spec.target?.steps?.[1]?.app?.input?.kind).toEqual({ case: "input", value: "" });
  const prompt = spec.target?.steps?.[2]?.agent?.prompt;
  expect(typeof prompt === "object" && prompt !== null ? prompt.template : prompt).toBe(
    "Extracted ${{ steps.extract.outputs.rowId }}",
  );

  const applyBuilder = (workflow: Workflow) =>
    workflow.applyDefinition("provider", "workflow-definition-key", definition);
  void applyBuilder;
});

test("materialized workflow spec bridges to client path encoding", () => {
  const definition = defineWorkflow({
    id: "bridge-example",
    runAs: "service_account:workflow-runner",
  })
    .on(schedule("0 * * * *", (input) => ({ accountId: input.input.accountId })))
    .step("extract", {
      app: {
        name: "dealHub",
        operation: "extractRow",
        input: ({ input }) => ({ accountId: input.accountId }),
      },
    });

  const providerSpec = definition.toSpec();
  const clientSpec = fromWireWorkflowDefinitionSpec(
    workflowDefinitionSpecToProto(providerSpec)!,
  );

  expect(clientSpec.activations[0]?.input?.kind).toEqual({
    case: "object",
    value: {
      fields: {
        accountId: {
          kind: {
            case: "input",
            value: { path: "accountId" },
          },
        },
      },
    },
  });
  const stepInput =
    clientSpec.target?.steps?.[0]?.action?.case === "app"
      ? clientSpec.target.steps[0].action.value.input?.kind
      : undefined;
  expect(stepInput).toEqual({
    case: "object",
    value: {
      fields: {
        accountId: {
          kind: {
            case: "input",
            value: { path: "accountId" },
          },
        },
      },
    },
  });
});

test("workflow builder captures reference values in when guards", () => {
  let guardValue: unknown;
  const withSource = defineWorkflow({
    id: "when-example",
    runAs: "service_account:workflow-runner",
  }).step("source", {
    app: {
      name: "dealHub",
      operation: "extractRow",
      input: (scope) => {
        guardValue = scope.input.accountId;
        return { accountId: scope.input.accountId };
      },
    },
  });

  const spec = withSource
    .step("guard", {
      when: { value: guardValue, equals: "ready" },
    })
    .toSpec();

  expect(spec.target?.steps?.[1]?.when?.value?.kind).toEqual({
    case: "input",
    value: "accountId",
  });
});

test("workflow builder rejects both app and agent on the same step", () => {
  const definition = defineWorkflow({
    id: "invalid-step",
    runAs: "service_account:workflow-runner",
  });

  expect(() =>
    definition.step("broken", {
      app: {
        name: "dealHub",
        operation: "extractRow",
      },
      agent: {
        provider: "openai",
        prompt: "summarize",
      },
    }),
  ).toThrow("workflow step cannot configure both app and agent actions");
});
