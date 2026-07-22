import { writeFileSync } from "node:fs";
import YAML from "yaml";

import type {
  WorkflowDefinitionSpec,
  WorkflowStep,
  WorkflowStepAppCall,
} from "./providers/workflow.ts";

type StaticWorkflowDefinitions = {
  definitions: Array<{
    id?: string;
    steps: Array<{
      app: string;
      operation?: string;
    }>;
  }>;
};

/**
 * Serializes workflow app-call steps for provider packaging.
 */
export function workflowsToYaml(
  specs: readonly WorkflowDefinitionSpec[],
): string {
  const definitions = specs
    .map((spec) => ({
      id: spec.id,
      steps: workflowAppCallsFromTarget(spec.target),
    }))
    .filter((definition) => definition.steps.length > 0);
  const payload: StaticWorkflowDefinitions = { definitions };
  return YAML.stringify(payload);
}

/**
 * Writes workflow app-call metadata to disk as YAML.
 */
export function writeWorkflowsYaml(
  path: string,
  specs: readonly WorkflowDefinitionSpec[],
): void {
  const yaml = workflowsToYaml(specs);
  if (!yaml.trim()) {
    return;
  }
  writeFileSync(path, yaml, "utf8");
}

function workflowAppCallsFromTarget(
  target: WorkflowDefinitionSpec["target"],
): Array<{ app: string; operation?: string }> {
  if (!target?.steps?.length) {
    return [];
  }
  const out: Array<{ app: string; operation?: string }> = [];
  for (const step of target.steps) {
    const appCall = workflowStepAppCall(step);
    if (!appCall?.name?.trim()) {
      continue;
    }
    out.push({
      app: appCall.name.trim(),
      operation: appCall.operation?.trim() || undefined,
    });
  }
  return out;
}

function workflowStepAppCall(step: WorkflowStep): WorkflowStepAppCall | undefined {
  if (step.app?.name) {
    return step.app;
  }
  if (step.action?.case === "app") {
    return step.action.value;
  }
  return undefined;
}
