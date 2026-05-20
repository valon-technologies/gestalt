import { expect, test } from "bun:test";
import { create } from "@bufbuild/protobuf";

import {
  WorkflowActivationMode,
  WorkflowRunStatus,
  boundWorkflowTarget,
  workflowActivation,
  workflowDeploymentSpec,
  workflowLiteral,
  workflowObject,
  workflowRun,
  workflowRunInput,
  workflowSignal,
  workflowStep,
  workflowStepOutput,
  workflowStepPluginCall,
  workflowTemplate,
} from "../src/index.ts";
import {
  boundWorkflowTargetFromProto,
  boundWorkflowTargetToProto,
} from "../src/workflow.ts";
import { BoundWorkflowTargetSchema } from "../src/internal/gen/v1/workflow_pb.ts";

test("step workflow targets carry plugin actions and workflow values", () => {
  const step = workflowStep({
    id: "notify",
    inputs: {
      text: workflowTemplate("Roadmap item {{ run.input.item_id }} changed"),
      previous: workflowStepOutput("load", "body"),
    },
    action: {
      case: "plugin",
      value: workflowStepPluginCall({
        name: "slack",
        operation: "reply",
        input: workflowObject({
          channel: workflowLiteral("#ops"),
        }),
      }),
    },
  });

  const target = boundWorkflowTarget({ steps: [step] });

  expect(target.steps).toHaveLength(1);
  expect(target.steps[0]?.action.case).toBe("plugin");
  expect(target.steps[0]?.inputs["text"]?.kind.case).toBe("template");
  expect(target.steps[0]?.inputs["previous"]?.kind.case).toBe("stepOutput");
  expect(boundWorkflowTargetToProto(target).steps.map((candidate) => candidate.id)).toEqual(["notify"]);
});

test("deployment activations and runs use the new deployment API", () => {
  const target = boundWorkflowTarget({
    steps: [
      workflowStep({
        id: "sync",
        action: {
          case: "plugin",
          value: workflowStepPluginCall({
            name: "roadmap",
            operation: "sync",
            input: workflowRunInput("item"),
          }),
        },
      }),
    ],
  });
  const activation = workflowActivation({
    id: "manual",
    mode: WorkflowActivationMode.START,
    input: workflowRunInput(),
    kind: { case: "manual", value: {} },
  });
  const spec = workflowDeploymentSpec({
    id: "roadmap-sync",
    generation: 7n,
    target,
    activations: [activation],
    labels: { owner: "workflow-tests" },
  });
  const signal = workflowSignal({ name: "roadmap.changed", sequence: 0n });
  const run = workflowRun({
    id: "run-1",
    deploymentId: spec.id,
    deploymentGeneration: spec.generation,
    workflowKey: "roadmap:item-1",
    status: WorkflowRunStatus.PENDING,
  });

  expect(spec.target?.steps[0]?.id).toBe("sync");
  expect(spec.activations[0]?.kind.case).toBe("manual");
  expect(signal.name).toBe("roadmap.changed");
  expect(run.deploymentGeneration).toBe(7n);
});

test("workflow targets round-trip through proto helpers", () => {
  const step = workflowStep({
    id: "notify",
    action: {
      case: "plugin",
      value: workflowStepPluginCall({
        name: "slack",
        operation: "reply",
      }),
    },
  });

  const decoded = boundWorkflowTargetFromProto(
    create(BoundWorkflowTargetSchema, { steps: [step] }),
  );

  expect(decoded.steps.map((candidate) => candidate.id)).toEqual(["notify"]);
  expect(boundWorkflowTargetToProto(decoded).steps.map((candidate) => candidate.id)).toEqual(["notify"]);
});
