/**
 * Hand-written workflow service client extensions.
 *
 * @module
 */

import { fromWireWorkflowDefinitionSpec } from "./internal/codec/workflow.ts";
import {
  resolveWorkflowDefinitionSpec,
  WorkflowBuilder,
  type WorkflowDefinitionInput,
} from "./workflow-define.ts";
import {
  Workflow as GeneratedWorkflow,
  type WorkflowDefinition,
} from "./workflow.ts";
import {
  workflowDefinitionSpecToProto,
  type WorkflowDefinitionSpec as BuilderWorkflowDefinitionSpec,
} from "./providers/workflow.ts";

export type { WorkflowDefinitionInput };

function toClientWorkflowDefinitionSpec(
  spec: BuilderWorkflowDefinitionSpec,
): import("./workflow.ts").WorkflowDefinitionSpec {
  const proto = workflowDefinitionSpecToProto(spec);
  if (proto === undefined) {
    throw new Error("workflow definition spec is required");
  }
  return fromWireWorkflowDefinitionSpec(proto);
}

/** Workflow client that accepts fluent workflow builders in applyDefinition. */
export class Workflow extends GeneratedWorkflow {
  override async applyDefinition(
    provider: string,
    idempotencyKey: string,
    spec?: import("./workflow.ts").WorkflowDefinitionSpec,
  ): Promise<WorkflowDefinition> {
    if (spec instanceof WorkflowBuilder) {
      const resolved = toClientWorkflowDefinitionSpec(resolveWorkflowDefinitionSpec(spec));
      return super.applyDefinition(provider, idempotencyKey, resolved);
    }
    if (
      spec !== undefined &&
      typeof spec === "object" &&
      "toSpec" in spec &&
      typeof spec.toSpec === "function"
    ) {
      const resolved = toClientWorkflowDefinitionSpec(
        resolveWorkflowDefinitionSpec(spec as WorkflowDefinitionInput),
      );
      return super.applyDefinition(provider, idempotencyKey, resolved);
    }
    return super.applyDefinition(provider, idempotencyKey, spec);
  }
}
