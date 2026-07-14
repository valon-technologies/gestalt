/**
 * Fluent workflow definition builder with proxy-based reference capture.
 *
 * References are captured into workflow value nodes while step IDs and mapped
 * run-input keys accumulate in the fluent builder's type parameters.
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

type EmptyInput = Record<never, never>;
type EmptySteps = Record<never, never>;

export type WorkflowDefinitionInput =
  | WorkflowDefinitionSpec
  | WorkflowBuilder
  | { toSpec(): WorkflowDefinitionSpec };

export type WorkflowPrompt<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> =
  | string
  | WorkflowTextExpression
  | ((scope: StepScope<RunInput, Steps>) => string | WorkflowTextExpression);

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

export interface EventActivationConfig<ActivationInput extends object = EmptyInput> {
  readonly __workflowActivation: "event";
  readonly type: string;
  readonly mapInput?: ((event: EventScope) => ActivationInput) | undefined;
  readonly options?: EventActivationOptions | undefined;
}

export interface ScheduleActivationConfig<ActivationInput extends object = EmptyInput> {
  readonly __workflowActivation: "schedule";
  readonly cron: string;
  readonly mapInput?: ((scope: ActivationScope) => ActivationInput) | undefined;
  readonly options?: ScheduleActivationOptions | undefined;
}

export type ActivationConfig<ActivationInput extends object = EmptyInput> =
  | EventActivationConfig<ActivationInput>
  | ScheduleActivationConfig<ActivationInput>;

export interface StepAppConfig<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> {
  name: string;
  operation: string;
  input?: (
    (scope: StepScope<RunInput, Steps>) => Record<string, unknown> | StepRefProxy
  ) | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  credentialMode?: string | undefined;
}

export interface StepAgentMessageConfig<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> {
  role: string;
  text: WorkflowPrompt<RunInput, Steps>;
}

export interface StepAgentConfig<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> {
  provider: string;
  model?: string | undefined;
  sessionKey?: string | undefined;
  prompt?: WorkflowPrompt<RunInput, Steps> | undefined;
  messages?: readonly StepAgentMessageConfig<RunInput, Steps>[] | undefined;
  tools?: readonly AgentToolRef[] | undefined;
  output?: AgentOutput | undefined;
  modelOptions?: JsonObjectInput | undefined;
}

type RuntimeWorkflowPrompt =
  | string
  | WorkflowTextExpression
  | ((scope: unknown) => string | WorkflowTextExpression);

interface RuntimeStepAgentMessageConfig {
  role: string;
  text: RuntimeWorkflowPrompt;
}

export interface StepWhenConfig {
  value: unknown;
  equals?: JsonInput | undefined;
}

export interface StepConfig<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> {
  app?: StepAppConfig<RunInput, Steps> | undefined;
  agent?: StepAgentConfig<RunInput, Steps> | undefined;
  inputs?:
    | Record<string, unknown>
    | ((scope: StepScope<RunInput, Steps>) => Record<string, unknown>)
    | undefined;
  when?: StepWhenConfig | undefined;
  timeoutSeconds?: number | undefined;
  metadata?: JsonObjectInput | undefined;
}

interface StepRefProxy {
  readonly [key: string]: StepRefProxy;
}

interface StepScopeRefs<Output = unknown> {
  readonly outputs: RefView<Output>;
  readonly inputs: StepRefProxy;
}

type WorkflowSteps = Record<string, unknown>;

type RefView<T> = T extends readonly unknown[]
  ? StepRefProxy
  : T extends object
  ? StepRefProxy & { readonly [K in keyof T]: RefView<T[K]> }
  : StepRefProxy;

type ScopeRefObject<T extends object> = StepRefProxy & {
  readonly [K in keyof T]: RefView<T[K]>;
};

interface StepScope<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> {
  readonly input: ScopeRefObject<RunInput>;
  readonly signal: StepRefProxy;
  stepOutput(stepId: string, path: string): WorkflowRef;
  stepInput(stepId: string, path: string): WorkflowRef;
  readonly steps: {
    readonly [K in keyof Steps]: StepScopeRefs<Steps[K]>;
  } & { readonly [key: string]: StepScopeRefs };
}

interface EventScope {
  readonly data: StepRefProxy;
}

interface ActivationScope {
  readonly input: StepRefProxy;
}

/** Creates an event activation configuration for `.on()`. */
export function event<ActivationInput extends object = EmptyInput>(
  type: string,
  mapInput?: (event: EventScope) => ActivationInput,
  options?: EventActivationOptions,
): EventActivationConfig<ActivationInput> {
  return {
    __workflowActivation: "event",
    type,
    mapInput,
    options,
  };
}

/** Creates a schedule activation configuration for `.on()`. */
export function schedule<ActivationInput extends object = EmptyInput>(
  cron: string,
  mapInput?: (scope: ActivationScope) => ActivationInput,
  options?: ScheduleActivationOptions,
): ScheduleActivationConfig<ActivationInput> {
  return {
    __workflowActivation: "schedule",
    cron,
    mapInput,
    options,
  };
}

/** Starts a fluent workflow definition builder. `runAs` is required. */
export function defineWorkflow<RunInput extends object = EmptyInput>(
  options: DefineWorkflowOptions,
): WorkflowBuilder<RunInput, EmptySteps> {
  const runAs = options.runAs?.trim() ?? "";
  if (!runAs) {
    throw new Error("defineWorkflow requires runAs");
  }
  const id = options.id?.trim() ?? "";
  if (!id) {
    throw new Error("defineWorkflow requires id");
  }
  return new WorkflowBuilder<RunInput, EmptySteps>({
    id,
    runAs,
    paused: options.paused ?? false,
    activations: [],
    steps: [],
  });
}

/** Composes workflow prompt/message text from literals and captured references. */
export function text(
  // Indexed proxy properties are possibly undefined under
  // `noUncheckedIndexedAccess`; the runtime proxy returns a reference for
  // every string property.
  ...parts: Array<string | StepRefProxy | WorkflowRef | undefined>
): WorkflowTextExpression {
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

type MergeRunInput<Current extends object, Next extends object> =
  keyof Next extends never ? Current : Current & Next;

export class WorkflowBuilder<
  RunInput extends object = EmptyInput,
  Steps extends WorkflowSteps = EmptySteps,
> {
  private readonly state: {
    id: string;
    runAs: string;
    paused: boolean;
    activations: WorkflowActivation[];
    steps: WorkflowStep[];
  };

  constructor(state: WorkflowBuilder<RunInput, Steps>["state"]) {
    this.state = state;
  }

  on<ActivationInput extends object>(
    activation: ActivationConfig<ActivationInput>,
  ): WorkflowBuilder<MergeRunInput<RunInput, ActivationInput>, Steps> {
    if (activation.__workflowActivation === "event") {
      const activationId = activation.options?.id?.trim() || activation.type;
      const mapped = activation.mapInput
        ? activation.mapInput({ data: createRefProxy("data", "signal") })
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
      return this as unknown as WorkflowBuilder<
        MergeRunInput<RunInput, ActivationInput>,
        Steps
      >;
    }

    const activationId = activation.options?.id?.trim() || activation.cron;
    const mapped = activation.mapInput
      ? activation.mapInput({ input: createRefProxy("", "input") })
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
    return this as unknown as WorkflowBuilder<
      MergeRunInput<RunInput, ActivationInput>,
      Steps
    >;
  }

  step<const StepId extends string, Output = unknown>(
    stepId: StepId,
    config: StepConfig<RunInput, Steps>,
  ): WorkflowBuilder<RunInput, Steps & Record<StepId, Output>> {
    const scope = createStepScope<RunInput, Steps>();
    const step: WorkflowStep = { id: stepId };

    if (config.inputs !== undefined) {
      const mapped =
        typeof config.inputs === "function" ? config.inputs(scope) : config.inputs;
      step.inputs = captureObject(mapped, "input");
    }

    if (config.app !== undefined && config.agent !== undefined) {
      throw new Error("workflow step cannot configure both app and agent actions");
    }

    if (config.app !== undefined) {
      const input = config.app.input?.(scope);
      step.app = workflowStepAppCall({
        name: config.app.name,
        operation: config.app.operation,
        input:
          input === undefined ? undefined : workflowValue(captureValue(input, "input")),
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
      const messages = config.agent.messages as unknown as
        | readonly RuntimeStepAgentMessageConfig[]
        | undefined;
      step.agent = workflowStepAgentTurn({
        provider: config.agent.provider,
        model: config.agent.model,
        sessionKey: config.agent.sessionKey,
        prompt:
          prompt === undefined ? undefined : workflowText(resolveWorkflowPrompt(prompt)),
        messages: messages?.map((message) => {
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
        isWorkflowRef(config.when.value)
          ? workflowValue(captureValue(config.when.value, "input"))
          : typeof config.when.value === "object" &&
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
    return this as unknown as WorkflowBuilder<
      RunInput,
      Steps & Record<StepId, Output>
    >;
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

function createStepScope<
  RunInput extends object,
  Steps extends WorkflowSteps,
>(): StepScope<RunInput, Steps> {
  const steps = new Proxy(
    {},
    {
      get(_target, stepId) {
        if (typeof stepId !== "string") {
          return undefined;
        }
        return {
          outputs: createRefProxy("", "stepOutput", stepId) as StepRefProxy,
          inputs: createRefProxy("", "stepInput", stepId) as StepRefProxy,
        };
      },
    },
  ) as Steps & WorkflowSteps;
  return {
    input: createRefProxy("", "input") as StepScope<RunInput, Steps>["input"],
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
    steps,
  } as unknown as StepScope<RunInput, Steps>;
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
  value: object,
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
