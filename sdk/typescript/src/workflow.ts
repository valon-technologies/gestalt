import { create, type JsonObject } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import {
  BoundWorkflowDefinitionSchema,
  BoundWorkflowEventTriggerSchema,
  BoundWorkflowRunSchema,
  BoundWorkflowScheduleSchema,
  BoundWorkflowTargetSchema,
  ListWorkflowProviderEventTriggersResponseSchema,
  ListWorkflowProviderRunsResponseSchema,
  ListWorkflowProviderSchedulesResponseSchema,
  SignalWorkflowRunResponseSchema,
  WorkflowEventMatchSchema,
  WorkflowEventSchema,
  WorkflowEventTriggerInvocationSchema,
  WorkflowManualTriggerSchema,
  WorkflowAgentMessageSchema,
  WorkflowArraySchema,
  WorkflowObjectSchema,
  WorkflowPathSourceSchema,
  WorkflowProvider as WorkflowProviderService,
  WorkflowRunStatus as ProtoWorkflowRunStatus,
  WorkflowRunTriggerSchema,
  WorkflowScheduleTriggerSchema,
  WorkflowSignalSchema,
  type BoundWorkflowDefinition as ProtoBoundWorkflowDefinition,
  type BoundWorkflowEventTrigger as ProtoBoundWorkflowEventTrigger,
  type BoundWorkflowRun as ProtoBoundWorkflowRun,
  type BoundWorkflowSchedule as ProtoBoundWorkflowSchedule,
  type BoundWorkflowTarget as ProtoBoundWorkflowTarget,
  type CancelWorkflowProviderRunRequest as ProtoCancelWorkflowProviderRunRequest,
  type CreateWorkflowProviderDefinitionRequest as ProtoCreateWorkflowProviderDefinitionRequest,
  type DeleteWorkflowProviderDefinitionRequest as ProtoDeleteWorkflowProviderDefinitionRequest,
  type DeleteWorkflowProviderEventTriggerRequest as ProtoDeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest as ProtoDeleteWorkflowProviderScheduleRequest,
  type GetWorkflowProviderDefinitionRequest as ProtoGetWorkflowProviderDefinitionRequest,
  type GetWorkflowProviderEventTriggerRequest as ProtoGetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest as ProtoGetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest as ProtoGetWorkflowProviderScheduleRequest,
  type ListWorkflowProviderEventTriggersRequest as ProtoListWorkflowProviderEventTriggersRequest,
  type ListWorkflowProviderRunsRequest as ProtoListWorkflowProviderRunsRequest,
  type ListWorkflowProviderSchedulesRequest as ProtoListWorkflowProviderSchedulesRequest,
  type PauseWorkflowProviderEventTriggerRequest as ProtoPauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest as ProtoPauseWorkflowProviderScheduleRequest,
  type PublishWorkflowProviderEventRequest as ProtoPublishWorkflowProviderEventRequest,
  type ResumeWorkflowProviderEventTriggerRequest as ProtoResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest as ProtoResumeWorkflowProviderScheduleRequest,
  type SignalOrStartWorkflowProviderRunRequest as ProtoSignalOrStartWorkflowProviderRunRequest,
  type SignalWorkflowProviderRunRequest as ProtoSignalWorkflowProviderRunRequest,
  type SignalWorkflowRunResponse as ProtoSignalWorkflowRunResponse,
  type StartWorkflowProviderRunRequest as ProtoStartWorkflowProviderRunRequest,
  type UpdateWorkflowProviderDefinitionRequest as ProtoUpdateWorkflowProviderDefinitionRequest,
  type UpsertWorkflowProviderEventTriggerRequest as ProtoUpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest as ProtoUpsertWorkflowProviderScheduleRequest,
  type WorkflowEvent as ProtoWorkflowEvent,
  type WorkflowEventMatch as ProtoWorkflowEventMatch,
  type WorkflowEventTriggerInvocation as ProtoWorkflowEventTriggerInvocation,
  WorkflowStepAgentTurnSchema,
  WorkflowStepOutputSourceSchema,
  WorkflowStepAppCallSchema,
  WorkflowStepSchema,
  WorkflowStepWhenSchema,
  WorkflowTextSchema,
  WorkflowValueSchema,
  type WorkflowAgentMessage as ProtoWorkflowAgentMessage,
  type WorkflowRunTrigger as ProtoWorkflowRunTrigger,
  type WorkflowScheduleTrigger as ProtoWorkflowScheduleTrigger,
  type WorkflowSignal as ProtoWorkflowSignal,
  type WorkflowStep as ProtoWorkflowStep,
  type WorkflowStepAgentTurn as ProtoWorkflowStepAgentTurn,
  type WorkflowStepOutputSource as ProtoWorkflowStepOutputSource,
  type WorkflowStepAppCall as ProtoWorkflowStepAppCall,
  type WorkflowStepWhen as ProtoWorkflowStepWhen,
  type WorkflowText as ProtoWorkflowText,
  type WorkflowValue as ProtoWorkflowValue,
} from "./internal/gen/v1/workflow_pb.ts";
import type { AgentOutput, AgentToolRef } from "./agent.ts";
import {
  agentOutputFromProto,
  agentOutputToProto,
  agentToolRefFromProto,
  agentToolRefToProto,
} from "./agent-conversions.ts";
import { errorMessage, type MaybePromise, type Request } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import {
  dateFromTimestamp,
  jsonFromValue,
  jsonObjectFromStruct,
  structFromObject,
  timestampFromDate,
  valueFromJson,
  type JsonInput,
  type JsonObjectInput,
} from "./protocol.ts";
import {
  optionalObjectFromStruct,
  optionalStruct,
} from "./protocol-internal.ts";

type WorkflowProviderServiceImpl = Partial<
  ServiceImpl<typeof WorkflowProviderService>
>;

/** Native workflow-run status constants for authored workflow providers. */
export const WorkflowRunStatus = {
  UNSPECIFIED: ProtoWorkflowRunStatus.UNSPECIFIED,
  PENDING: ProtoWorkflowRunStatus.PENDING,
  RUNNING: ProtoWorkflowRunStatus.RUNNING,
  SUCCEEDED: ProtoWorkflowRunStatus.SUCCEEDED,
  FAILED: ProtoWorkflowRunStatus.FAILED,
  CANCELED: ProtoWorkflowRunStatus.CANCELED,
} as const;
export type WorkflowRunStatus =
  (typeof WorkflowRunStatus)[keyof typeof WorkflowRunStatus];

export interface WorkflowText {
  template?: string | undefined;
}

export interface WorkflowStepOutputSource {
  stepId?: string | undefined;
  path?: string | undefined;
}

export type WorkflowValueKind =
  | { case: "literal"; value: JsonInput }
  | { case: "object"; value: Record<string, WorkflowValue> }
  | { case: "array"; value: readonly WorkflowValue[] }
  | { case: "template"; value: WorkflowText | string }
  | { case: "runInput"; value: string }
  | { case: "signalPayload"; value: string }
  | { case: "stepOutput"; value: WorkflowStepOutputSource }
  | { case: undefined; value?: undefined };

export interface WorkflowValue {
  literal?: JsonInput | undefined;
  object?: Record<string, WorkflowValue> | undefined;
  array?: readonly WorkflowValue[] | undefined;
  template?: WorkflowText | string | undefined;
  runInput?: string | undefined;
  signalPayload?: string | undefined;
  stepOutput?: WorkflowStepOutputSource | undefined;
  kind?: WorkflowValueKind | undefined;
}

export interface WorkflowStepAppCall {
  name?: string | undefined;
  operation?: string | undefined;
  input?: WorkflowValue | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  credentialMode?: string | undefined;
}

export interface WorkflowAgentMessage {
  role?: string | undefined;
  text?: WorkflowText | string | undefined;
  metadata?: JsonObjectInput | undefined;
}

export interface WorkflowStepAgentTurn {
  provider?: string | undefined;
  model?: string | undefined;
  sessionKey?: string | undefined;
  prompt?: WorkflowText | string | undefined;
  messages?: readonly WorkflowAgentMessage[] | undefined;
  tools?: readonly AgentToolRef[] | undefined;
  output: AgentOutput;
  modelOptions?: JsonObjectInput | undefined;
}

export interface WorkflowStepWhen {
  value?: WorkflowValue | undefined;
  equals?: JsonInput | undefined;
}

export interface BoundWorkflowTarget {
  steps?: readonly WorkflowStep[] | undefined;
}

export type WorkflowStepActionKind =
  | { case: "app"; value: WorkflowStepAppCall }
  | { case: "agent"; value: WorkflowStepAgentTurn }
  | { case: undefined; value?: undefined };

function workflowStepAppAction(
  action: WorkflowStepActionKind | undefined,
): WorkflowStepAppCall | undefined {
  return action?.case === "app" ? action.value : undefined;
}

function workflowStepAgentAction(
  action: WorkflowStepActionKind | undefined,
): WorkflowStepAgentTurn | undefined {
  return action?.case === "agent" ? action.value : undefined;
}

export interface WorkflowStep {
  id?: string | undefined;
  inputs?: Record<string, WorkflowValue> | undefined;
  app?: WorkflowStepAppCall | undefined;
  agent?: WorkflowStepAgentTurn | undefined;
  when?: WorkflowStepWhen | undefined;
  timeoutSeconds?: number | undefined;
  metadata?: JsonObjectInput | undefined;
  action?: WorkflowStepActionKind | undefined;
}


export interface WorkflowEvent {
  id?: string | undefined;
  source?: string | undefined;
  specVersion?: string | undefined;
  type?: string | undefined;
  subject?: string | undefined;
  time?: Date | undefined;
  datacontenttype?: string | undefined;
  data?: JsonObjectInput | undefined;
  extensions?: Record<string, JsonInput> | undefined;
}

export interface WorkflowEventMatch {
  type?: string | undefined;
  source?: string | undefined;
  subject?: string | undefined;
}

export interface WorkflowSignal {
  id?: string | undefined;
  name?: string | undefined;
  payload?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  createdBySubjectId?: string | undefined;
  createdAt?: Date | undefined;
  idempotencyKey?: string | undefined;
  sequence?: bigint | number | undefined;
}

export interface WorkflowScheduleTrigger {
  scheduleId?: string | undefined;
  scheduledFor?: Date | undefined;
}

export interface WorkflowEventTriggerInvocation {
  triggerId?: string | undefined;
  event?: WorkflowEvent | undefined;
}

export type WorkflowRunTriggerKind =
  | { case: "manual"; value?: Record<string, never> }
  | { case: "schedule"; value: WorkflowScheduleTrigger }
  | { case: "event"; value: WorkflowEventTriggerInvocation }
  | { case: undefined; value?: undefined };

export interface WorkflowRunTrigger {
  manual?: boolean | undefined;
  schedule?: WorkflowScheduleTrigger | undefined;
  event?: WorkflowEventTriggerInvocation | undefined;
  kind?: WorkflowRunTriggerKind | undefined;
}

export interface BoundWorkflowRun {
  id?: string | undefined;
  status?: WorkflowRunStatus | undefined;
  target?: BoundWorkflowTarget | undefined;
  trigger?: WorkflowRunTrigger | undefined;
  createdAt?: Date | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
  statusMessage?: string | undefined;
  resultBody?: string | undefined;
  createdBySubjectId?: string | undefined;
  workflowKey?: string | undefined;
  providerName?: string | undefined;
  definitionId?: string | undefined;
}

export interface BoundWorkflowSchedule {
  id?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  nextRunAt?: Date | undefined;
  createdBySubjectId?: string | undefined;
  providerName?: string | undefined;
  definitionId?: string | undefined;
}

export interface BoundWorkflowEventTrigger {
  id?: string | undefined;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  createdBySubjectId?: string | undefined;
  providerName?: string | undefined;
  definitionId?: string | undefined;
}

export interface BoundWorkflowDefinition {
  id?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  createdBySubjectId?: string | undefined;
  createdAt?: Date | undefined;
  providerName?: string | undefined;
}

export interface StartWorkflowProviderRunRequest {
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey: string;
  createdBySubjectId?: string | undefined;
  workflowKey: string;
  definitionId?: string | undefined;
}

export interface GetWorkflowProviderRunRequest {
  runId: string;
}

export interface ListWorkflowProviderRunsRequest {
  pageSize?: number | undefined;
  pageToken?: string | undefined;
  status?: WorkflowRunStatus | undefined;
  targetApp?: string | undefined;
}

export interface ListWorkflowProviderRunsResponse {
  runs: readonly BoundWorkflowRun[];
  nextPageToken?: string | undefined;
}

export interface CancelWorkflowProviderRunRequest {
  runId: string;
  reason: string;
}

export interface SignalWorkflowProviderRunRequest {
  runId: string;
  signal?: WorkflowSignal | undefined;
}

export interface SignalOrStartWorkflowProviderRunRequest {
  workflowKey: string;
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey: string;
  createdBySubjectId?: string | undefined;
  signal?: WorkflowSignal | undefined;
  definitionId?: string | undefined;
}

export interface SignalWorkflowRunResponse {
  run?: BoundWorkflowRun | undefined;
  signal?: WorkflowSignal | undefined;
  startedRun: boolean;
  workflowKey: string;
}

export interface CreateWorkflowProviderDefinitionRequest {
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey: string;
  createdBySubjectId?: string | undefined;
}

export interface GetWorkflowProviderDefinitionRequest {
  definitionId: string;
}

export interface UpdateWorkflowProviderDefinitionRequest {
  definitionId: string;
  target?: BoundWorkflowTarget | undefined;
  requestedBySubjectId?: string | undefined;
}

export interface DeleteWorkflowProviderDefinitionRequest {
  definitionId: string;
}

export interface UpsertWorkflowProviderScheduleRequest {
  scheduleId: string;
  cron: string;
  timezone: string;
  target?: BoundWorkflowTarget | undefined;
  paused: boolean;
  requestedBySubjectId?: string | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

export interface GetWorkflowProviderScheduleRequest {
  scheduleId: string;
}

export interface ListWorkflowProviderSchedulesRequest {}

export interface DeleteWorkflowProviderScheduleRequest {
  scheduleId: string;
}

export interface PauseWorkflowProviderScheduleRequest {
  scheduleId: string;
}

export interface ResumeWorkflowProviderScheduleRequest {
  scheduleId: string;
}

export interface UpsertWorkflowProviderEventTriggerRequest {
  triggerId: string;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused: boolean;
  requestedBySubjectId?: string | undefined;
  idempotencyKey?: string | undefined;
  definitionId?: string | undefined;
}

export interface GetWorkflowProviderEventTriggerRequest {
  triggerId: string;
}

export interface ListWorkflowProviderEventTriggersRequest {}

export interface DeleteWorkflowProviderEventTriggerRequest {
  triggerId: string;
}

export interface PauseWorkflowProviderEventTriggerRequest {
  triggerId: string;
}

export interface ResumeWorkflowProviderEventTriggerRequest {
  triggerId: string;
}

export interface PublishWorkflowProviderEventRequest {
  appName: string;
  event?: WorkflowEvent | undefined;
  publishedBySubjectId?: string | undefined;
}

export interface WorkflowSchedule {
  providerName?: string | undefined;
  schedule?: BoundWorkflowSchedule | undefined;
}

export interface WorkflowEventTrigger {
  providerName?: string | undefined;
  trigger?: BoundWorkflowEventTrigger | undefined;
}

export interface WorkflowDefinition {
  providerName?: string | undefined;
  definition?: BoundWorkflowDefinition | undefined;
}

export interface WorkflowRun {
  providerName?: string | undefined;
  run?: BoundWorkflowRun | undefined;
}

export interface WorkflowRunSignal {
  providerName?: string | undefined;
  run?: BoundWorkflowRun | undefined;
  signal?: WorkflowSignal | undefined;
  startedRun?: boolean | undefined;
  workflowKey?: string | undefined;
}

/** Creates workflow event-match fields from native input. */
export function workflowEventMatch(
  input: WorkflowEventMatch = {},
): WorkflowEventMatch {
  return {
    type: input.type ?? "",
    source: input.source ?? "",
    subject: input.subject ?? "",
  };
}

/** Returns native input copied from workflow event-match fields. */
export function workflowEventMatchInputFromMatch(
  input?: WorkflowEventMatch,
): WorkflowEventMatch | undefined {
  return input === undefined ? undefined : { ...input };
}

/** Creates a bound workflow target from native input. */
export function workflowText(input: WorkflowText | string = {}): WorkflowText {
  if (typeof input === "string") {
    return { template: input };
  }
  return { template: input.template ?? "" };
}

/** Returns native input copied from workflow text. */
export function workflowTextInputFromText(
  input?: WorkflowText,
): WorkflowText | undefined {
  return input === undefined ? undefined : { template: input.template };
}

/** Creates a workflow step-output source from native input. */
export function workflowStepOutputSource(
  input: WorkflowStepOutputSource = {},
): WorkflowStepOutputSource {
  return {
    stepId: input.stepId ?? "",
    path: input.path ?? "",
  };
}

/** Returns native input copied from a workflow step-output source. */
export function workflowStepOutputSourceInputFromSource(
  input?: WorkflowStepOutputSource,
): WorkflowStepOutputSource | undefined {
  return input === undefined ? undefined : { ...input };
}

/** Creates a workflow value expression from native input. */
export function workflowValue(input: WorkflowValue = {}): WorkflowValue {
  if (input.kind !== undefined) {
    return { kind: cloneWorkflowValueKind(input.kind) };
  }
  const selected = [
    Object.prototype.hasOwnProperty.call(input, "literal") ? "literal" : undefined,
    input.object === undefined ? undefined : "object",
    input.array === undefined ? undefined : "array",
    input.template === undefined ? undefined : "template",
    input.runInput === undefined ? undefined : "runInput",
    input.signalPayload === undefined ? undefined : "signalPayload",
    input.stepOutput === undefined ? undefined : "stepOutput",
  ].filter((value): value is string => value !== undefined);
  if (selected.length === 0) {
    return { kind: { case: undefined } };
  }
  if (selected.length > 1) {
    throw new Error("workflow value must set exactly one value kind");
  }
  switch (selected[0]) {
    case "literal":
      return { kind: { case: "literal", value: input.literal ?? null } };
    case "object":
      return {
        kind: {
          case: "object",
          value: Object.fromEntries(
            Object.entries(input.object ?? {}).map(([key, value]) => [
              key,
              workflowValue(value),
            ]),
          ),
        },
      };
    case "array":
      return { kind: { case: "array", value: (input.array ?? []).map(workflowValue) } };
    case "template":
      return { kind: { case: "template", value: workflowText(input.template ?? {}) } };
    case "runInput":
      return { kind: { case: "runInput", value: input.runInput ?? "" } };
    case "signalPayload":
      return { kind: { case: "signalPayload", value: input.signalPayload ?? "" } };
    default:
      return {
        kind: {
          case: "stepOutput",
          value: workflowStepOutputSource(input.stepOutput),
        },
      };
  }
}

/** Returns native input copied from a workflow value expression. */
export function workflowValueInputFromValue(
  input?: WorkflowValue,
): WorkflowValue | undefined {
  if (input === undefined) {
    return undefined;
  }
  const kind = input.kind;
  switch (kind?.case) {
    case "literal":
      return { literal: jsonClone(kind.value) };
    case "object":
      return {
        object: Object.fromEntries(
          Object.entries(kind.value).map(([key, value]) => [
            key,
            workflowValueInputFromValue(value)!,
          ]),
        ),
      };
    case "array":
      return { array: kind.value.map((value) => workflowValueInputFromValue(value)!) };
    case "template":
      return { template: workflowTextInputFromText(workflowText(kind.value)) };
    case "runInput":
      return { runInput: kind.value };
    case "signalPayload":
      return { signalPayload: kind.value };
    case "stepOutput":
      return { stepOutput: workflowStepOutputSourceInputFromSource(kind.value) };
    default:
      return {};
  }
}

/** Creates a workflow app step call from native input. */
export function workflowStepAppCall(
  input: WorkflowStepAppCall = {},
): WorkflowStepAppCall {
  return {
    name: input.name ?? "",
    operation: input.operation ?? "",
    input: input.input === undefined ? undefined : workflowValue(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  };
}

/** Returns native input copied from a workflow app step call. */
export function workflowStepAppCallInputFromCall(
  input?: WorkflowStepAppCall,
): WorkflowStepAppCall | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    name: input.name,
    operation: input.operation,
    input: workflowValueInputFromValue(input.input),
    connection: input.connection,
    instance: input.instance,
    credentialMode: input.credentialMode,
  };
}

/** Creates a workflow agent message from native input. */
export function workflowAgentMessage(
  input: WorkflowAgentMessage = {},
): WorkflowAgentMessage {
  return {
    role: input.role ?? "",
    text: input.text === undefined ? undefined : workflowText(input.text),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
  };
}

/** Returns native input copied from a workflow agent message. */
export function workflowAgentMessageInputFromMessage(
  input?: WorkflowAgentMessage,
): WorkflowAgentMessage | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    role: input.role,
    text: workflowTextInputFromText(input.text as WorkflowText | undefined),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
  };
}

/** Creates a workflow agent step turn from native input. */
export function workflowStepAgentTurn(
  input: WorkflowStepAgentTurn,
): WorkflowStepAgentTurn {
  return {
    provider: input.provider ?? "",
    model: input.model ?? "",
    sessionKey: input.sessionKey ?? "",
    prompt: input.prompt === undefined ? undefined : workflowText(input.prompt),
    messages: input.messages?.map(workflowAgentMessage) ?? [],
    tools: [...(input.tools ?? [])],
    output: input.output,
    modelOptions: input.modelOptions === undefined ? undefined : structFromObject(input.modelOptions),
  };
}

/** Returns native input copied from a workflow agent step turn. */
export function workflowStepAgentTurnInputFromTurn(
  input?: WorkflowStepAgentTurn,
): WorkflowStepAgentTurn | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    provider: input.provider,
    model: input.model,
    sessionKey: input.sessionKey,
    prompt: workflowTextInputFromText(input.prompt as WorkflowText | undefined),
    messages: input.messages?.map((message) => workflowAgentMessageInputFromMessage(message)!) ?? [],
    tools: [...(input.tools ?? [])],
    output: workflowAgentOutputInputFromOutput(input.output),
    modelOptions: input.modelOptions === undefined ? undefined : jsonObjectClone(input.modelOptions),
  };
}

/** Creates a condition for running one workflow step. */
export function workflowStepWhen(input: WorkflowStepWhen = {}): WorkflowStepWhen {
  const out: WorkflowStepWhen = {
    value: input.value === undefined ? undefined : workflowValue(input.value),
  };
  if (Object.prototype.hasOwnProperty.call(input, "equals")) {
    out.equals = input.equals === undefined ? null : jsonClone(input.equals);
  }
  return out;
}

/** Returns native input copied from a workflow step condition. */
export function workflowStepWhenInputFromWhen(
  input?: WorkflowStepWhen,
): WorkflowStepWhen | undefined {
  if (input === undefined) {
    return undefined;
  }
  const out: WorkflowStepWhen = {
    value: workflowValueInputFromValue(input.value),
  };
  if (Object.prototype.hasOwnProperty.call(input, "equals")) {
    out.equals = input.equals === undefined ? null : jsonClone(input.equals);
  }
  return out;
}

/** Creates one bound workflow step from native input. */
export function workflowStep(input: WorkflowStep = {}): WorkflowStep {
  const app = input.app ?? workflowStepAppAction(input.action);
  const agent = input.agent ?? workflowStepAgentAction(input.action);
  if (app !== undefined && agent !== undefined) {
    throw new Error("workflow step must set either app or agent");
  }
  const timeoutSeconds = input.timeoutSeconds ?? 0;
  if (
    !Number.isInteger(timeoutSeconds) || timeoutSeconds < 0
  ) {
    throw new Error("workflow step timeoutSeconds must not be negative");
  }
  const action: WorkflowStepActionKind = app !== undefined
    ? { case: "app" as const, value: workflowStepAppCall(app) }
    : agent !== undefined
      ? { case: "agent" as const, value: workflowStepAgentTurn(agent) }
      : { case: undefined };
  return {
    id: input.id ?? "",
    inputs: Object.fromEntries(
      Object.entries(input.inputs ?? {}).map(([key, value]) => [
        key,
        workflowValue(value),
      ]),
    ),
    app: workflowStepAppAction(action),
    agent: workflowStepAgentAction(action),
    action,
    when: input.when === undefined ? undefined : workflowStepWhen(input.when),
    timeoutSeconds,
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
  };
}

/** Returns native input copied from one bound workflow step. */
export function workflowStepInputFromStep(
  input?: WorkflowStep,
): WorkflowStep | undefined {
  if (input === undefined) {
    return undefined;
  }
  const action = input.action;
  const app = input.app ?? workflowStepAppAction(action);
  const agent = input.agent ?? workflowStepAgentAction(action);
  return {
    id: input.id,
    inputs: Object.fromEntries(
      Object.entries(input.inputs ?? {}).map(([key, value]) => [
        key,
        workflowValueInputFromValue(value)!,
      ]),
    ),
    app: workflowStepAppCallInputFromCall(app),
    agent: workflowStepAgentTurnInputFromTurn(agent),
    action: action === undefined
      ? undefined
      : action.case === "app"
        ? { case: "app", value: workflowStepAppCallInputFromCall(action.value)! }
        : action.case === "agent"
          ? { case: "agent", value: workflowStepAgentTurnInputFromTurn(action.value)! }
          : { case: undefined },
    when: workflowStepWhenInputFromWhen(input.when),
    timeoutSeconds: input.timeoutSeconds,
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
  };
}

/** Creates a bound workflow target from native input. */
export function boundWorkflowTarget(input: BoundWorkflowTarget = {}): BoundWorkflowTarget {
  return {
    steps: (input.steps ?? []).map(workflowStep),
  };
}

/** Returns native input copied from a bound workflow target. */
export function boundWorkflowTargetInputFromTarget(
  input?: BoundWorkflowTarget,
): BoundWorkflowTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    steps: input.steps?.map((step) => workflowStepInputFromStep(step)!),
  };
}

/** Returns a deep copy of a bound workflow target. */
export function boundWorkflowTargetFromTarget(input: BoundWorkflowTarget): BoundWorkflowTarget {
  return boundWorkflowTarget(boundWorkflowTargetInputFromTarget(input) ?? {});
}

/** Creates a workflow event from native input. */
export function workflowEvent(input: WorkflowEvent = {}): WorkflowEvent {
  return {
    id: input.id ?? "",
    source: input.source ?? "",
    specVersion: input.specVersion ?? "",
    type: input.type ?? "",
    subject: input.subject ?? "",
    time: input.time,
    datacontenttype: input.datacontenttype ?? "",
    data: input.data === undefined ? undefined : structFromObject(input.data),
    extensions: valueMapInput(input.extensions),
  };
}

/** Returns native input copied from a workflow event. */
export function workflowEventInputFromEvent(input?: WorkflowEvent): WorkflowEvent | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    source: input.source,
    specVersion: input.specVersion,
    type: input.type,
    subject: input.subject,
    time: input.time,
    datacontenttype: input.datacontenttype,
    data: input.data === undefined ? undefined : jsonObjectClone(input.data),
    extensions: input.extensions === undefined ? undefined : { ...input.extensions },
  };
}

/** Returns a deep copy of a workflow event. */
export function workflowEventFromEvent(input: WorkflowEvent): WorkflowEvent {
  return workflowEvent(workflowEventInputFromEvent(input) ?? {});
}

/** Creates a workflow signal from native input. */
export function workflowSignal(input: WorkflowSignal = {}): WorkflowSignal {
  return {
    id: input.id ?? "",
    name: input.name ?? "",
    payload: input.payload === undefined ? undefined : structFromObject(input.payload),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
    createdBySubjectId: input.createdBySubjectId?.trim() ?? "",
    createdAt: input.createdAt,
    idempotencyKey: input.idempotencyKey ?? "",
    sequence: input.sequence === undefined ? 0n : BigInt(input.sequence),
  };
}

/** Returns native input copied from a workflow signal. */
export function workflowSignalInputFromSignal(input?: WorkflowSignal): WorkflowSignal | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    name: input.name,
    payload: input.payload === undefined ? undefined : jsonObjectClone(input.payload),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
    createdBySubjectId: input.createdBySubjectId,
    createdAt: input.createdAt,
    idempotencyKey: input.idempotencyKey,
    sequence: input.sequence,
  };
}

/** Returns a deep copy of a workflow signal. */
export function workflowSignalFromSignal(input: WorkflowSignal): WorkflowSignal {
  return workflowSignal(workflowSignalInputFromSignal(input) ?? {});
}

/** Creates a workflow schedule trigger from native input. */
export function workflowScheduleTrigger(
  input: WorkflowScheduleTrigger = {},
): WorkflowScheduleTrigger {
  return {
    scheduleId: input.scheduleId ?? "",
    scheduledFor: input.scheduledFor,
  };
}

/** Creates a workflow event-trigger invocation from native input. */
export function workflowEventTriggerInvocation(
  input: WorkflowEventTriggerInvocation = {},
): WorkflowEventTriggerInvocation {
  return {
    triggerId: input.triggerId ?? "",
    event: input.event === undefined ? undefined : workflowEvent(input.event),
  };
}

/** Creates a workflow run trigger from native input. */
export function workflowRunTrigger(
  input: WorkflowRunTrigger = {},
): WorkflowRunTrigger {
  if ("kind" in input && input.kind !== undefined) {
    return workflowRunTriggerFromTrigger({ kind: input.kind });
  }
  const triggerInput = input as WorkflowRunTrigger;
  const selected = [
    triggerInput.manual === true ? "manual" : undefined,
    triggerInput.schedule === undefined ? undefined : "schedule",
    triggerInput.event === undefined ? undefined : "event",
  ].filter((value): value is string => value !== undefined);
  if (selected.length === 0) {
    return { kind: { case: undefined } };
  }
  if (selected.length > 1) {
    throw new Error("workflow run trigger must set exactly one trigger kind");
  }
  switch (selected[0]) {
    case "manual":
      return { kind: { case: "manual", value: {} } };
    case "schedule":
      return { kind: { case: "schedule", value: workflowScheduleTrigger(triggerInput.schedule!) } };
    default:
      return { kind: { case: "event", value: workflowEventTriggerInvocation(triggerInput.event!) } };
  }
}

/** Returns native input copied from a workflow run trigger. */
export function workflowRunTriggerInputFromTrigger(
  input?: WorkflowRunTrigger,
): WorkflowRunTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  const kind = input.kind;
  switch (kind?.case) {
    case "manual":
      return { manual: true };
    case "schedule":
      return { schedule: { ...kind.value } };
    case "event":
      return {
        event: {
          triggerId: kind.value.triggerId,
          event: workflowEventInputFromEvent(kind.value.event),
        },
      };
    default:
      return {};
  }
}

/** Returns a deep copy of a workflow run trigger. */
export function workflowRunTriggerFromTrigger(input: WorkflowRunTrigger): WorkflowRunTrigger {
  return workflowRunTrigger(workflowRunTriggerInputFromTrigger(input) ?? {});
}

/** Creates a workflow-provider run from native input. */
export function boundWorkflowRun(input: BoundWorkflowRun = {}): BoundWorkflowRun {
  return {
    id: input.id ?? "",
    status: input.status ?? WorkflowRunStatus.UNSPECIFIED,
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    trigger: input.trigger === undefined ? undefined : workflowRunTrigger(input.trigger),
    createdAt: input.createdAt,
    startedAt: input.startedAt,
    completedAt: input.completedAt,
    statusMessage: input.statusMessage ?? "",
    resultBody: input.resultBody ?? "",
    createdBySubjectId: input.createdBySubjectId?.trim() ?? "",
    workflowKey: input.workflowKey ?? "",
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
  };
}

/** Returns native input copied from a workflow-provider run. */
export function boundWorkflowRunInputFromRun(input?: BoundWorkflowRun): BoundWorkflowRun | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    ...input,
    target: boundWorkflowTargetInputFromTarget(input.target) === undefined
      ? undefined
      : boundWorkflowTarget(input.target!),
    trigger: workflowRunTriggerInputFromTrigger(input.trigger) === undefined
      ? undefined
      : workflowRunTrigger(input.trigger!),
    createdBySubjectId: input.createdBySubjectId,
  };
}

/** Returns a deep copy of a workflow-provider run. */
export function boundWorkflowRunFromRun(input: BoundWorkflowRun): BoundWorkflowRun {
  return boundWorkflowRun(boundWorkflowRunInputFromRun(input) ?? {});
}

/** Creates a workflow-provider definition from native input. */
export function boundWorkflowDefinition(
  input: BoundWorkflowDefinition = {},
): BoundWorkflowDefinition {
  return {
    id: input.id ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    createdBySubjectId: input.createdBySubjectId?.trim() ?? "",
    createdAt: input.createdAt,
    providerName: input.providerName ?? "",
  };
}

/** Returns native input copied from a workflow-provider definition. */
export function boundWorkflowDefinitionInputFromDefinition(
  input?: BoundWorkflowDefinition,
): BoundWorkflowDefinition | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    ...input,
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    createdBySubjectId: input.createdBySubjectId,
  };
}

/** Returns a deep copy of a workflow-provider definition. */
export function boundWorkflowDefinitionFromDefinition(
  input: BoundWorkflowDefinition,
): BoundWorkflowDefinition {
  return boundWorkflowDefinition(boundWorkflowDefinitionInputFromDefinition(input) ?? {});
}

/** Creates a workflow-provider schedule from native input. */
export function boundWorkflowSchedule(
  input: BoundWorkflowSchedule = {},
): BoundWorkflowSchedule {
  return {
    id: input.id ?? "",
    cron: input.cron ?? "",
    timezone: input.timezone ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    paused: input.paused ?? false,
    createdAt: input.createdAt,
    updatedAt: input.updatedAt,
    nextRunAt: input.nextRunAt,
    createdBySubjectId: input.createdBySubjectId?.trim() ?? "",
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
  };
}

/** Returns native input copied from a workflow-provider schedule. */
export function boundWorkflowScheduleInputFromSchedule(
  input?: BoundWorkflowSchedule,
): BoundWorkflowSchedule | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    ...input,
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    createdBySubjectId: input.createdBySubjectId,
  };
}

/** Returns a deep copy of a workflow-provider schedule. */
export function boundWorkflowScheduleFromSchedule(
  input: BoundWorkflowSchedule,
): BoundWorkflowSchedule {
  return boundWorkflowSchedule(boundWorkflowScheduleInputFromSchedule(input) ?? {});
}

/** Creates a workflow-provider event trigger from native input. */
export function boundWorkflowEventTrigger(
  input: BoundWorkflowEventTrigger = {},
): BoundWorkflowEventTrigger {
  return {
    id: input.id ?? "",
    match: input.match === undefined ? undefined : workflowEventMatch(input.match),
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    paused: input.paused ?? false,
    createdAt: input.createdAt,
    updatedAt: input.updatedAt,
    createdBySubjectId: input.createdBySubjectId?.trim() ?? "",
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
  };
}

/** Returns native input copied from a workflow-provider event trigger. */
export function boundWorkflowEventTriggerInputFromTrigger(
  input?: BoundWorkflowEventTrigger,
): BoundWorkflowEventTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    ...input,
    match: workflowEventMatchInputFromMatch(input.match),
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    createdBySubjectId: input.createdBySubjectId,
  };
}

/** Returns a deep copy of a workflow-provider event trigger. */
export function boundWorkflowEventTriggerFromTrigger(
  input: BoundWorkflowEventTrigger,
): BoundWorkflowEventTrigger {
  return boundWorkflowEventTrigger(boundWorkflowEventTriggerInputFromTrigger(input) ?? {});
}

/** Handlers and runtime metadata for a workflow provider. */
export interface WorkflowProviderOptions extends ProviderBaseOptions {
  createDefinition: (
    request: CreateWorkflowProviderDefinitionRequest,
  ) => MaybePromise<BoundWorkflowDefinition>;
  getDefinition: (
    request: GetWorkflowProviderDefinitionRequest,
  ) => MaybePromise<BoundWorkflowDefinition>;
  updateDefinition: (
    request: UpdateWorkflowProviderDefinitionRequest,
  ) => MaybePromise<BoundWorkflowDefinition>;
  deleteDefinition: (
    request: DeleteWorkflowProviderDefinitionRequest,
  ) => MaybePromise<void>;
  startRun: (
    request: StartWorkflowProviderRunRequest,
  ) => MaybePromise<BoundWorkflowRun>;
  getRun: (
    request: GetWorkflowProviderRunRequest,
  ) => MaybePromise<BoundWorkflowRun>;
  listRuns: (
    request: ListWorkflowProviderRunsRequest,
  ) => MaybePromise<readonly BoundWorkflowRun[] | ListWorkflowProviderRunsResponse>;
  cancelRun: (
    request: CancelWorkflowProviderRunRequest,
  ) => MaybePromise<BoundWorkflowRun>;
  signalRun: (
    request: SignalWorkflowProviderRunRequest,
  ) => MaybePromise<SignalWorkflowRunResponse>;
  signalOrStartRun: (
    request: SignalOrStartWorkflowProviderRunRequest,
  ) => MaybePromise<SignalWorkflowRunResponse>;
  upsertSchedule: (
    request: UpsertWorkflowProviderScheduleRequest,
  ) => MaybePromise<BoundWorkflowSchedule>;
  getSchedule: (
    request: GetWorkflowProviderScheduleRequest,
  ) => MaybePromise<BoundWorkflowSchedule>;
  listSchedules: (
    request: ListWorkflowProviderSchedulesRequest,
  ) => MaybePromise<readonly BoundWorkflowSchedule[]>;
  deleteSchedule: (
    request: DeleteWorkflowProviderScheduleRequest,
  ) => MaybePromise<void>;
  pauseSchedule: (
    request: PauseWorkflowProviderScheduleRequest,
  ) => MaybePromise<BoundWorkflowSchedule>;
  resumeSchedule: (
    request: ResumeWorkflowProviderScheduleRequest,
  ) => MaybePromise<BoundWorkflowSchedule>;
  upsertEventTrigger: (
    request: UpsertWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<BoundWorkflowEventTrigger>;
  getEventTrigger: (
    request: GetWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<BoundWorkflowEventTrigger>;
  listEventTriggers: (
    request: ListWorkflowProviderEventTriggersRequest,
  ) => MaybePromise<readonly BoundWorkflowEventTrigger[]>;
  deleteEventTrigger: (
    request: DeleteWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<void>;
  pauseEventTrigger: (
    request: PauseWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<BoundWorkflowEventTrigger>;
  resumeEventTrigger: (
    request: ResumeWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<BoundWorkflowEventTrigger>;
  publishEvent: (
    request: PublishWorkflowProviderEventRequest,
  ) => MaybePromise<WorkflowEvent>;
}

/** Runtime provider implementation for the Gestalt workflow host contract. */
export class WorkflowProvider extends ProviderBase {
  readonly kind = "workflow" as const;

  private readonly createDefinitionHandler: WorkflowProviderOptions["createDefinition"];
  private readonly getDefinitionHandler: WorkflowProviderOptions["getDefinition"];
  private readonly updateDefinitionHandler: WorkflowProviderOptions["updateDefinition"];
  private readonly deleteDefinitionHandler: WorkflowProviderOptions["deleteDefinition"];
  private readonly startRunHandler: WorkflowProviderOptions["startRun"];
  private readonly getRunHandler: WorkflowProviderOptions["getRun"];
  private readonly listRunsHandler: WorkflowProviderOptions["listRuns"];
  private readonly cancelRunHandler: WorkflowProviderOptions["cancelRun"];
  private readonly signalRunHandler: WorkflowProviderOptions["signalRun"];
  private readonly signalOrStartRunHandler: WorkflowProviderOptions["signalOrStartRun"];
  private readonly upsertScheduleHandler: WorkflowProviderOptions["upsertSchedule"];
  private readonly getScheduleHandler: WorkflowProviderOptions["getSchedule"];
  private readonly listSchedulesHandler: WorkflowProviderOptions["listSchedules"];
  private readonly deleteScheduleHandler: WorkflowProviderOptions["deleteSchedule"];
  private readonly pauseScheduleHandler: WorkflowProviderOptions["pauseSchedule"];
  private readonly resumeScheduleHandler: WorkflowProviderOptions["resumeSchedule"];
  private readonly upsertEventTriggerHandler: WorkflowProviderOptions["upsertEventTrigger"];
  private readonly getEventTriggerHandler: WorkflowProviderOptions["getEventTrigger"];
  private readonly listEventTriggersHandler: WorkflowProviderOptions["listEventTriggers"];
  private readonly deleteEventTriggerHandler: WorkflowProviderOptions["deleteEventTrigger"];
  private readonly pauseEventTriggerHandler: WorkflowProviderOptions["pauseEventTrigger"];
  private readonly resumeEventTriggerHandler: WorkflowProviderOptions["resumeEventTrigger"];
  private readonly publishEventHandler: WorkflowProviderOptions["publishEvent"];

  constructor(options: WorkflowProviderOptions) {
    super(options);
    this.createDefinitionHandler = options.createDefinition;
    this.getDefinitionHandler = options.getDefinition;
    this.updateDefinitionHandler = options.updateDefinition;
    this.deleteDefinitionHandler = options.deleteDefinition;
    this.startRunHandler = options.startRun;
    this.getRunHandler = options.getRun;
    this.listRunsHandler = options.listRuns;
    this.cancelRunHandler = options.cancelRun;
    this.signalRunHandler = options.signalRun;
    this.signalOrStartRunHandler = options.signalOrStartRun;
    this.upsertScheduleHandler = options.upsertSchedule;
    this.getScheduleHandler = options.getSchedule;
    this.listSchedulesHandler = options.listSchedules;
    this.deleteScheduleHandler = options.deleteSchedule;
    this.pauseScheduleHandler = options.pauseSchedule;
    this.resumeScheduleHandler = options.resumeSchedule;
    this.upsertEventTriggerHandler = options.upsertEventTrigger;
    this.getEventTriggerHandler = options.getEventTrigger;
    this.listEventTriggersHandler = options.listEventTriggers;
    this.deleteEventTriggerHandler = options.deleteEventTrigger;
    this.pauseEventTriggerHandler = options.pauseEventTrigger;
    this.resumeEventTriggerHandler = options.resumeEventTrigger;
    this.publishEventHandler = options.publishEvent;
  }

  async createDefinition(
    request: CreateWorkflowProviderDefinitionRequest,
  ): Promise<BoundWorkflowDefinition> {
    return await this.createDefinitionHandler(request);
  }

  async getDefinition(
    request: GetWorkflowProviderDefinitionRequest,
  ): Promise<BoundWorkflowDefinition> {
    return await this.getDefinitionHandler(request);
  }

  async updateDefinition(
    request: UpdateWorkflowProviderDefinitionRequest,
  ): Promise<BoundWorkflowDefinition> {
    return await this.updateDefinitionHandler(request);
  }

  async deleteDefinition(request: DeleteWorkflowProviderDefinitionRequest): Promise<void> {
    await this.deleteDefinitionHandler(request);
  }

  async startRun(request: StartWorkflowProviderRunRequest): Promise<BoundWorkflowRun> {
    return await this.startRunHandler(request);
  }

  async getRun(request: GetWorkflowProviderRunRequest): Promise<BoundWorkflowRun> {
    return await this.getRunHandler(request);
  }

  async listRuns(
    request: ListWorkflowProviderRunsRequest,
  ): Promise<readonly BoundWorkflowRun[] | ListWorkflowProviderRunsResponse> {
    return await this.listRunsHandler(request);
  }

  async cancelRun(request: CancelWorkflowProviderRunRequest): Promise<BoundWorkflowRun> {
    return await this.cancelRunHandler(request);
  }

  async signalRun(request: SignalWorkflowProviderRunRequest): Promise<SignalWorkflowRunResponse> {
    return await this.signalRunHandler(request);
  }

  async signalOrStartRun(
    request: SignalOrStartWorkflowProviderRunRequest,
  ): Promise<SignalWorkflowRunResponse> {
    return await this.signalOrStartRunHandler(request);
  }

  async upsertSchedule(request: UpsertWorkflowProviderScheduleRequest): Promise<BoundWorkflowSchedule> {
    return await this.upsertScheduleHandler(request);
  }

  async getSchedule(request: GetWorkflowProviderScheduleRequest): Promise<BoundWorkflowSchedule> {
    return await this.getScheduleHandler(request);
  }

  async listSchedules(
    request: ListWorkflowProviderSchedulesRequest,
  ): Promise<readonly BoundWorkflowSchedule[]> {
    return await this.listSchedulesHandler(request);
  }

  async deleteSchedule(request: DeleteWorkflowProviderScheduleRequest): Promise<void> {
    await this.deleteScheduleHandler(request);
  }

  async pauseSchedule(request: PauseWorkflowProviderScheduleRequest): Promise<BoundWorkflowSchedule> {
    return await this.pauseScheduleHandler(request);
  }

  async resumeSchedule(request: ResumeWorkflowProviderScheduleRequest): Promise<BoundWorkflowSchedule> {
    return await this.resumeScheduleHandler(request);
  }

  async upsertEventTrigger(
    request: UpsertWorkflowProviderEventTriggerRequest,
  ): Promise<BoundWorkflowEventTrigger> {
    return await this.upsertEventTriggerHandler(request);
  }

  async getEventTrigger(
    request: GetWorkflowProviderEventTriggerRequest,
  ): Promise<BoundWorkflowEventTrigger> {
    return await this.getEventTriggerHandler(request);
  }

  async listEventTriggers(
    request: ListWorkflowProviderEventTriggersRequest,
  ): Promise<readonly BoundWorkflowEventTrigger[]> {
    return await this.listEventTriggersHandler(request);
  }

  async deleteEventTrigger(request: DeleteWorkflowProviderEventTriggerRequest): Promise<void> {
    await this.deleteEventTriggerHandler(request);
  }

  async pauseEventTrigger(
    request: PauseWorkflowProviderEventTriggerRequest,
  ): Promise<BoundWorkflowEventTrigger> {
    return await this.pauseEventTriggerHandler(request);
  }

  async resumeEventTrigger(
    request: ResumeWorkflowProviderEventTriggerRequest,
  ): Promise<BoundWorkflowEventTrigger> {
    return await this.resumeEventTriggerHandler(request);
  }

  async publishEvent(request: PublishWorkflowProviderEventRequest): Promise<WorkflowEvent> {
    return await this.publishEventHandler(request);
  }
}

/** Creates a workflow provider for export from a provider module. */
export function defineWorkflowProvider(
  options: WorkflowProviderOptions,
): WorkflowProvider {
  return new WorkflowProvider(options);
}

/** Runtime type guard for workflow providers loaded from user modules. */
export function isWorkflowProvider(value: unknown): value is WorkflowProvider {
  return (
    value instanceof WorkflowProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "workflow" &&
      "createDefinition" in value &&
      "getDefinition" in value &&
      "updateDefinition" in value &&
      "deleteDefinition" in value &&
      "startRun" in value &&
      "getRun" in value &&
      "listRuns" in value &&
      "cancelRun" in value &&
      "signalRun" in value &&
      "signalOrStartRun" in value &&
      "upsertSchedule" in value &&
      "getSchedule" in value &&
      "listSchedules" in value &&
      "deleteSchedule" in value &&
      "pauseSchedule" in value &&
      "resumeSchedule" in value &&
      "upsertEventTrigger" in value &&
      "getEventTrigger" in value &&
      "listEventTriggers" in value &&
      "deleteEventTrigger" in value &&
      "pauseEventTrigger" in value &&
      "resumeEventTrigger" in value &&
      "publishEvent" in value)
  );
}

/** Builds the Connect service implementation used by the TypeScript runtime. */
export function createWorkflowProviderService(
  provider: WorkflowProvider,
): WorkflowProviderServiceImpl {
  return {
    async createDefinition(request) {
      return create(
        BoundWorkflowDefinitionSchema,
        boundWorkflowDefinitionToProto(
          await invokeWorkflowProvider("create definition", () =>
            provider.createDefinition(createWorkflowProviderDefinitionRequestFromProto(request)),
          ),
        ),
      );
    },
    async getDefinition(request) {
      return create(
        BoundWorkflowDefinitionSchema,
        boundWorkflowDefinitionToProto(
          await invokeWorkflowProvider("get definition", () =>
            provider.getDefinition(getWorkflowProviderDefinitionRequestFromProto(request)),
          ),
        ),
      );
    },
    async updateDefinition(request) {
      return create(
        BoundWorkflowDefinitionSchema,
        boundWorkflowDefinitionToProto(
          await invokeWorkflowProvider("update definition", () =>
            provider.updateDefinition(updateWorkflowProviderDefinitionRequestFromProto(request)),
          ),
        ),
      );
    },
    async deleteDefinition(request) {
      await invokeWorkflowProvider("delete definition", () =>
        provider.deleteDefinition(deleteWorkflowProviderDefinitionRequestFromProto(request)),
      );
      return create(EmptySchema, {});
    },
    async startRun(request) {
      return create(
        BoundWorkflowRunSchema,
        boundWorkflowRunToProto(
          await invokeWorkflowProvider("start run", () =>
            provider.startRun(startWorkflowProviderRunRequestFromProto(request)),
          ),
        ),
      );
    },
    async getRun(request) {
      return create(
        BoundWorkflowRunSchema,
        boundWorkflowRunToProto(
          await invokeWorkflowProvider("get run", () =>
            provider.getRun(getWorkflowProviderRunRequestFromProto(request)),
          ),
        ),
      );
    },
    async listRuns(request) {
      const response = await invokeWorkflowProvider("list runs", () =>
        provider.listRuns(listWorkflowProviderRunsRequestFromProto(request)),
      );
      const result = listRunsResult(response);
      return create(ListWorkflowProviderRunsResponseSchema, {
        runs: result.runs.map(boundWorkflowRunToProto),
        nextPageToken: result.nextPageToken ?? "",
      });
    },
    async cancelRun(request) {
      return create(
        BoundWorkflowRunSchema,
        boundWorkflowRunToProto(
          await invokeWorkflowProvider("cancel run", () =>
            provider.cancelRun(cancelWorkflowProviderRunRequestFromProto(request)),
          ),
        ),
      );
    },
    async signalRun(request) {
      return create(
        SignalWorkflowRunResponseSchema,
        signalWorkflowRunResponseToProto(
          await invokeWorkflowProvider("signal run", () =>
            provider.signalRun(signalWorkflowProviderRunRequestFromProto(request)),
          ),
        ),
      );
    },
    async signalOrStartRun(request) {
      return create(
        SignalWorkflowRunResponseSchema,
        signalWorkflowRunResponseToProto(
          await invokeWorkflowProvider("signal or start run", () =>
            provider.signalOrStartRun(signalOrStartWorkflowProviderRunRequestFromProto(request)),
          ),
        ),
      );
    },
    async upsertSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        boundWorkflowScheduleToProto(
          await invokeWorkflowProvider("upsert schedule", () =>
            provider.upsertSchedule(upsertWorkflowProviderScheduleRequestFromProto(request)),
          ),
        ),
      );
    },
    async getSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        boundWorkflowScheduleToProto(
          await invokeWorkflowProvider("get schedule", () =>
            provider.getSchedule(getWorkflowProviderScheduleRequestFromProto(request)),
          ),
        ),
      );
    },
    async listSchedules(request) {
      return create(ListWorkflowProviderSchedulesResponseSchema, {
        schedules: (
          await invokeWorkflowProvider("list schedules", () =>
            provider.listSchedules(listWorkflowProviderSchedulesRequestFromProto(request)),
          )
        ).map(boundWorkflowScheduleToProto),
      });
    },
    async deleteSchedule(request) {
      await invokeWorkflowProvider("delete schedule", () =>
        provider.deleteSchedule(deleteWorkflowProviderScheduleRequestFromProto(request)),
      );
      return create(EmptySchema, {});
    },
    async pauseSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        boundWorkflowScheduleToProto(
          await invokeWorkflowProvider("pause schedule", () =>
            provider.pauseSchedule(pauseWorkflowProviderScheduleRequestFromProto(request)),
          ),
        ),
      );
    },
    async resumeSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        boundWorkflowScheduleToProto(
          await invokeWorkflowProvider("resume schedule", () =>
            provider.resumeSchedule(resumeWorkflowProviderScheduleRequestFromProto(request)),
          ),
        ),
      );
    },
    async upsertEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        boundWorkflowEventTriggerToProto(
          await invokeWorkflowProvider("upsert event trigger", () =>
            provider.upsertEventTrigger(upsertWorkflowProviderEventTriggerRequestFromProto(request)),
          ),
        ),
      );
    },
    async getEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        boundWorkflowEventTriggerToProto(
          await invokeWorkflowProvider("get event trigger", () =>
            provider.getEventTrigger(getWorkflowProviderEventTriggerRequestFromProto(request)),
          ),
        ),
      );
    },
    async listEventTriggers(request) {
      return create(ListWorkflowProviderEventTriggersResponseSchema, {
        triggers: (
          await invokeWorkflowProvider("list event triggers", () =>
            provider.listEventTriggers(listWorkflowProviderEventTriggersRequestFromProto(request)),
          )
        ).map(boundWorkflowEventTriggerToProto),
      });
    },
    async deleteEventTrigger(request) {
      await invokeWorkflowProvider("delete event trigger", () =>
        provider.deleteEventTrigger(deleteWorkflowProviderEventTriggerRequestFromProto(request)),
      );
      return create(EmptySchema, {});
    },
    async pauseEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        boundWorkflowEventTriggerToProto(
          await invokeWorkflowProvider("pause event trigger", () =>
            provider.pauseEventTrigger(pauseWorkflowProviderEventTriggerRequestFromProto(request)),
          ),
        ),
      );
    },
    async resumeEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        boundWorkflowEventTriggerToProto(
          await invokeWorkflowProvider("resume event trigger", () =>
            provider.resumeEventTrigger(resumeWorkflowProviderEventTriggerRequestFromProto(request)),
          ),
        ),
      );
    },
    async publishEvent(request) {
      return workflowEventToProto(
        await invokeWorkflowProvider("publish event", () =>
          provider.publishEvent(publishWorkflowProviderEventRequestFromProto(request)),
        ),
      ) ?? create(WorkflowEventSchema, {});
    },
  };
}

export function workflowEventMatchToProto(
  input?: WorkflowEventMatch | undefined,
): ProtoWorkflowEventMatch | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowEventMatchSchema, {
    type: input.type ?? "",
    source: input.source ?? "",
    subject: input.subject ?? "",
  });
}

export function workflowEventMatchFromProto(
  input?: ProtoWorkflowEventMatch | undefined,
): WorkflowEventMatch | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    type: input.type,
    source: input.source,
    subject: input.subject,
  };
}

export function workflowTextToProto(
  input?: WorkflowText | string | undefined,
): ProtoWorkflowText | undefined {
  if (input === undefined) {
    return undefined;
  }
  const text = workflowText(input);
  return create(WorkflowTextSchema, { template: text.template ?? "" });
}

export function workflowTextFromProto(
  input?: ProtoWorkflowText | undefined,
): WorkflowText | undefined {
  return input === undefined ? undefined : { template: input.template };
}

export function workflowStepOutputSourceToProto(
  input?: WorkflowStepOutputSource | undefined,
): ProtoWorkflowStepOutputSource | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowStepOutputSourceSchema, {
    stepId: input.stepId ?? "",
    path: input.path ?? "",
  });
}

export function workflowStepOutputSourceFromProto(
  input?: ProtoWorkflowStepOutputSource | undefined,
): WorkflowStepOutputSource | undefined {
  return input === undefined
    ? undefined
    : { stepId: input.stepId, path: input.path };
}

export function workflowValueToProto(
  input?: WorkflowValue | undefined,
): ProtoWorkflowValue | undefined {
  if (input === undefined) {
    return undefined;
  }
  const value = workflowValue(input);
  const kind = value.kind;
  switch (kind?.case) {
    case "literal":
      return create(WorkflowValueSchema, {
        kind: { case: "literal", value: valueFromJson(kind.value) },
      });
    case "object":
      return create(WorkflowValueSchema, {
        kind: {
          case: "object",
          value: create(WorkflowObjectSchema, {
            fields: Object.fromEntries(
              Object.entries(kind.value).map(([key, nested]) => [
                key,
                workflowValueToProto(nested)!,
              ]),
            ),
          }),
        },
      });
    case "array":
      return create(WorkflowValueSchema, {
        kind: {
          case: "array",
          value: create(WorkflowArraySchema, {
            values: kind.value.map((nested) => workflowValueToProto(nested)!),
          }),
        },
      });
    case "template":
      return create(WorkflowValueSchema, {
        kind: { case: "template", value: workflowTextToProto(kind.value)! },
      });
    case "runInput":
      return create(WorkflowValueSchema, {
        kind: {
          case: "runInput",
          value: create(WorkflowPathSourceSchema, { path: kind.value }),
        },
      });
    case "signalPayload":
      return create(WorkflowValueSchema, {
        kind: {
          case: "signalPayload",
          value: create(WorkflowPathSourceSchema, { path: kind.value }),
        },
      });
    case "stepOutput":
      return create(WorkflowValueSchema, {
        kind: { case: "stepOutput", value: workflowStepOutputSourceToProto(kind.value)! },
      });
    default:
      return create(WorkflowValueSchema);
  }
}

export function workflowValueFromProto(
  input?: ProtoWorkflowValue | undefined,
): WorkflowValue | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "literal":
      return { kind: { case: "literal", value: jsonFromValue(input.kind.value) as JsonInput } };
    case "object":
      return {
        kind: {
          case: "object",
          value: Object.fromEntries(
            Object.entries(input.kind.value.fields).map(([key, nested]) => [
              key,
              workflowValueFromProto(nested)!,
            ]),
          ),
        },
      };
    case "array":
      return {
        kind: {
          case: "array",
          value: input.kind.value.values.map((nested) => workflowValueFromProto(nested)!),
        },
      };
    case "template":
      return { kind: { case: "template", value: workflowTextFromProto(input.kind.value)! } };
    case "runInput":
      return { kind: { case: "runInput", value: input.kind.value.path } };
    case "signalPayload":
      return { kind: { case: "signalPayload", value: input.kind.value.path } };
    case "stepOutput":
      return { kind: { case: "stepOutput", value: workflowStepOutputSourceFromProto(input.kind.value)! } };
    default:
      return { kind: { case: undefined } };
  }
}

export function workflowStepAppCallToProto(
  input?: WorkflowStepAppCall | undefined,
): ProtoWorkflowStepAppCall | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowStepAppCallSchema, {
    name: input.name ?? "",
    operation: input.operation ?? "",
    input: workflowValueToProto(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  });
}

export function workflowStepAppCallFromProto(
  input?: ProtoWorkflowStepAppCall | undefined,
): WorkflowStepAppCall | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    name: input.name,
    operation: input.operation,
    input: workflowValueFromProto(input.input),
    connection: input.connection,
    instance: input.instance,
    credentialMode: input.credentialMode,
  };
}

export function workflowAgentMessageToProto(
  input: WorkflowAgentMessage,
): ProtoWorkflowAgentMessage {
  return create(WorkflowAgentMessageSchema, {
    role: input.role ?? "",
    text: workflowTextToProto(input.text),
    metadata: optionalStruct(input.metadata),
  });
}

export function workflowAgentMessageFromProto(
  input: ProtoWorkflowAgentMessage,
): WorkflowAgentMessage {
  return {
    role: input.role,
    text: workflowTextFromProto(input.text),
    metadata: optionalObjectFromStruct(input.metadata),
  };
}

export function workflowStepAgentTurnToProto(
  input?: WorkflowStepAgentTurn | undefined,
): ProtoWorkflowStepAgentTurn | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowStepAgentTurnSchema, {
    provider: input.provider ?? "",
    model: input.model ?? "",
    sessionKey: input.sessionKey ?? "",
    prompt: workflowTextToProto(input.prompt),
    messages: input.messages?.map(workflowAgentMessageToProto) ?? [],
    tools: input.tools?.map(agentToolRefToProto) ?? [],
    output: agentOutputToProto(input.output),
    modelOptions: optionalStruct(input.modelOptions),
  });
}

export function workflowStepAgentTurnFromProto(
	input?: ProtoWorkflowStepAgentTurn | undefined,
): WorkflowStepAgentTurn | undefined {
  if (input === undefined) {
    return undefined;
  }
  const output = agentOutputFromProto(input.output);
  if (output === undefined) {
    throw new Error("workflow agent output is required");
  }
  return {
    provider: input.provider,
    model: input.model,
    sessionKey: input.sessionKey,
    prompt: workflowTextFromProto(input.prompt),
    messages: input.messages.map(workflowAgentMessageFromProto),
    tools: input.tools.map(agentToolRefFromProto),
    output,
    modelOptions: optionalObjectFromStruct(input.modelOptions),
  };
}

export function workflowStepWhenToProto(
  input?: WorkflowStepWhen | undefined,
): ProtoWorkflowStepWhen | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowStepWhenSchema, {
    value: workflowValueToProto(input.value),
    equals: Object.prototype.hasOwnProperty.call(input, "equals")
      ? valueFromJson(input.equals ?? null)
      : undefined,
  });
}

export function workflowStepWhenFromProto(
  input?: ProtoWorkflowStepWhen | undefined,
): WorkflowStepWhen | undefined {
  if (input === undefined) {
    return undefined;
  }
  const out: WorkflowStepWhen = {
    value: workflowValueFromProto(input.value),
  };
  if (input.equals !== undefined) {
    out.equals = jsonFromValue(input.equals) as JsonInput;
  }
  return out;
}

export function workflowStepToProto(input: WorkflowStep): ProtoWorkflowStep {
  const step = workflowStep(input);
  const action = step.action;
  return create(WorkflowStepSchema, {
    id: step.id ?? "",
    inputs: Object.fromEntries(
      Object.entries(step.inputs ?? {}).map(([key, value]) => [
        key,
        workflowValueToProto(value)!,
      ]),
    ),
    action: action?.case === "app"
      ? { case: "app", value: workflowStepAppCallToProto(action.value)! }
      : action?.case === "agent"
        ? { case: "agent", value: workflowStepAgentTurnToProto(action.value)! }
        : { case: undefined },
    when: workflowStepWhenToProto(step.when),
    timeoutSeconds: step.timeoutSeconds ?? 0,
    metadata: optionalStruct(step.metadata),
  });
}

export function workflowStepFromProto(input: ProtoWorkflowStep): WorkflowStep {
  const action: WorkflowStepActionKind = input.action.case === "app"
    ? { case: "app" as const, value: workflowStepAppCallFromProto(input.action.value)! }
    : input.action.case === "agent"
      ? { case: "agent" as const, value: workflowStepAgentTurnFromProto(input.action.value)! }
      : { case: undefined };
  return {
    id: input.id,
    inputs: Object.fromEntries(
      Object.entries(input.inputs).map(([key, value]) => [
        key,
        workflowValueFromProto(value)!,
      ]),
    ),
    app: workflowStepAppAction(action),
    agent: workflowStepAgentAction(action),
    action,
    when: workflowStepWhenFromProto(input.when),
    timeoutSeconds: input.timeoutSeconds,
    metadata: optionalObjectFromStruct(input.metadata),
  };
}

export function boundWorkflowTargetToProto(
  input?: BoundWorkflowTarget | undefined,
): ProtoBoundWorkflowTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  const target = boundWorkflowTarget(input);
  return create(BoundWorkflowTargetSchema, {
    steps: target.steps?.map(workflowStepToProto) ?? [],
  });
}

export function boundWorkflowTargetFromProto(
  input?: ProtoBoundWorkflowTarget | undefined,
): BoundWorkflowTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    steps: input.steps.map(workflowStepFromProto),
  };
}

export function workflowEventToProto(input?: WorkflowEvent | undefined): ProtoWorkflowEvent | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowEventSchema, {
    id: input.id ?? "",
    source: input.source ?? "",
    specVersion: input.specVersion ?? "",
    type: input.type ?? "",
    subject: input.subject ?? "",
    time: optionalTimestamp(input.time),
    datacontenttype: input.datacontenttype ?? "",
    data: optionalStruct(input.data),
    extensions: Object.fromEntries(
      Object.entries(input.extensions ?? {}).map(([key, value]) => [key, valueFromJson(value)]),
    ),
  });
}

export function workflowEventFromProto(input?: ProtoWorkflowEvent | undefined): WorkflowEvent | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    source: input.source,
    specVersion: input.specVersion,
    type: input.type,
    subject: input.subject,
    time: optionalDate(input.time),
    datacontenttype: input.datacontenttype,
    data: optionalObjectFromStruct(input.data),
    extensions: Object.fromEntries(
      Object.entries(input.extensions).map(([key, value]) => [key, jsonFromValue(value) as JsonInput]),
    ),
  };
}

export function workflowSignalToProto(input?: WorkflowSignal | undefined): ProtoWorkflowSignal | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowSignalSchema, {
    id: input.id ?? "",
    name: input.name ?? "",
    payload: optionalStruct(input.payload),
    metadata: optionalStruct(input.metadata),
    createdBySubjectId: (input.createdBySubjectId ?? "").trim(),
    createdAt: optionalTimestamp(input.createdAt),
    idempotencyKey: input.idempotencyKey ?? "",
    sequence: BigInt(input.sequence ?? 0),
  });
}

export function workflowSignalFromProto(input?: ProtoWorkflowSignal | undefined): WorkflowSignal | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    name: input.name,
    payload: optionalObjectFromStruct(input.payload),
    metadata: optionalObjectFromStruct(input.metadata),
    createdBySubjectId: input.createdBySubjectId,
    createdAt: optionalDate(input.createdAt),
    idempotencyKey: input.idempotencyKey,
    sequence: input.sequence,
  };
}

export function workflowScheduleTriggerToProto(
  input?: WorkflowScheduleTrigger | undefined,
): ProtoWorkflowScheduleTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowScheduleTriggerSchema, {
    scheduleId: input.scheduleId ?? "",
    scheduledFor: optionalTimestamp(input.scheduledFor),
  });
}

export function workflowScheduleTriggerFromProto(
  input?: ProtoWorkflowScheduleTrigger | undefined,
): WorkflowScheduleTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    scheduleId: input.scheduleId,
    scheduledFor: optionalDate(input.scheduledFor),
  };
}

export function workflowEventTriggerInvocationToProto(
  input?: WorkflowEventTriggerInvocation | undefined,
): ProtoWorkflowEventTriggerInvocation | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowEventTriggerInvocationSchema, {
    triggerId: input.triggerId ?? "",
    event: workflowEventToProto(input.event),
  });
}

export function workflowEventTriggerInvocationFromProto(
  input?: ProtoWorkflowEventTriggerInvocation | undefined,
): WorkflowEventTriggerInvocation | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    triggerId: input.triggerId,
    event: workflowEventFromProto(input.event),
  };
}

export function workflowRunTriggerToProto(
  input?: WorkflowRunTrigger | undefined,
): ProtoWorkflowRunTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  const trigger = workflowRunTrigger(input);
  const kind = trigger.kind;
  switch (kind?.case) {
    case "manual":
      return create(WorkflowRunTriggerSchema, {
        kind: { case: "manual", value: create(WorkflowManualTriggerSchema) },
      });
    case "schedule":
      return create(WorkflowRunTriggerSchema, {
        kind: { case: "schedule", value: workflowScheduleTriggerToProto(kind.value)! },
      });
    case "event":
      return create(WorkflowRunTriggerSchema, {
        kind: { case: "event", value: workflowEventTriggerInvocationToProto(kind.value)! },
      });
    default:
      return create(WorkflowRunTriggerSchema);
  }
}

export function workflowRunTriggerFromProto(
  input?: ProtoWorkflowRunTrigger | undefined,
): WorkflowRunTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "manual":
      return { kind: { case: "manual", value: {} } };
    case "schedule":
      return { kind: { case: "schedule", value: workflowScheduleTriggerFromProto(input.kind.value)! } };
    case "event":
      return { kind: { case: "event", value: workflowEventTriggerInvocationFromProto(input.kind.value)! } };
    default:
      return { kind: { case: undefined } };
  }
}

export function boundWorkflowRunToProto(input: BoundWorkflowRun): ProtoBoundWorkflowRun {
  return create(BoundWorkflowRunSchema, {
    id: input.id ?? "",
    status: input.status ?? WorkflowRunStatus.UNSPECIFIED,
    target: boundWorkflowTargetToProto(input.target),
    trigger: workflowRunTriggerToProto(input.trigger),
    createdAt: optionalTimestamp(input.createdAt),
    startedAt: optionalTimestamp(input.startedAt),
    completedAt: optionalTimestamp(input.completedAt),
    statusMessage: input.statusMessage ?? "",
    resultBody: input.resultBody ?? "",
    createdBySubjectId: (input.createdBySubjectId ?? "").trim(),
    workflowKey: input.workflowKey ?? "",
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
  });
}

export function boundWorkflowRunFromProto(input?: ProtoBoundWorkflowRun | undefined): BoundWorkflowRun | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    status: input.status as WorkflowRunStatus,
    target: boundWorkflowTargetFromProto(input.target),
    trigger: workflowRunTriggerFromProto(input.trigger),
    createdAt: optionalDate(input.createdAt),
    startedAt: optionalDate(input.startedAt),
    completedAt: optionalDate(input.completedAt),
    statusMessage: input.statusMessage,
    resultBody: input.resultBody,
    createdBySubjectId: input.createdBySubjectId,
    workflowKey: input.workflowKey,
    providerName: input.providerName,
    definitionId: input.definitionId,
  };
}

export function signalWorkflowRunResponseToProto(
  input: SignalWorkflowRunResponse,
): ProtoSignalWorkflowRunResponse {
  return create(SignalWorkflowRunResponseSchema, {
    run: input.run === undefined ? undefined : boundWorkflowRunToProto(input.run),
    signal: input.signal === undefined ? undefined : workflowSignalToProto(input.signal),
    startedRun: input.startedRun ?? false,
    workflowKey: input.workflowKey ?? "",
  });
}

export function boundWorkflowScheduleToProto(input: BoundWorkflowSchedule): ProtoBoundWorkflowSchedule {
  return create(BoundWorkflowScheduleSchema, {
    id: input.id ?? "",
    cron: input.cron ?? "",
    timezone: input.timezone ?? "",
    target: boundWorkflowTargetToProto(input.target),
    paused: input.paused ?? false,
    createdAt: optionalTimestamp(input.createdAt),
    updatedAt: optionalTimestamp(input.updatedAt),
    nextRunAt: optionalTimestamp(input.nextRunAt),
    createdBySubjectId: (input.createdBySubjectId ?? "").trim(),
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
  });
}

export function boundWorkflowScheduleFromProto(
  input?: ProtoBoundWorkflowSchedule | undefined,
): BoundWorkflowSchedule | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    cron: input.cron,
    timezone: input.timezone,
    target: boundWorkflowTargetFromProto(input.target),
    paused: input.paused,
    createdAt: optionalDate(input.createdAt),
    updatedAt: optionalDate(input.updatedAt),
    nextRunAt: optionalDate(input.nextRunAt),
    createdBySubjectId: input.createdBySubjectId,
    providerName: input.providerName,
    definitionId: input.definitionId,
  };
}

export function boundWorkflowEventTriggerToProto(
  input: BoundWorkflowEventTrigger,
): ProtoBoundWorkflowEventTrigger {
  return create(BoundWorkflowEventTriggerSchema, {
    id: input.id ?? "",
    match: workflowEventMatchToProto(input.match),
    target: boundWorkflowTargetToProto(input.target),
    paused: input.paused ?? false,
    createdAt: optionalTimestamp(input.createdAt),
    updatedAt: optionalTimestamp(input.updatedAt),
    createdBySubjectId: (input.createdBySubjectId ?? "").trim(),
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
  });
}

export function boundWorkflowEventTriggerFromProto(
  input?: ProtoBoundWorkflowEventTrigger | undefined,
): BoundWorkflowEventTrigger | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    match: workflowEventMatchFromProto(input.match),
    target: boundWorkflowTargetFromProto(input.target),
    paused: input.paused,
    createdAt: optionalDate(input.createdAt),
    updatedAt: optionalDate(input.updatedAt),
    createdBySubjectId: input.createdBySubjectId,
    providerName: input.providerName,
    definitionId: input.definitionId,
  };
}

export function boundWorkflowDefinitionToProto(input: BoundWorkflowDefinition) {
  return create(BoundWorkflowDefinitionSchema, {
    id: input.id ?? "",
    target: boundWorkflowTargetToProto(input.target),
    createdBySubjectId: (input.createdBySubjectId ?? "").trim(),
    createdAt: optionalTimestamp(input.createdAt),
    providerName: input.providerName ?? "",
  });
}

export function boundWorkflowDefinitionFromProto(
  input?: ProtoBoundWorkflowDefinition | undefined,
): BoundWorkflowDefinition | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    target: boundWorkflowTargetFromProto(input.target),
    createdBySubjectId: input.createdBySubjectId,
    createdAt: optionalDate(input.createdAt),
    providerName: input.providerName,
  };
}

function startWorkflowProviderRunRequestFromProto(
  input: ProtoStartWorkflowProviderRunRequest,
): StartWorkflowProviderRunRequest {
	return {
		target: boundWorkflowTargetFromProto(input.target),
		idempotencyKey: input.idempotencyKey,
		createdBySubjectId: input.createdBySubjectId,
		workflowKey: input.workflowKey,
		definitionId: input.definitionId,
	};
}

function getWorkflowProviderRunRequestFromProto(
  input: ProtoGetWorkflowProviderRunRequest,
): GetWorkflowProviderRunRequest {
  return { runId: input.runId };
}

function listWorkflowProviderRunsRequestFromProto(
  input: ProtoListWorkflowProviderRunsRequest,
): ListWorkflowProviderRunsRequest {
  return {
    pageSize: input.pageSize,
    pageToken: input.pageToken,
    status: input.status as WorkflowRunStatus,
    targetApp: input.targetApp,
  };
}

function cancelWorkflowProviderRunRequestFromProto(
  input: ProtoCancelWorkflowProviderRunRequest,
): CancelWorkflowProviderRunRequest {
  return { runId: input.runId, reason: input.reason };
}

function signalWorkflowProviderRunRequestFromProto(
  input: ProtoSignalWorkflowProviderRunRequest,
): SignalWorkflowProviderRunRequest {
  return {
    runId: input.runId,
    signal: workflowSignalFromProto(input.signal),
  };
}

function signalOrStartWorkflowProviderRunRequestFromProto(
  input: ProtoSignalOrStartWorkflowProviderRunRequest,
): SignalOrStartWorkflowProviderRunRequest {
  return {
    workflowKey: input.workflowKey,
    target: boundWorkflowTargetFromProto(input.target),
    idempotencyKey: input.idempotencyKey,
    createdBySubjectId: input.createdBySubjectId,
    signal: workflowSignalFromProto(input.signal),
    definitionId: input.definitionId,
  };
}

function createWorkflowProviderDefinitionRequestFromProto(
  input: ProtoCreateWorkflowProviderDefinitionRequest,
): CreateWorkflowProviderDefinitionRequest {
	return {
		target: boundWorkflowTargetFromProto(input.target),
		idempotencyKey: input.idempotencyKey,
		createdBySubjectId: input.createdBySubjectId,
	};
}

function getWorkflowProviderDefinitionRequestFromProto(
  input: ProtoGetWorkflowProviderDefinitionRequest,
): GetWorkflowProviderDefinitionRequest {
  return { definitionId: input.definitionId };
}

function updateWorkflowProviderDefinitionRequestFromProto(
  input: ProtoUpdateWorkflowProviderDefinitionRequest,
): UpdateWorkflowProviderDefinitionRequest {
  return {
    definitionId: input.definitionId,
    target: boundWorkflowTargetFromProto(input.target),
    requestedBySubjectId: input.requestedBySubjectId,
  };
}

function deleteWorkflowProviderDefinitionRequestFromProto(
  input: ProtoDeleteWorkflowProviderDefinitionRequest,
): DeleteWorkflowProviderDefinitionRequest {
  return { definitionId: input.definitionId };
}

function upsertWorkflowProviderScheduleRequestFromProto(
  input: ProtoUpsertWorkflowProviderScheduleRequest,
): UpsertWorkflowProviderScheduleRequest {
  return {
    scheduleId: input.scheduleId,
    cron: input.cron,
    timezone: input.timezone,
    target: boundWorkflowTargetFromProto(input.target),
    paused: input.paused,
    requestedBySubjectId: input.requestedBySubjectId,
    idempotencyKey: input.idempotencyKey,
    definitionId: input.definitionId,
  };
}

function getWorkflowProviderScheduleRequestFromProto(
  input: ProtoGetWorkflowProviderScheduleRequest,
): GetWorkflowProviderScheduleRequest {
  return { scheduleId: input.scheduleId };
}

function listWorkflowProviderSchedulesRequestFromProto(
  _input: ProtoListWorkflowProviderSchedulesRequest,
): ListWorkflowProviderSchedulesRequest {
  return {};
}

function deleteWorkflowProviderScheduleRequestFromProto(
  input: ProtoDeleteWorkflowProviderScheduleRequest,
): DeleteWorkflowProviderScheduleRequest {
  return { scheduleId: input.scheduleId };
}

function pauseWorkflowProviderScheduleRequestFromProto(
  input: ProtoPauseWorkflowProviderScheduleRequest,
): PauseWorkflowProviderScheduleRequest {
  return { scheduleId: input.scheduleId };
}

function resumeWorkflowProviderScheduleRequestFromProto(
  input: ProtoResumeWorkflowProviderScheduleRequest,
): ResumeWorkflowProviderScheduleRequest {
  return { scheduleId: input.scheduleId };
}

function upsertWorkflowProviderEventTriggerRequestFromProto(
  input: ProtoUpsertWorkflowProviderEventTriggerRequest,
): UpsertWorkflowProviderEventTriggerRequest {
  return {
    triggerId: input.triggerId,
    match: workflowEventMatchFromProto(input.match),
    target: boundWorkflowTargetFromProto(input.target),
    paused: input.paused,
    requestedBySubjectId: input.requestedBySubjectId,
    idempotencyKey: input.idempotencyKey,
    definitionId: input.definitionId,
  };
}

function getWorkflowProviderEventTriggerRequestFromProto(
  input: ProtoGetWorkflowProviderEventTriggerRequest,
): GetWorkflowProviderEventTriggerRequest {
  return { triggerId: input.triggerId };
}

function listWorkflowProviderEventTriggersRequestFromProto(
  _input: ProtoListWorkflowProviderEventTriggersRequest,
): ListWorkflowProviderEventTriggersRequest {
  return {};
}

function deleteWorkflowProviderEventTriggerRequestFromProto(
  input: ProtoDeleteWorkflowProviderEventTriggerRequest,
): DeleteWorkflowProviderEventTriggerRequest {
  return { triggerId: input.triggerId };
}

function pauseWorkflowProviderEventTriggerRequestFromProto(
  input: ProtoPauseWorkflowProviderEventTriggerRequest,
): PauseWorkflowProviderEventTriggerRequest {
  return { triggerId: input.triggerId };
}

function resumeWorkflowProviderEventTriggerRequestFromProto(
  input: ProtoResumeWorkflowProviderEventTriggerRequest,
): ResumeWorkflowProviderEventTriggerRequest {
  return { triggerId: input.triggerId };
}

function publishWorkflowProviderEventRequestFromProto(
  input: ProtoPublishWorkflowProviderEventRequest,
): PublishWorkflowProviderEventRequest {
  return {
    appName: input.appName,
    event: workflowEventFromProto(input.event),
    publishedBySubjectId: input.publishedBySubjectId,
  };
}

export function workflowScheduleFromProto(
  input: ProtoBoundWorkflowSchedule,
): WorkflowSchedule {
  return {
    providerName: input.providerName,
    schedule: boundWorkflowScheduleFromProto(input),
  };
}

export function workflowEventTriggerFromProto(
  input: ProtoBoundWorkflowEventTrigger,
): WorkflowEventTrigger {
  return {
    providerName: input.providerName,
    trigger: boundWorkflowEventTriggerFromProto(input),
  };
}

export function workflowDefinitionFromProto(
  input: ProtoBoundWorkflowDefinition,
): WorkflowDefinition {
  return {
    providerName: input.providerName,
    definition: boundWorkflowDefinitionFromProto(input),
  };
}

export function workflowRunFromProto(
  input: ProtoBoundWorkflowRun,
): WorkflowRun {
  return {
    providerName: input.providerName,
    run: boundWorkflowRunFromProto(input),
  };
}

export function workflowRunSignalFromProto(
  input: ProtoSignalWorkflowRunResponse,
): WorkflowRunSignal {
  return {
    providerName: input.run?.providerName,
    run: boundWorkflowRunFromProto(input.run),
    signal: workflowSignalFromProto(input.signal),
    startedRun: input.startedRun,
    workflowKey: input.workflowKey,
  };
}

function cloneWorkflowValueKind(kind: WorkflowValueKind): WorkflowValueKind {
  switch (kind.case) {
    case "literal":
      return { case: "literal", value: jsonClone(kind.value) };
    case "object":
      return {
        case: "object",
        value: Object.fromEntries(
          Object.entries(kind.value).map(([key, value]) => [key, workflowValue(value)]),
        ),
      };
    case "array":
      return { case: "array", value: kind.value.map(workflowValue) };
    case "template":
      return { case: "template", value: workflowText(kind.value) };
    case "runInput":
    case "signalPayload":
      return { case: kind.case, value: kind.value };
    case "stepOutput":
      return { case: "stepOutput", value: workflowStepOutputSource(kind.value) };
    default:
      return { case: undefined };
  }
}

function valueMapInput(input?: Record<string, JsonInput>): Record<string, JsonInput> {
  return input === undefined ? {} : { ...input };
}

function workflowAgentOutputInputFromOutput(
  input: AgentOutput,
): AgentOutput {
  if (input.text !== undefined) {
    return { text: {} };
  }
  if (input.structured !== undefined) {
    return {
      structured: {
        schema: jsonObjectClone(input.structured.schema),
      },
    };
  }
  throw new Error("workflow agent output is required");
}

function jsonObjectClone(input: JsonObjectInput): JsonObjectInput {
  return structFromObject(jsonObjectFromStruct(input as JsonObject));
}

function jsonClone(input: JsonInput): JsonInput {
  return jsonFromValue(valueFromJson(input)) as JsonInput;
}

function optionalTimestamp(value?: Date | undefined) {
  return value === undefined ? undefined : timestampFromDate(value);
}

function optionalDate(timestamp?: Parameters<typeof dateFromTimestamp>[0]) {
  return timestamp === undefined ? undefined : dateFromTimestamp(timestamp);
}

function listRunsResult(
  value: readonly BoundWorkflowRun[] | ListWorkflowProviderRunsResponse,
): ListWorkflowProviderRunsResponse {
  return "runs" in value ? value : { runs: value };
}

async function invokeWorkflowProvider<T>(
  action: string,
  fn: () => Promise<T>,
): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    if (error instanceof ConnectError) {
      throw error;
    }
    throw new ConnectError(
      `workflow provider ${action}: ${errorMessage(error)}`,
      Code.Unknown,
    );
  }
}

export interface WorkflowExecutionRequest {
  providerName?: string | undefined;
  runId?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  trigger?: WorkflowRunTrigger | undefined;
  input?: Record<string, JsonInput> | undefined;
  metadata?: Record<string, JsonInput> | undefined;
  createdBySubjectId?: string | undefined;
  invocationToken?: string | undefined;
  signals?: readonly WorkflowSignal[] | undefined;
}

export interface WorkflowRunContextTrigger {
  kind: string;
  scheduleId: string;
  scheduledFor: string;
  triggerId: string;
  event?: Record<string, JsonInput> | undefined;
}

export interface WorkflowRunContextSignal {
  id: string;
  name: string;
  payload: Record<string, JsonInput>;
  metadata: Record<string, JsonInput>;
  createdBySubjectId?: string | undefined;
  createdAt: string;
  idempotencyKey: string;
  sequence?: number | undefined;
}

export interface WorkflowRunContext {
  provider: string;
  runId: string;
  target?: Record<string, JsonInput> | undefined;
  trigger: WorkflowRunContextTrigger;
  input: Record<string, JsonInput>;
  metadata: Record<string, JsonInput>;
  signals: readonly WorkflowRunContextSignal[];
  createdBySubjectId?: string | undefined;
  latestSignal?: WorkflowRunContextSignal | undefined;
}

export interface WorkflowEvalContext {
  request: WorkflowExecutionRequest;
  outputs?: Record<string, unknown> | undefined;
  inputs?: Record<string, unknown> | undefined;
  allowInputs?: boolean | undefined;
}

export class WorkflowValueError extends Error {}

export function evaluateWorkflowStepInputs(
  ctx: WorkflowEvalContext,
  values?: Record<string, WorkflowValue> | undefined,
): Record<string, unknown> | undefined {
  if (values === undefined || Object.keys(values).length === 0) {
    return undefined;
  }
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(values)) {
    const resolved = evaluateWorkflowValue(ctx, value);
    if (!resolved.ok) {
      throw new WorkflowValueError(`inputs.${key} did not resolve`);
    }
    out[key] = resolved.value;
  }
  return out;
}

export function evaluateWorkflowValue(
  ctx: WorkflowEvalContext,
  input: WorkflowValue,
): { value: unknown; ok: boolean } {
  const value = workflowValue(input);
  const kind = value.kind;
  switch (kind?.case) {
    case "literal":
      return { value: kind.value, ok: true };
    case "object": {
      const out: Record<string, unknown> = {};
      for (const [key, child] of Object.entries(kind.value)) {
        const resolved = evaluateWorkflowValue(ctx, child);
        if (!resolved.ok) {
          return { value: undefined, ok: false };
        }
        out[key] = resolved.value;
      }
      return { value: out, ok: true };
    }
    case "array": {
      const out: unknown[] = [];
      for (const child of kind.value) {
        const resolved = evaluateWorkflowValue(ctx, child);
        if (!resolved.ok) {
          return { value: undefined, ok: false };
        }
        out.push(resolved.value);
      }
      return { value: out, ok: true };
    }
    case "template":
      return {
        value: renderWorkflowTemplate(
          ctx,
          typeof kind.value === "string" ? kind.value : (kind.value.template ?? ""),
        ),
        ok: true,
      };
    case "runInput":
      return mapPathValue(ctx.request.input, kind.value);
    case "signalPayload": {
      const signal = latestWorkflowSignal(ctx.request.signals);
      return signal === undefined
        ? { value: undefined, ok: false }
        : pathValue(signal.payload, kind.value);
    }
    case "stepOutput": {
      const stepId = kind.value.stepId?.trim() ?? "";
      const outputs = ctx.outputs ?? {};
      if (!Object.prototype.hasOwnProperty.call(outputs, stepId)) {
        throw new WorkflowValueError(
          `workflow step output references missing step "${stepId}"`,
        );
      }
      return pathValue(outputs[stepId], kind.value.path ?? "");
    }
    default:
      return { value: undefined, ok: true };
  }
}

export function renderWorkflowTemplate(
  ctx: WorkflowEvalContext,
  template: string,
): string {
  let out = "";
  for (let i = 0; i < template.length; ) {
    if (template.startsWith("$${", i)) {
      out += "${";
      i += 3;
      continue;
    }
    if (!template.startsWith("${", i)) {
      out += template[i];
      i += 1;
      continue;
    }
    const end = template.indexOf("}", i + 2);
    if (end < 0) {
      throw new WorkflowValueError("unterminated template expression");
    }
    const expr = template.slice(i + 2, end).trim();
    const resolved = templateExpressionValue(ctx, expr);
    if (!resolved.ok) {
      throw new WorkflowValueError(`template expression "${expr}" did not resolve`);
    }
    out += renderTemplateValue(resolved.value);
    i = end + 1;
  }
  return out;
}

export function workflowRunContext(
  req: WorkflowExecutionRequest,
): Record<string, JsonInput> {
  const out: Record<string, JsonInput> = {};
  if (req.runId?.trim()) {
    out.runId = req.runId.trim();
  }
  if (req.providerName?.trim()) {
    out.provider = req.providerName.trim();
  }
  const target = workflowTargetContext(req.target);
  if (target !== undefined) {
    out.target = target;
  }
  const trigger = workflowTriggerContext(req.trigger);
  if (trigger !== undefined) {
    out.trigger = trigger;
  }
  if (req.input !== undefined) {
    out.input = { ...req.input };
  }
  if (req.metadata !== undefined) {
    out.metadata = { ...req.metadata };
  }
  const signals = workflowSignalsContext(req.signals);
  if (signals.length > 0) {
    out.signals = signals;
  }
  const createdBySubjectId = req.createdBySubjectId?.trim();
  if (createdBySubjectId) {
    out.createdBySubjectId = createdBySubjectId;
  }
  return out;
}

export function parseWorkflowRunContext(
  value?: Request | Record<string, unknown> | null | undefined,
): WorkflowRunContext {
  const data = workflowRunContextData(value);
  const target = workflowContextOptionalObject(data.target);
  const signals = Array.isArray(data.signals)
    ? data.signals
      .map(workflowRunContextSignal)
      .filter((signal): signal is WorkflowRunContextSignal => signal !== undefined)
    : [];
  const createdBySubjectId = workflowContextString(data.createdBySubjectId);
  const latestSignal = signals.at(-1);
  const context: WorkflowRunContext = {
    provider: workflowContextString(data.provider),
    runId: workflowContextString(data.runId),
    trigger: workflowRunContextTrigger(data.trigger),
    input: workflowContextObject(data.input),
    metadata: workflowContextObject(data.metadata),
    signals,
  };
  if (target !== undefined) {
    context.target = target;
  }
  if (createdBySubjectId) {
    context.createdBySubjectId = createdBySubjectId;
  }
  if (latestSignal !== undefined) {
    context.latestSignal = latestSignal;
  }
  return context;
}

function workflowRunContextData(
  value?: Request | Record<string, unknown> | null | undefined,
): Record<string, unknown> {
  if (!isWorkflowContextRecord(value)) {
    return {};
  }
  return isWorkflowContextRecord(value.workflow) ? value.workflow : value;
}

function workflowRunContextTrigger(value: unknown): WorkflowRunContextTrigger {
  const data = isWorkflowContextRecord(value) ? value : {};
  const event = workflowContextOptionalObject(data.event);
  const trigger: WorkflowRunContextTrigger = {
    kind: workflowContextString(data.kind),
    scheduleId: workflowContextString(data.scheduleId),
    scheduledFor: workflowContextString(data.scheduledFor),
    triggerId: workflowContextString(data.triggerId),
  };
  if (event !== undefined) {
    trigger.event = event;
  }
  return trigger;
}

function workflowRunContextSignal(value: unknown): WorkflowRunContextSignal | undefined {
  if (!isWorkflowContextRecord(value)) {
    return undefined;
  }
  const signal: WorkflowRunContextSignal = {
    id: workflowContextString(value.id),
    name: workflowContextString(value.name),
    payload: workflowContextObject(value.payload),
    metadata: workflowContextObject(value.metadata),
    createdAt: workflowContextString(value.createdAt),
    idempotencyKey: workflowContextString(value.idempotencyKey),
  };
  const createdBySubjectId = typeof value.createdBySubjectId === "string" ? value.createdBySubjectId.trim() : "";
  if (createdBySubjectId) {
    signal.createdBySubjectId = createdBySubjectId;
  }
  const sequence = workflowContextNumber(value.sequence);
  if (sequence !== undefined) {
    signal.sequence = sequence;
  }
  return signal;
}

function workflowContextString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function workflowContextNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function workflowContextObject(value: unknown): Record<string, JsonInput> {
  return isWorkflowContextRecord(value) ? { ...(value as Record<string, JsonInput>) } : {};
}

function workflowContextOptionalObject(value: unknown): Record<string, JsonInput> | undefined {
  return isWorkflowContextRecord(value) ? { ...(value as Record<string, JsonInput>) } : undefined;
}

function isWorkflowContextRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function workflowTargetContext(
  target?: BoundWorkflowTarget,
): Record<string, JsonInput> | undefined {
  if (target?.steps === undefined || target.steps.length === 0) {
    return undefined;
  }
  return {
    kind: "steps",
    steps: target.steps.map((step) => {
      const item: Record<string, JsonInput> = { id: step.id?.trim() ?? "" };
      const app = step.app ?? workflowStepAppAction(step.action);
      const agent = step.agent ?? workflowStepAgentAction(step.action);
      if (app !== undefined) {
        item.kind = "app";
        item.app = app.name?.trim() ?? "";
        item.operation = app.operation?.trim() ?? "";
        if (app.connection?.trim()) item.connection = app.connection.trim();
        if (app.instance?.trim()) item.instance = app.instance.trim();
        if (app.credentialMode?.trim()) item.credentialMode = app.credentialMode.trim();
        return item;
      }
      if (agent !== undefined) {
        item.kind = "agent";
        item.agentProvider = agent.provider?.trim() ?? "";
        item.model = agent.model?.trim() ?? "";
        return item;
      }
      item.kind = "unknown";
      return item;
    }),
  };
}

function workflowTriggerContext(
  trigger?: WorkflowRunTrigger,
): Record<string, JsonInput> | undefined {
  if (trigger === undefined) {
    return undefined;
  }
  const normalized = workflowRunTrigger(trigger);
  switch (normalized.kind?.case) {
    case "schedule": {
      const value: Record<string, JsonInput> = {
        kind: "schedule",
        scheduleId: normalized.kind.value.scheduleId ?? "",
      };
      if (normalized.kind.value.scheduledFor !== undefined) {
        value.scheduledFor = normalized.kind.value.scheduledFor.toISOString();
      }
      return value;
    }
    case "event": {
      const value: Record<string, JsonInput> = {
        kind: "event",
        triggerId: normalized.kind.value.triggerId ?? "",
      };
      const event = workflowEventContext(normalized.kind.value.event);
      if (event !== undefined) {
        value.event = event;
      }
      return value;
    }
    case "manual":
      return { kind: "manual" };
    default:
      return undefined;
  }
}

function workflowEventContext(event?: WorkflowEvent): Record<string, JsonInput> | undefined {
  if (event === undefined) {
    return undefined;
  }
  const out: Record<string, JsonInput> = {};
  if (event.id?.trim()) out.id = event.id.trim();
  if (event.source?.trim()) out.source = event.source.trim();
  if (event.specVersion?.trim()) out.specVersion = event.specVersion.trim();
  if (event.type?.trim()) out.type = event.type.trim();
  if (event.subject?.trim()) out.subject = event.subject.trim();
  if (event.time !== undefined) out.time = event.time.toISOString();
  if (event.datacontenttype?.trim()) out.dataContentType = event.datacontenttype.trim();
  if (event.data !== undefined) out.data = jsonClone(event.data);
  if (event.extensions !== undefined) out.extensions = { ...event.extensions };
  return Object.keys(out).length === 0 ? undefined : out;
}


export function workflowSignalsContext(
  signals?: readonly WorkflowSignal[] | undefined,
): Array<Record<string, JsonInput>> {
  return (signals ?? []).slice(0, 10).map((signal) => {
    const out: Record<string, JsonInput> = {};
    if (signal.id?.trim()) out.id = signal.id.trim();
    if (signal.name?.trim()) out.name = signal.name.trim();
    if (signal.payload !== undefined) {
      const payload = compactWorkflowSignalPayload(signal.payload);
      if (Object.keys(payload).length > 0) out.payload = payload;
    }
    if (signal.metadata !== undefined) out.metadata = compactJsonValue(signal.metadata, 4);
    const createdBySubjectId = signal.createdBySubjectId?.trim();
    if (createdBySubjectId) out.createdBySubjectId = createdBySubjectId;
    if (signal.createdAt !== undefined) out.createdAt = signal.createdAt.toISOString();
    if (signal.idempotencyKey?.trim()) {
      out.idempotencyKey = signal.idempotencyKey.trim();
    }
    if (signal.sequence !== undefined && signal.sequence !== 0) {
      out.sequence = Number(signal.sequence);
    }
    return out;
  });
}

export function latestWorkflowSignal(
  signals?: readonly WorkflowSignal[] | undefined,
): WorkflowSignal | undefined {
  return signals === undefined || signals.length === 0
    ? undefined
    : signals[signals.length - 1];
}

export function mapPathValue(
  values: Record<string, unknown> | undefined,
  path: string,
): { value: unknown; ok: boolean } {
  return values === undefined || Object.keys(values).length === 0
    ? { value: undefined, ok: false }
    : pathValue(values, path);
}

export function pathValue(
  root: unknown,
  path: string,
): { value: unknown; ok: boolean } {
  const trimmed = path.trim();
  if (!trimmed) {
    return { value: root, ok: true };
  }
  let current = root;
  for (const segment of pathSegments(trimmed)) {
    if (
      typeof segment === "string" &&
      current !== null &&
      typeof current === "object" &&
      !Array.isArray(current)
    ) {
      if (!Object.prototype.hasOwnProperty.call(current, segment)) {
        return { value: undefined, ok: false };
      }
      current = (current as Record<string, unknown>)[segment];
      continue;
    }
    if (typeof segment === "number" && Array.isArray(current)) {
      if (segment < 0 || segment >= current.length) {
        return { value: undefined, ok: false };
      }
      current = current[segment];
      continue;
    }
    return { value: undefined, ok: false };
  }
  return { value: current, ok: true };
}

function templateExpressionValue(
  ctx: WorkflowEvalContext,
  expr: string,
): { value: unknown; ok: boolean } {
  if (expr.startsWith("inputs.")) {
    if (!ctx.allowInputs) {
      throw new WorkflowValueError("inputs references are not allowed here");
    }
    return mapPathValue(ctx.inputs, expr.slice("inputs.".length));
  }
  if (expr.startsWith("runInput.")) {
    return mapPathValue(ctx.request.input, expr.slice("runInput.".length));
  }
  if (expr.startsWith("signalPayload.")) {
    const signal = latestWorkflowSignal(ctx.request.signals);
    return signal === undefined
      ? { value: undefined, ok: false }
      : pathValue(signal.payload, expr.slice("signalPayload.".length));
  }
  throw new WorkflowValueError(`unsupported template expression "${expr}"`);
}

function renderTemplateValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}

function pathSegments(path: string): Array<string | number> {
  const out: Array<string | number> = [];
  for (let i = 0; i < path.length; ) {
    if (path[i] === ".") {
      i += 1;
      continue;
    }
    if (path[i] === "[") {
      const end = path.indexOf("]", i);
      if (end < 0) throw new WorkflowValueError(`invalid workflow path "${path}"`);
      const token = path.slice(i + 1, end).trim();
      out.push(parseBracketPathToken(token, path));
      i = end + 1;
      continue;
    }
    const start = i;
    while (i < path.length && path[i] !== "." && path[i] !== "[") i += 1;
    const key = path.slice(start, i).trim();
    if (!key) throw new WorkflowValueError(`invalid workflow path "${path}"`);
    out.push(key);
  }
  return out;
}

function parseBracketPathToken(token: string, path: string): string | number {
  if (token.startsWith("'") || token.startsWith('"')) {
    return unquotePathKey(token, path);
  }
  const index = Number.parseInt(token, 10);
  if (!/^[+-]?\d+$/.test(token) || !Number.isInteger(index)) {
    throw new WorkflowValueError(`invalid workflow path "${path}"`);
  }
  return index;
}

function unquotePathKey(token: string, path: string): string {
  if (token.startsWith('"')) {
    try {
      return JSON.parse(token);
    } catch {
      throw new WorkflowValueError(`invalid workflow path "${path}"`);
    }
  }
  if (token.length < 2 || !token.endsWith("'")) {
    throw new WorkflowValueError(`invalid workflow path "${path}"`);
  }
  let out = "";
  for (let i = 1; i < token.length - 1;) {
    const ch = token[i];
    if (ch !== "\\") {
      out += ch;
      i += 1;
      continue;
    }
    i += 1;
    if (i >= token.length - 1) {
      throw new WorkflowValueError(`invalid workflow path "${path}"`);
    }
    const escaped = token[i];
    switch (escaped) {
      case "'":
      case '"':
      case "\\":
        out += escaped;
        i += 1;
        break;
      case "n":
        out += "\n";
        i += 1;
        break;
      case "r":
        out += "\r";
        i += 1;
        break;
      case "t":
        out += "\t";
        i += 1;
        break;
      case "u": {
        const hex = token.slice(i + 1, i + 5);
        if (!/^[0-9a-fA-F]{4}$/.test(hex)) {
          throw new WorkflowValueError(`invalid workflow path "${path}"`);
        }
        out += String.fromCharCode(Number.parseInt(hex, 16));
        i += 5;
        break;
      }
      default:
        out += escaped;
        i += 1;
        break;
    }
  }
  return out;
}

function compactWorkflowSignalPayload(payload: JsonInput): Record<string, JsonInput> {
  const source = workflowMapValue(payload);
  if (Object.keys(source).length === 0) return {};
  const out: Record<string, JsonInput> = {};
  for (const key of [
    "delivery_id", "deliveryId", "github_event", "githubEvent", "github_action", "githubAction",
    "event", "action", "summary", "user_prompt", "userPrompt", "payload_sha256", "payloadSha256",
    "payload_omitted", "payloadOmitted",
  ]) {
    copyCompactPayloadField(out, source, key);
  }
  for (const key of [
    "agent_request", "agentRequest", "installation", "repository", "sender", "webhook_policy",
    "webhookPolicy", "pull_request", "pullRequest", "issue", "comment", "review", "ref",
    "check_run", "checkRun", "check_suite", "checkSuite", "workflow_run", "workflowRun",
    "review_check_run", "reviewCheckRun",
  ]) {
    if (Object.prototype.hasOwnProperty.call(source, key)) {
      const value = source[key];
      if (value !== undefined) out[key] = compactJsonValue(value, 4);
    }
  }
  const fields: Record<string, JsonInput> = {};
  for (const key of Object.keys(source).sort()) {
    if (Object.keys(fields).length >= 20) break;
    if (Object.prototype.hasOwnProperty.call(out, key) || workflowSignalPayloadKeyExcluded(key)) {
      continue;
    }
    const value = source[key];
    if (value === undefined) continue;
    const compact = compactJsonScalar(value);
    if (compact.ok) fields[key] = compact.value;
  }
  if (Object.keys(fields).length > 0) out.fields = fields;
  out.payloadOmitted = true;
  return out;
}

function workflowMapValue(value: JsonInput): Record<string, JsonInput> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? { ...(value as Record<string, JsonInput>) }
    : { value };
}

function copyCompactPayloadField(
  out: Record<string, JsonInput>,
  payload: Record<string, JsonInput>,
  key: string,
): void {
  if (!Object.prototype.hasOwnProperty.call(payload, key) || workflowSignalPayloadKeyExcluded(key)) {
    return;
  }
  const value = payload[key];
  if (value === undefined) return;
  const compact = compactJsonScalar(value);
  out[key] = compact.ok ? compact.value : compactJsonValue(value, 4);
}

function workflowSignalPayloadKeyExcluded(key: string): boolean {
  return key.trim() === "" || key === "payload" || key === "_gestalt_payload_preview_json";
}

function compactJsonScalar(value: JsonInput): { value: JsonInput; ok: boolean } {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "boolean" ||
    typeof value === "number"
  ) {
    return {
      value: typeof value === "string" ? truncateWorkflowString(value, 4096) : value,
      ok: true,
    };
  }
  return { value: null, ok: false };
}

function compactJsonValue(value: JsonInput, depth: number): JsonInput {
  const scalar = compactJsonScalar(value);
  if (scalar.ok) return scalar.value;
  if (depth <= 0) return { omitted: true };
  if (Array.isArray(value)) return value.slice(0, 20).map((item) => compactJsonValue(item, depth - 1));
  const objectValue = value as Record<string, JsonInput>;
  const keys = Object.keys(objectValue)
    .filter((key) => !workflowSignalPayloadKeyExcluded(key))
    .sort();
  const out: Record<string, JsonInput> = {};
  for (const key of keys.slice(0, 20)) {
    const item = objectValue[key];
    if (item !== undefined) out[key] = compactJsonValue(item, depth - 1);
  }
  if (keys.length > Object.keys(out).length) {
    out.omittedFields = keys.length - Object.keys(out).length;
  }
  return out;
}

function truncateWorkflowString(value: string, maxBytes: number): string {
  const encoded = new TextEncoder().encode(value);
  if (encoded.length <= maxBytes) return value;
  const suffix = "...";
  let bytes = encoded.slice(0, Math.max(0, maxBytes - suffix.length));
  let text = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  while (new TextEncoder().encode(text + suffix).length > maxBytes && text.length > 0) {
    text = text.slice(0, -1);
    bytes = new TextEncoder().encode(text);
  }
  return new TextDecoder().decode(bytes) + suffix;
}
