import { writeFileSync } from "node:fs";
import YAML from "yaml";

import type {
  WorkflowDefinitionSpec,
  WorkflowStep,
  WorkflowStepAppCall,
} from "./providers/workflow.ts";

type StaticWorkflowAppCall = {
  app: string;
  operation?: string;
};

type StaticWorkflowDefinition = {
  id?: string;
  steps: StaticWorkflowAppCall[];
};

type StaticWorkflowDefinitions = {
  definitions: StaticWorkflowDefinition[];
};

/**
 * Serializes workflow app-call steps for provider packaging.
 */
export function workflowsToYaml(
  specs: readonly WorkflowDefinitionSpec[],
): string {
  const definitions: StaticWorkflowDefinition[] = [];
  for (const spec of specs) {
    const steps = workflowAppCallsFromTarget(spec.target);
    if (steps.length === 0) {
      continue;
    }
    const definition: StaticWorkflowDefinition = { steps };
    if (spec.id) {
      definition.id = spec.id;
    }
    definitions.push(definition);
  }
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
): StaticWorkflowAppCall[] {
  if (!target?.steps?.length) {
    return [];
  }
  const out: StaticWorkflowAppCall[] = [];
  for (const step of target.steps) {
    const appCall = workflowStepAppCall(step);
    if (!appCall?.name?.trim()) {
      continue;
    }
    const entry: StaticWorkflowAppCall = { app: appCall.name.trim() };
    const operation = appCall.operation?.trim();
    if (operation) {
      entry.operation = operation;
    }
    out.push(entry);
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
