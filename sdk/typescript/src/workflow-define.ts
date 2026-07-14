/**
 * Fluent workflow definition builder with proxy-based reference capture.
 *
 * This is a structural fluent builder: references are captured into workflow
 * value nodes, but step IDs and paths are not accumulated as compile-time types.
 *
 * @module
 */

import type { AgentOutput, AgentToolRef } from "./providers/agent.ts";
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

interface WorkflowTextExpression {
  readonly [WORKFLOW_REF]: true;
  readonly kind: "composedText";
  readonly parts: ReadonlyArray<string | WorkflowRef>;
}

type CapturedRef = WorkflowRef | WorkflowLiteralRef | WorkflowTemplateRef | WorkflowTextExpression;

export type WorkflowDefinitionInput =
  | WorkflowDefinitionSpec
  | WorkflowBuilder
  | { toSpec(): WorkflowDefinitionSpec };

export type WorkflowPrompt =
  | string
  | WorkflowTextExpression
  | ((scope: StepScope) => string | WorkflowTextExpression);

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
  text: WorkflowPrompt;
}

export interface StepAgentConfig {
  provider: string;
  model?: string | undefined;
  sessionKey?: string | undefined;
  prompt?: WorkflowPrompt | undefined;
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

export interface StepRefProxy {
  readonly [key: string]: unknown;
}

export interface StepScopeRefs {
  readonly output: StepRefProxy;
  readonly input: StepRefProxy;
}

export interface StepScope {
  readonly input: StepRefProxy;
  readonly signal: StepRefProxy;
  stepOutput(stepId: string, path: string): WorkflowRef;
  stepInput(stepId: string, path: string): WorkflowRef;
  readonly steps: Record<string, StepScopeRefs>;
}

export type EventScope = StepRefProxy;
export type ActivationScope = StepRefProxy;

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

/** Starts a fluent workflow definition builder. `runAs` is required. */
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

/** Composes workflow prompt/message text from literals and captured references. */
export function text(...parts: Array<string | StepRefProxy | WorkflowRef>): WorkflowTextExpression {
  const normalized: Array<string | WorkflowRef> = [];
  for (const part of parts) {
    if (typeof part === "string") {
      normalized.push(part);
      continue;
    }
    if (isWorkflowRef(part)) {
      normalized.push(part);
      continue;
    }
    throw new Error("text() parts must be strings or workflow references");
  }
  return {
    [WORKFLOW_REF]: true,
    kind: "composedText",
    parts: normalized,
  };
}

/** Returns a workflow definition spec when a builder is passed where a raw spec is accepted. */
export function resolveWorkflowDefinitionSpec(input: WorkflowDefinitionInput): WorkflowDefinitionSpec {
  if (input instanceof WorkflowBuilder) {
    return input.toSpec();
  }
  if (
    typeof input === "object" &&
    input !== null &&
    "toSpec" in input &&
    typeof input.toSpec === "function"
  ) {
    return input.toSpec();
  }
  return workflowDefinitionSpec(input as WorkflowDefinitionSpec);
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
        ? activation.mapInput(createRefProxy("", "signal") as EventScope)
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
      ? activation.mapInput(createRefProxy("", "input") as ActivationScope)
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
        prompt:
          prompt === undefined ? undefined : workflowText(resolveWorkflowPrompt(prompt)),
        messages: config.agent.messages?.map((message) => {
          const textValue =
            typeof message.text === "function" ? message.text(scope) : message.text;
          return workflowAgentMessage({
            role: message.role,
            text: workflowText(resolveWorkflowPrompt(textValue)),
          });
        }),
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
    input: createRefProxy("", "input") as StepRefProxy,
    signal: createRefProxy("", "signal") as StepRefProxy,
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
            output: createRefProxy("", "stepOutput", stepId) as StepRefProxy,
            input: createRefProxy("", "stepInput", stepId) as StepRefProxy,
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

function isCapturedRef(value: unknown): value is CapturedRef {
  return (
    typeof value === "object" &&
    value !== null &&
    (value as CapturedRef)[WORKFLOW_REF] === true
  );
}

function isWorkflowRef(value: unknown): value is WorkflowRef {
  return (
    isCapturedRef(value) &&
    (value.kind === "input" ||
      value.kind === "signal" ||
      value.kind === "stepOutput" ||
      value.kind === "stepInput")
  );
}

function isTextExpression(value: unknown): value is WorkflowTextExpression {
  return isCapturedRef(value) && value.kind === "composedText";
}

function resolveWorkflowPrompt(value: string | WorkflowTextExpression): string {
  if (typeof value === "string") {
    return value;
  }
  if (isTextExpression(value)) {
    return renderTextExpression(value);
  }
  throw new Error("workflow prompt must be a string or text() expression");
}

function renderTextExpression(expression: WorkflowTextExpression): string {
  let rendered = "";
  for (const part of expression.parts) {
    if (typeof part === "string") {
      rendered += part;
      continue;
    }
    rendered += refToTemplatePlaceholder(part);
  }
  return rendered;
}

function refToTemplatePlaceholder(ref: WorkflowRef): string {
  switch (ref.kind) {
    case "input":
      return `\${{ input.${ref.path} }}`;
    case "signal":
      return `\${{ signal.${ref.path} }}`;
    case "stepOutput":
      return `\${{ steps.${ref.stepId ?? ""}.outputs.${ref.path} }}`;
    case "stepInput":
      return `\${{ steps.${ref.stepId ?? ""}.inputs.${ref.path} }}`;
  }
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
    if (value.kind === "composedText") {
      throw new Error("workflow values cannot use text(); use text() only for prompts and messages");
    }
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
