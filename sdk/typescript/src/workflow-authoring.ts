/**
 * Typed workflow authoring builder with proxy-based reference capture.
 *
 * @module
 */

import type { AgentOutput, AgentToolRef } from "./agent.ts";
import type { JsonInput, JsonObjectInput } from "./protocol.ts";
import {
  boundWorkflowTarget,
  workflowActivation,
  workflowAgentMessage,
  workflowDefinitionSpec,
  workflowEventActivation,
  workflowEventMatch,
  workflowStep,
  workflowStepAgentTurn,
  workflowStepAppCall,
  workflowStepWhen,
  workflowText,
  workflowValue,
  type BoundWorkflowTarget,
  type WorkflowActivation,
  type WorkflowDefinitionSpec,
  type WorkflowStep,
  type WorkflowValue,
} from "./providers/workflow.ts";

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

export interface DefineWorkflowOptions {
  id: string;
  runAs: string;
  paused?: boolean | undefined;
}

export interface EventActivationOptions {
  id?: string | undefined;
  source?: string | undefined;
  subject?: string | undefined;
  paused?: boolean | undefined;
}

export interface ScheduleActivationOptions {
  id?: string | undefined;
  timezone?: string | undefined;
  paused?: boolean | undefined;
}

export interface EventActivationConfig {
  readonly __workflowActivation: "event";
  readonly type: string;
  readonly mapInput?: ((event: EventScope) => Record<string, unknown>) | undefined;
  readonly options?: EventActivationOptions | undefined;
}

export interface ScheduleActivationConfig {
  readonly __workflowActivation: "schedule";
  readonly cron: string;
  readonly mapInput?: ((scope: ActivationScope) => Record<string, unknown>) | undefined;
  readonly options?: ScheduleActivationOptions | undefined;
}

export type ActivationConfig = EventActivationConfig | ScheduleActivationConfig;

export interface StepAppConfig {
  name: string;
  operation: string;
  input?: ((scope: StepScope) => Record<string, unknown>) | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  credentialMode?: string | undefined;
}

export interface StepAgentMessageConfig {
  role: string;
  text: string | ((scope: StepScope) => string);
}

export interface StepAgentConfig {
  provider: string;
  model?: string | undefined;
  sessionKey?: string | undefined;
  prompt?: string | ((scope: StepScope) => string) | undefined;
  messages?: readonly StepAgentMessageConfig[] | undefined;
  tools?: readonly AgentToolRef[] | undefined;
  output?: AgentOutput | undefined;
  modelOptions?: JsonObjectInput | undefined;
}

export interface StepWhenConfig {
  value: unknown;
  equals?: JsonInput | undefined;
}

export interface StepConfig {
  app?: StepAppConfig | undefined;
  agent?: StepAgentConfig | undefined;
  inputs?: Record<string, unknown> | ((scope: StepScope) => Record<string, unknown>) | undefined;
  when?: StepWhenConfig | undefined;
  timeoutSeconds?: number | undefined;
  metadata?: JsonObjectInput | undefined;
}

export type EventScope = ReturnType<typeof createRefProxy>;
export type ActivationScope = ReturnType<typeof createRefProxy>;
export type StepScope = ReturnType<typeof createStepScope>;

/** Creates an event activation configuration for `.on()`. */
export function event(
  type: string,
  mapInput?: (event: EventScope) => Record<string, unknown>,
  options?: EventActivationOptions,
): EventActivationConfig {
  return {
    __workflowActivation: "event",
    type,
    mapInput,
    options,
  };
}

/** Creates a schedule activation configuration for `.on()`. */
export function schedule(
  cron: string,
  mapInput?: (scope: ActivationScope) => Record<string, unknown>,
  options?: ScheduleActivationOptions,
): ScheduleActivationConfig {
  return {
    __workflowActivation: "schedule",
    cron,
    mapInput,
    options,
  };
}

/** Starts a typed workflow definition builder. `runAs` is required. */
export function defineWorkflow(options: DefineWorkflowOptions): WorkflowBuilder {
  const runAs = options.runAs?.trim() ?? "";
  if (!runAs) {
    throw new Error("defineWorkflow requires runAs");
  }
  const id = options.id?.trim() ?? "";
  if (!id) {
    throw new Error("defineWorkflow requires id");
  }
  return new WorkflowBuilder({
    id,
    runAs,
    paused: options.paused ?? false,
    activations: [],
    steps: [],
  });
}

/** Returns a workflow definition spec when a builder is passed where a raw spec is accepted. */
export function resolveWorkflowDefinitionSpec(
  input: WorkflowDefinitionSpec | WorkflowBuilder,
): WorkflowDefinitionSpec {
  if (input instanceof WorkflowBuilder) {
    return input.toSpec();
  }
  return workflowDefinitionSpec(input);
}

/** Applies a workflow definition from either a builder or a raw spec. */
export async function applyWorkflowDefinition(
  workflow: import("./workflow.ts").Workflow,
  provider: string,
  idempotencyKey: string,
  spec?: WorkflowDefinitionSpec | WorkflowBuilder,
): Promise<import("./workflow.ts").WorkflowDefinition> {
  return workflow.applyDefinition(
    provider,
    idempotencyKey,
    spec === undefined ? undefined : resolveWorkflowDefinitionSpec(spec),
  );
}

export class WorkflowBuilder {
  private readonly state: {
    id: string;
    runAs: string;
    paused: boolean;
    activations: WorkflowActivation[];
    steps: WorkflowStep[];
  };

  constructor(state: WorkflowBuilder["state"]) {
    this.state = state;
  }

  on(activation: ActivationConfig): this {
    if (activation.__workflowActivation === "event") {
      const activationId = activation.options?.id?.trim() || activation.type;
      const mapped = activation.mapInput
        ? activation.mapInput(createRefProxy("", "signal"))
        : undefined;
      this.state.activations.push(
        workflowActivation({
          id: activationId,
          paused: activation.options?.paused ?? false,
          event: workflowEventActivation({
            match: workflowEventMatch({
              type: activation.type,
              source: activation.options?.source ?? "",
              subject: activation.options?.subject ?? "",
            }),
          }),
          input:
            mapped === undefined
              ? undefined
              : workflowValue({ object: captureObject(mapped, "signal") }),
        }),
      );
      return this;
    }

    const activationId = activation.options?.id?.trim() || activation.cron;
    const mapped = activation.mapInput
      ? activation.mapInput(createRefProxy("", "input"))
      : undefined;
    this.state.activations.push(
      workflowActivation({
        id: activationId,
        paused: activation.options?.paused ?? false,
        schedule: {
          cron: activation.cron,
          timezone: activation.options?.timezone ?? "",
        },
        input:
          mapped === undefined
            ? undefined
            : workflowValue({ object: captureObject(mapped, "input") }),
      }),
    );
    return this;
  }

  step(stepId: string, config: StepConfig): this {
    const scope = createStepScope();
    const step: WorkflowStep = { id: stepId };

    if (config.inputs !== undefined) {
      const mapped =
        typeof config.inputs === "function" ? config.inputs(scope) : config.inputs;
      step.inputs = captureObject(mapped, "input");
    }

    if (config.app !== undefined) {
      const input = config.app.input?.(scope);
      step.app = workflowStepAppCall({
        name: config.app.name,
        operation: config.app.operation,
        input:
          input === undefined ? undefined : workflowValue({ object: captureObject(input, "input") }),
        connection: config.app.connection,
        instance: config.app.instance,
        credentialMode: config.app.credentialMode,
      });
    }

    if (config.agent !== undefined) {
      const prompt =
        typeof config.agent.prompt === "function"
          ? config.agent.prompt(scope)
          : config.agent.prompt;
      step.agent = workflowStepAgentTurn({
        provider: config.agent.provider,
        model: config.agent.model,
        sessionKey: config.agent.sessionKey,
        prompt: prompt === undefined ? undefined : workflowText(prompt),
        messages: config.agent.messages?.map((message) =>
          workflowAgentMessage({
            role: message.role,
            text: workflowText(
              typeof message.text === "function" ? message.text(scope) : message.text,
            ),
          }),
        ),
        tools: config.agent.tools ? [...config.agent.tools] : undefined,
        output: config.agent.output,
        modelOptions: config.agent.modelOptions,
      });
    }

    if (config.when !== undefined) {
      const whenValue =
        typeof config.when.value === "object" &&
        config.when.value !== null &&
        "kind" in (config.when.value as WorkflowValue)
          ? workflowValue(config.when.value as WorkflowValue)
          : workflowValue(captureValue(config.when.value, "input"));
      step.when = workflowStepWhen({
        value: whenValue,
        equals: config.when.equals,
      });
    }

    if (config.timeoutSeconds !== undefined) {
      step.timeoutSeconds = config.timeoutSeconds;
    }
    if (config.metadata !== undefined) {
      step.metadata = config.metadata;
    }

    this.state.steps.push(workflowStep(step));
    return this;
  }

  toSpec(): WorkflowDefinitionSpec {
    const target: BoundWorkflowTarget | undefined =
      this.state.steps.length === 0
        ? undefined
        : boundWorkflowTarget({ steps: this.state.steps });
    return workflowDefinitionSpec({
      id: this.state.id,
      runAs: this.state.runAs,
      paused: this.state.paused,
      activations: this.state.activations,
      target,
    });
  }
}

function createStepScope(): StepScope {
  return {
    input: createRefProxy("", "input"),
    signal: createRefProxy("", "signal"),
    stepOutput(stepId: string, path: string): WorkflowRef {
      return {
        [WORKFLOW_REF]: true,
        kind: "stepOutput",
        stepId,
        path,
      };
    },
    stepInput(stepId: string, path: string): WorkflowRef {
      return {
        [WORKFLOW_REF]: true,
        kind: "stepInput",
        stepId,
        path,
      };
    },
    steps: new Proxy(
      {},
      {
        get(_target, stepId) {
          if (typeof stepId !== "string") {
            return undefined;
          }
          return {
            output: createRefProxy("", "stepOutput", stepId),
            input: createRefProxy("", "stepInput", stepId),
          };
        },
      },
    ),
  };
}

function createRefProxy(
  path: string,
  kind: WorkflowRefKind,
  stepId?: string,
): unknown {
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
  });
}

function isCapturedRef(value: unknown): value is CapturedRef {
  return (
    typeof value === "object" &&
    value !== null &&
    (value as CapturedRef)[WORKFLOW_REF] === true
  );
}

function captureObject(
  value: Record<string, unknown>,
  defaultKind: WorkflowRefKind,
): Record<string, WorkflowValue> {
  const out: Record<string, WorkflowValue> = {};
  for (const [key, nested] of Object.entries(value)) {
    out[key] = workflowValue(captureValue(nested, defaultKind));
  }
  return out;
}

function captureValue(value: unknown, defaultKind: WorkflowRefKind): WorkflowValue {
  if (isCapturedRef(value)) {
    switch (value.kind) {
      case "input":
        return { input: value.path };
      case "signal":
        return { signal: value.path };
      case "stepOutput":
        return { stepOutput: { stepId: value.stepId ?? "", path: value.path } };
      case "stepInput":
        return { stepInput: { stepId: value.stepId ?? "", path: value.path } };
      case "literal":
        return { literal: value.value };
      case "template":
        return { template: workflowText(value.template) };
      default: {
        const exhaustive: never = value;
        throw new Error(`unsupported workflow ref kind: ${String(exhaustive)}`);
      }
    }
  }

  if (Array.isArray(value)) {
    return {
      array: value.map((item) => workflowValue(captureValue(item, defaultKind))),
    };
  }

  if (value !== null && typeof value === "object") {
    return {
      object: captureObject(value as Record<string, unknown>, defaultKind),
    };
  }

  return { literal: value as JsonInput };
}

/** Lowers a language-neutral value node from the shared fixture contract. */
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
      agent.prompt = { template: step.agent.prompt.template ?? "" };
    }
    if ((step.agent.messages ?? []).length > 0) {
      agent.messages = (step.agent.messages ?? []).map((message) => ({
        role: message.role ?? "",
        text:
          message.text === undefined
            ? undefined
            : { template: message.text.template ?? "" },
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
      return { template: kind.value.template ?? "" };
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
