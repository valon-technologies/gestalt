import type { AgentOutput, AgentToolRef } from "../src/providers/agent.ts";
import type { JsonInput, JsonObjectInput } from "../src/protocol.ts";
import {
  defineWorkflow,
  event,
  schedule,
  type StepConfig,
  type StepRefProxy,
  type WorkflowBuilder,
} from "../src/workflow-define.ts";
import {
  workflowDefinitionSpec,
  workflowText,
  workflowValue,
  type WorkflowActivation,
  type WorkflowDefinitionSpec,
  type WorkflowStep,
  type WorkflowValue,
} from "../src/providers/workflow.ts";

const WORKFLOW_REF = Symbol.for("gestalt.workflow.ref");

type WorkflowRefKind = "input" | "signal" | "stepOutput" | "stepInput";

interface WorkflowRef {
  readonly [WORKFLOW_REF]: true;
  readonly kind: WorkflowRefKind;
  readonly path: string;
  readonly stepId?: string | undefined;
}

interface WorkflowLiteralRef {
  readonly [WORKFLOW_REF]: true;
  readonly kind: "literal";
  readonly value: JsonInput;
}

interface WorkflowTemplateRef {
  readonly [WORKFLOW_REF]: true;
  readonly kind: "template";
  readonly template: string;
}

type CapturedRef = WorkflowRef | WorkflowLiteralRef | WorkflowTemplateRef;
function createRefProxy(
  path: string,
  kind: WorkflowRefKind,
  stepId?: string,
): StepRefProxy {
  const marker: WorkflowRef = {
    [WORKFLOW_REF]: true,
    kind,
    path,
    stepId,
  };
  return new Proxy(marker, {
    get(target, prop, receiver) {
      if (prop === WORKFLOW_REF) {
        return true;
      }
      if (prop === "kind") {
        return target.kind;
      }
      if (prop === "path") {
        return target.path;
      }
      if (prop === "stepId") {
        return target.stepId;
      }
      if (typeof prop === "symbol") {
        return Reflect.get(target, prop, receiver);
      }
      const nextPath = target.path ? `${target.path}.${String(prop)}` : String(prop);
      return createRefProxy(nextPath, target.kind, target.stepId);
    },
  }) as unknown as StepRefProxy;
}
export function lowerWorkflowValueNode(
  node: Record<string, unknown>,
): WorkflowValue {
  const kind = node.kind;
  if (typeof kind !== "string") {
    throw new Error("workflow value node requires kind");
  }
  switch (kind) {
    case "input":
      return workflowValue({ input: String(node.path ?? "") });
    case "signal":
      return workflowValue({ signal: String(node.path ?? "") });
    case "stepOutput":
      return workflowValue({
        stepOutput: {
          stepId: String(node.stepId ?? ""),
          path: String(node.path ?? ""),
        },
      });
    case "stepInput":
      return workflowValue({
        stepInput: {
          stepId: String(node.stepId ?? ""),
          path: String(node.path ?? ""),
        },
      });
    case "literal":
      return workflowValue({ literal: node.value as JsonInput });
    case "template":
      return workflowValue({ template: workflowText(String(node.template ?? "")) });
    case "object": {
      const fields = node.fields;
      if (fields === null || typeof fields !== "object" || Array.isArray(fields)) {
        throw new Error("workflow object node requires fields");
      }
      const out: Record<string, WorkflowValue> = {};
      for (const [key, nested] of Object.entries(fields)) {
        out[key] = lowerWorkflowValueNode(nested as Record<string, unknown>);
      }
      return workflowValue({ object: out });
    }
    case "array": {
      const values = node.values;
      if (!Array.isArray(values)) {
        throw new Error("workflow array node requires values");
      }
      return workflowValue({
        array: values.map((item) =>
          workflowValue(lowerWorkflowValueNode(item as Record<string, unknown>)),
        ),
      });
    }
    default:
      throw new Error(`unsupported workflow value kind: ${kind}`);
  }
}

/** Builds a workflow definition from the shared lowering-contract case shape. */
export function buildWorkflowFromLoweringCase(caseData: {
  init: { id: string; runAs: string; paused?: boolean };
  activations?: Array<Record<string, unknown>>;
  steps?: Array<Record<string, unknown>>;
}): WorkflowBuilder {
  const builder = defineWorkflow({
    id: caseData.init.id,
    runAs: caseData.init.runAs,
    paused: caseData.init.paused,
  });

  for (const activation of caseData.activations ?? []) {
    if (activation.event !== undefined) {
      const eventConfig = activation.event as Record<string, unknown>;
      const input = activation.input as Record<string, unknown> | undefined;
      builder.on(
        event(
          String(eventConfig.type ?? ""),
          input === undefined
            ? undefined
            : () => lowerContractInputToMappedObject(input),
          {
            id: String(activation.id ?? ""),
            source: String(eventConfig.source ?? ""),
            subject: String(eventConfig.subject ?? ""),
            paused: Boolean(activation.paused ?? false),
          },
        ),
      );
      continue;
    }
    const scheduleConfig = activation.schedule as Record<string, unknown>;
    const input = activation.input as Record<string, unknown> | undefined;
    builder.on(
      schedule(
        String(scheduleConfig.cron ?? ""),
        input === undefined
          ? undefined
          : () => lowerContractInputToMappedObject(input),
        {
          id: String(activation.id ?? ""),
          timezone: String(scheduleConfig.timezone ?? ""),
          paused: Boolean(activation.paused ?? false),
        },
      ),
    );
  }

  for (const step of caseData.steps ?? []) {
    const config: StepConfig = {};
    if (step.inputs !== undefined) {
      config.inputs = () =>
        lowerContractInputToMappedObject(step.inputs as Record<string, unknown>);
    }
    if (step.app !== undefined) {
      const app = step.app as Record<string, unknown>;
      config.app = {
        name: String(app.name ?? ""),
        operation: String(app.operation ?? ""),
        input:
          app.input === undefined
            ? undefined
            : () => lowerContractInputToMappedObject(app.input as Record<string, unknown>),
        connection: app.connection === undefined ? undefined : String(app.connection),
        instance: app.instance === undefined ? undefined : String(app.instance),
        credentialMode:
          app.credentialMode === undefined ? undefined : String(app.credentialMode),
      };
    }
    if (step.agent !== undefined) {
      const agent = step.agent as Record<string, unknown>;
      config.agent = {
        provider: String(agent.provider ?? ""),
        model: agent.model === undefined ? undefined : String(agent.model),
        sessionKey: agent.sessionKey === undefined ? undefined : String(agent.sessionKey),
        prompt:
          agent.prompt === undefined
            ? undefined
            : () => {
                const promptNode = agent.prompt as Record<string, unknown>;
                if (promptNode.kind === "template") {
                  return String(promptNode.template ?? "");
                }
                throw new Error("agent prompt must be a template node in lowering contract");
              },
        messages: Array.isArray(agent.messages)
          ? agent.messages.map((message) => {
              const typed = message as Record<string, unknown>;
              const textNode = typed.text as Record<string, unknown>;
              return {
                role: String(typed.role ?? ""),
                text:
                  textNode.kind === "literal"
                    ? String(textNode.value ?? "")
                    : String(textNode.template ?? ""),
              };
            })
          : undefined,
        tools: Array.isArray(agent.tools) ? (agent.tools as AgentToolRef[]) : undefined,
        output: agent.output as AgentOutput | undefined,
        modelOptions: agent.modelOptions as JsonObjectInput | undefined,
      };
    }
    if (step.when !== undefined) {
      const when = step.when as Record<string, unknown>;
      config.when = {
        value: lowerWorkflowValueNode(when.value as Record<string, unknown>),
        equals: when.equals as JsonInput | undefined,
      };
    }
    if (step.timeoutSeconds !== undefined) {
      config.timeoutSeconds = Number(step.timeoutSeconds);
    }
    if (step.metadata !== undefined) {
      config.metadata = step.metadata as JsonObjectInput;
    }
    builder.step(String(step.id ?? ""), config);
  }

  return builder;
}

function lowerContractInputToMappedObject(
  node: Record<string, unknown>,
): Record<string, unknown> {
  if (node.kind !== "object") {
    throw new Error("lowering contract input must be an object node");
  }
  const fields = node.fields;
  if (fields === null || typeof fields !== "object" || Array.isArray(fields)) {
    throw new Error("lowering contract object node requires fields");
  }
  const out: Record<string, unknown> = {};
  for (const [key, nested] of Object.entries(fields)) {
    out[key] = lowerContractValueToRuntime(nested as Record<string, unknown>);
  }
  return out;
}

function lowerContractValueToRuntime(node: Record<string, unknown>): unknown {
  const kind = String(node.kind ?? "");
  switch (kind) {
    case "input":
      return createRefProxy(String(node.path ?? ""), "input");
    case "signal":
      return createRefProxy(String(node.path ?? ""), "signal");
    case "stepOutput":
      return {
        [WORKFLOW_REF]: true,
        kind: "stepOutput",
        stepId: String(node.stepId ?? ""),
        path: String(node.path ?? ""),
      } satisfies WorkflowRef;
    case "stepInput":
      return {
        [WORKFLOW_REF]: true,
        kind: "stepInput",
        stepId: String(node.stepId ?? ""),
        path: String(node.path ?? ""),
      } satisfies WorkflowRef;
    case "literal":
      return node.value;
    case "template":
      return {
        [WORKFLOW_REF]: true,
        kind: "template",
        template: String(node.template ?? ""),
      } satisfies WorkflowTemplateRef;
    case "object": {
      const fields = node.fields;
      if (fields === null || typeof fields !== "object" || Array.isArray(fields)) {
        throw new Error("workflow object node requires fields");
      }
      const out: Record<string, unknown> = {};
      for (const [key, nested] of Object.entries(fields)) {
        out[key] = lowerContractValueToRuntime(nested as Record<string, unknown>);
      }
      return out;
    }
    case "array": {
      const values = node.values;
      if (!Array.isArray(values)) {
        throw new Error("workflow array node requires values");
      }
      return values.map((item) => lowerContractValueToRuntime(item as Record<string, unknown>));
    }
    default:
      throw new Error(`unsupported workflow value kind: ${kind}`);
  }
}

/** Canonical JSON representation used by golden fixture tests. */
export function canonicalWorkflowDefinitionSpec(
  spec: WorkflowDefinitionSpec,
): Record<string, unknown> {
  const normalized = workflowDefinitionSpec(spec);
  return {
    id: normalized.id ?? "",
    runAs: normalized.runAs ?? "",
    paused: normalized.paused ?? false,
    activations: (normalized.activations ?? []).map(canonicalWorkflowActivation),
    target:
      normalized.target === undefined
        ? undefined
        : {
            steps: (normalized.target.steps ?? []).map(canonicalWorkflowStep),
          },
  };
}

function canonicalWorkflowActivation(activation: WorkflowActivation): Record<string, unknown> {
  const out: Record<string, unknown> = {
    id: activation.id ?? "",
    paused: activation.paused ?? false,
  };
  if (activation.schedule !== undefined) {
    out.schedule = {
      cron: activation.schedule.cron ?? "",
      timezone: activation.schedule.timezone ?? "",
    };
  }
  if (activation.event !== undefined) {
    out.event = {
      match: {
        type: activation.event.match?.type ?? "",
        source: activation.event.match?.source ?? "",
        subject: activation.event.match?.subject ?? "",
      },
    };
  }
  if (activation.input !== undefined) {
    out.input = canonicalWorkflowValue(activation.input);
  }
  return out;
}

function canonicalWorkflowStep(step: WorkflowStep): Record<string, unknown> {
  const out: Record<string, unknown> = { id: step.id ?? "" };
  if (step.inputs !== undefined && Object.keys(step.inputs).length > 0) {
    out.inputs = Object.fromEntries(
      Object.entries(step.inputs).map(([key, value]) => [key, canonicalWorkflowValue(value)]),
    );
  }
  if (step.app !== undefined) {
    out.app = {
      name: step.app.name ?? "",
      operation: step.app.operation ?? "",
      input: step.app.input === undefined ? undefined : canonicalWorkflowValue(step.app.input),
    };
  }
  if (step.agent !== undefined) {
    const agent: Record<string, unknown> = {
      provider: step.agent.provider ?? "",
      model: step.agent.model ?? "",
    };
    if (step.agent.sessionKey) {
      agent.sessionKey = step.agent.sessionKey;
    }
    if (step.agent.prompt !== undefined) {
      agent.prompt = { template: workflowTextTemplate(step.agent.prompt) };
    }
    if ((step.agent.messages ?? []).length > 0) {
      agent.messages = (step.agent.messages ?? []).map((message) => ({
        role: message.role ?? "",
        text:
          message.text === undefined
            ? undefined
            : { template: workflowTextTemplate(message.text) },
      }));
    }
    if ((step.agent.tools ?? []).length > 0) {
      agent.tools = step.agent.tools ?? [];
    }
    if (step.agent.output !== undefined) {
      agent.output = step.agent.output;
    }
    if (step.agent.modelOptions !== undefined) {
      agent.modelOptions = step.agent.modelOptions;
    }
    out.agent = agent;
  }
  if (step.when !== undefined) {
    out.when = {
      value: step.when.value === undefined ? undefined : canonicalWorkflowValue(step.when.value),
      equals: step.when.equals,
    };
  }
  if (step.timeoutSeconds !== undefined && step.timeoutSeconds !== 0) {
    out.timeoutSeconds = step.timeoutSeconds;
  }
  if (step.metadata !== undefined) {
    out.metadata = step.metadata;
  }
  return out;
}

function workflowTextTemplate(text: unknown): string {
  if (text === undefined || text === null) {
    return "";
  }
  if (typeof text === "string") {
    return text;
  }
  if (typeof text === "object" && "template" in text) {
    return String((text as { template?: string }).template ?? "");
  }
  return "";
}

function canonicalWorkflowValue(value: WorkflowValue): Record<string, unknown> {
  const normalized = workflowValue(value);
  const kind = normalized.kind;
  switch (kind?.case) {
    case "literal":
      return { literal: kind.value };
    case "object": {
      const fields: Record<string, unknown> = {};
      for (const [key, nested] of Object.entries(kind.value)) {
        fields[key] = canonicalWorkflowValue(nested);
      }
      return { object: fields };
    }
    case "array":
      return { array: kind.value.map((item) => canonicalWorkflowValue(item)) };
    case "template":
      return { template: workflowTextTemplate(kind.value) };
    case "input":
      return { input: kind.value };
    case "signal":
      return { signal: kind.value };
    case "stepOutput":
      return {
        stepOutput: {
          stepId: kind.value.stepId ?? "",
          path: kind.value.path ?? "",
        },
      };
    case "stepInput":
      return {
        stepInput: {
          stepId: kind.value.stepId ?? "",
          path: kind.value.path ?? "",
        },
      };
    default:
      return {};
  }
}
