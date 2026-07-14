/**
 * Hand-written workflow service client extensions.
 *
 * @module
 */

import { fromWireWorkflowDefinitionSpec } from "./internal/codec/workflow.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
} from "./host-service.ts";
import {
  resolveWorkflowDefinitionSpec,
  WorkflowBuilder,
  type WorkflowDefinitionInput,
} from "./workflow-define.ts";
import type { WorkflowValue as ProviderWorkflowValue } from "./providers/workflow.ts";
import {
  Workflow as GeneratedWorkflow,
  type WorkflowDefinition,
  type WorkflowDefinitionSpec,
} from "./workflow.ts";
import type { Init } from "./rpc_support.ts";
import {
  workflowDefinitionSpecToProto,
  type WorkflowDefinitionSpec as BuilderWorkflowDefinitionSpec,
} from "./providers/workflow.ts";

export type { WorkflowDefinitionInput };

type WorkflowServiceDefinitionInput =
  | WorkflowDefinitionInput
  | Init<WorkflowDefinitionSpec>;

function toClientWorkflowDefinitionSpec(
  spec: BuilderWorkflowDefinitionSpec,
): import("./workflow.ts").WorkflowDefinitionSpec {
  const proto = workflowDefinitionSpecToProto(spec);
  if (proto === undefined) {
    throw new Error("workflow definition spec is required");
  }
  return fromWireWorkflowDefinitionSpec(proto);
}

function providerWorkflowValueNeedsBridge(value: ProviderWorkflowValue): boolean {
  const kind = value.kind;
  if (kind?.case === "input" || kind?.case === "signal") {
    return typeof kind.value === "string";
  }
  if (kind?.case === "object" && kind.value) {
    return Object.values(kind.value).some(providerWorkflowValueNeedsBridge);
  }
  return false;
}

function authoredWorkflowDefinitionSpecNeedsBridge(
  spec: BuilderWorkflowDefinitionSpec,
): boolean {
  for (const activation of spec.activations ?? []) {
    if (activation.input !== undefined && providerWorkflowValueNeedsBridge(activation.input)) {
      return true;
    }
  }
  for (const step of spec.target?.steps ?? []) {
    if (step.app?.input !== undefined && providerWorkflowValueNeedsBridge(step.app.input)) {
      return true;
    }
    if (step.when?.value !== undefined && providerWorkflowValueNeedsBridge(step.when.value)) {
      return true;
    }
  }
  return false;
}

function resolveApplyDefinitionSpec(
  spec: unknown,
): import("./workflow.ts").WorkflowDefinitionSpec | undefined {
  if (spec === undefined || spec === null) {
    return undefined;
  }
  if (spec instanceof WorkflowBuilder) {
    return toClientWorkflowDefinitionSpec(spec.toSpec());
  }
  if (
    typeof spec === "object" &&
    "toSpec" in spec &&
    typeof spec.toSpec === "function"
  ) {
    return toClientWorkflowDefinitionSpec(
      resolveWorkflowDefinitionSpec(spec as WorkflowDefinitionInput),
    );
  }
  if (
    typeof spec === "object" &&
    "id" in spec &&
    "runAs" in spec &&
    authoredWorkflowDefinitionSpecNeedsBridge(spec as BuilderWorkflowDefinitionSpec)
  ) {
    return toClientWorkflowDefinitionSpec(
      resolveWorkflowDefinitionSpec(spec as WorkflowDefinitionInput),
    );
  }
  return undefined;
}

/** Workflow client that accepts fluent workflow builders in applyDefinition. */
export class Workflow extends GeneratedWorkflow {
  static connect(
    options?: Parameters<typeof GeneratedWorkflow.connect>[0],
  ): Workflow {
    const { target, token } = requireHostServiceTarget("workflow");
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("workflow", target),
      hostServiceMetadataInterceptors(token, options?.name?.trim() ?? ""),
    );
    return new Workflow(transport, {
      context: options?.context,
      timeoutMs: options?.timeoutMs,
    });
  }

  override async applyDefinition(
    provider: string,
    idempotencyKey: string,
    spec?: WorkflowServiceDefinitionInput,
  ): Promise<WorkflowDefinition>;
  override async applyDefinition(
    provider: string,
    idempotencyKey: string,
    spec?: unknown,
  ): Promise<WorkflowDefinition> {
    const resolved = resolveApplyDefinitionSpec(spec);
    if (resolved !== undefined) {
      return super.applyDefinition(provider, idempotencyKey, resolved);
    }
    return super.applyDefinition(
      provider,
      idempotencyKey,
      spec as Init<WorkflowDefinitionSpec> | undefined,
    );
  }
}
