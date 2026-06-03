import { create, type JsonObject } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  type ServiceImpl,
} from "@connectrpc/connect";

import type { AgentOutput, AgentToolRef } from "./agent.ts";
import {
  agentOutputFromProto,
  agentOutputToProto,
  agentToolRefFromProto,
  agentToolRefToProto,
} from "./agent-conversions.ts";
import {
  type MaybePromise,
  type Request,
  type Subject,
  type SubjectInput,
  errorMessage,
} from "./api.ts";
import { SubjectContextSchema, type SubjectContext } from "./internal/gen/v1/app_pb.ts";
import {
  ApplyWorkflowProviderDefinitionRequestSchema,
  BoundWorkflowTargetSchema,
  CancelWorkflowProviderRunRequestSchema,
  DeliverWorkflowProviderEventRequestSchema,
  GetWorkflowProviderRunEventsResponseSchema,
  GetWorkflowProviderRunOutputResponseSchema,
  ListWorkflowProviderDefinitionsResponseSchema,
  ListWorkflowProviderRunsResponseSchema,
  SignalWorkflowRunResponseSchema,
  WorkflowActivationSchema,
  WorkflowAgentMessageSchema,
  WorkflowArraySchema,
  WorkflowDefinitionSchema,
  WorkflowDefinitionSpecSchema,
  WorkflowEventActivationSchema,
  WorkflowEventMatchSchema,
  WorkflowEventSchema,
  WorkflowEventTriggerInvocationSchema,
  WorkflowManualTriggerSchema,
  WorkflowObjectSchema,
  WorkflowPathSourceSchema,
  WorkflowProvider as WorkflowProviderService,
  WorkflowRunEventSchema,
  WorkflowRunSchema,
  WorkflowRunStatus as ProtoWorkflowRunStatus,
  WorkflowRunTriggerSchema,
  WorkflowScheduleActivationSchema,
  WorkflowScheduleTriggerSchema,
  WorkflowSignalSchema,
  WorkflowStepAgentTurnSchema,
  WorkflowStepAppCallSchema,
  WorkflowStepAttemptSchema,
  WorkflowStepExecutionSchema,
  WorkflowStepInputSourceSchema,
  WorkflowStepOutputSourceSchema,
  WorkflowStepSchema,
  WorkflowStepStatus as ProtoWorkflowStepStatus,
  WorkflowStepWhenSchema,
  WorkflowTextSchema,
  WorkflowValueSchema,
  type ApplyWorkflowProviderDefinitionRequest as ProtoApplyWorkflowProviderDefinitionRequest,
  type BoundWorkflowTarget as ProtoBoundWorkflowTarget,
  type CancelWorkflowProviderRunRequest as ProtoCancelWorkflowProviderRunRequest,
  type DeliverWorkflowProviderEventRequest as ProtoDeliverWorkflowProviderEventRequest,
  type GetWorkflowProviderDefinitionRequest as ProtoGetWorkflowProviderDefinitionRequest,
  type GetWorkflowProviderRunEventsRequest as ProtoGetWorkflowProviderRunEventsRequest,
  type GetWorkflowProviderRunOutputRequest as ProtoGetWorkflowProviderRunOutputRequest,
  type GetWorkflowProviderRunOutputResponse as ProtoGetWorkflowProviderRunOutputResponse,
  type GetWorkflowProviderRunRequest as ProtoGetWorkflowProviderRunRequest,
  type ListWorkflowProviderDefinitionsRequest as ProtoListWorkflowProviderDefinitionsRequest,
  type ListWorkflowProviderRunsRequest as ProtoListWorkflowProviderRunsRequest,
  type SetWorkflowProviderActivationPausedRequest as ProtoSetWorkflowProviderActivationPausedRequest,
  type SetWorkflowProviderDefinitionPausedRequest as ProtoSetWorkflowProviderDefinitionPausedRequest,
  type SignalOrStartWorkflowProviderRunRequest as ProtoSignalOrStartWorkflowProviderRunRequest,
  type SignalWorkflowProviderRunRequest as ProtoSignalWorkflowProviderRunRequest,
  type SignalWorkflowRunResponse as ProtoSignalWorkflowRunResponse,
  type StartWorkflowProviderRunRequest as ProtoStartWorkflowProviderRunRequest,
  type WorkflowActivation as ProtoWorkflowActivation,
  type WorkflowAgentMessage as ProtoWorkflowAgentMessage,
  type WorkflowDefinition as ProtoWorkflowDefinition,
  type WorkflowDefinitionSpec as ProtoWorkflowDefinitionSpec,
  type WorkflowEvent as ProtoWorkflowEvent,
  type WorkflowEventActivation as ProtoWorkflowEventActivation,
  type WorkflowEventMatch as ProtoWorkflowEventMatch,
  type WorkflowEventTriggerInvocation as ProtoWorkflowEventTriggerInvocation,
  type WorkflowRun as ProtoWorkflowRun,
  type WorkflowRunEvent as ProtoWorkflowRunEvent,
  type WorkflowRunTrigger as ProtoWorkflowRunTrigger,
  type WorkflowScheduleActivation as ProtoWorkflowScheduleActivation,
  type WorkflowScheduleTrigger as ProtoWorkflowScheduleTrigger,
  type WorkflowSignal as ProtoWorkflowSignal,
  type WorkflowStep as ProtoWorkflowStep,
  type WorkflowStepAgentTurn as ProtoWorkflowStepAgentTurn,
  type WorkflowStepAppCall as ProtoWorkflowStepAppCall,
  type WorkflowStepAttempt as ProtoWorkflowStepAttempt,
  type WorkflowStepExecution as ProtoWorkflowStepExecution,
  type WorkflowStepInputSource as ProtoWorkflowStepInputSource,
  type WorkflowStepOutputSource as ProtoWorkflowStepOutputSource,
  type WorkflowStepWhen as ProtoWorkflowStepWhen,
  type WorkflowText as ProtoWorkflowText,
  type WorkflowValue as ProtoWorkflowValue,
} from "./internal/gen/v1/workflow_pb.ts";
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
import { optionalObjectFromStruct, optionalStruct } from "./protocol-internal.ts";

type WorkflowProviderServiceImpl = Partial<ServiceImpl<typeof WorkflowProviderService>>;

export const WorkflowRunStatus = {
  UNSPECIFIED: ProtoWorkflowRunStatus.UNSPECIFIED,
  PENDING: ProtoWorkflowRunStatus.PENDING,
  RUNNING: ProtoWorkflowRunStatus.RUNNING,
  SUCCEEDED: ProtoWorkflowRunStatus.SUCCEEDED,
  FAILED: ProtoWorkflowRunStatus.FAILED,
  CANCELED: ProtoWorkflowRunStatus.CANCELED,
} as const;
export type WorkflowRunStatus = (typeof WorkflowRunStatus)[keyof typeof WorkflowRunStatus];

export const WorkflowStepStatus = {
  UNSPECIFIED: ProtoWorkflowStepStatus.UNSPECIFIED,
  PENDING: ProtoWorkflowStepStatus.PENDING,
  RUNNING: ProtoWorkflowStepStatus.RUNNING,
  SKIPPED: ProtoWorkflowStepStatus.SKIPPED,
  SUCCEEDED: ProtoWorkflowStepStatus.SUCCEEDED,
  FAILED: ProtoWorkflowStepStatus.FAILED,
  UNKNOWN: ProtoWorkflowStepStatus.UNKNOWN,
} as const;
export type WorkflowStepStatus = (typeof WorkflowStepStatus)[keyof typeof WorkflowStepStatus];

export interface WorkflowText {
  template?: string | undefined;
}

export interface WorkflowPathSource {
  path?: string | undefined;
}

export interface WorkflowStepOutputSource {
  stepId?: string | undefined;
  path?: string | undefined;
}

export interface WorkflowStepInputSource {
  stepId?: string | undefined;
  path?: string | undefined;
}

export type WorkflowValueKind =
  | { case: "literal"; value: JsonInput }
  | { case: "object"; value: Record<string, WorkflowValue> }
  | { case: "array"; value: readonly WorkflowValue[] }
  | { case: "template"; value: WorkflowText | string }
  | { case: "input"; value: string }
  | { case: "signal"; value: string }
  | { case: "stepOutput"; value: WorkflowStepOutputSource }
  | { case: "stepInput"; value: WorkflowStepInputSource }
  | { case: undefined; value?: undefined };

export interface WorkflowValue {
  literal?: JsonInput | undefined;
  object?: Record<string, WorkflowValue> | undefined;
  array?: readonly WorkflowValue[] | undefined;
  template?: WorkflowText | string | undefined;
  input?: string | undefined;
  signal?: string | undefined;
  stepOutput?: WorkflowStepOutputSource | undefined;
  stepInput?: WorkflowStepInputSource | undefined;
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
  output?: AgentOutput | undefined;
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

export interface WorkflowScheduleActivation {
  cron?: string | undefined;
  timezone?: string | undefined;
}

export interface WorkflowEventActivation {
  match?: WorkflowEventMatch | undefined;
}

export type WorkflowActivationTriggerKind =
  | { case: "schedule"; value: WorkflowScheduleActivation }
  | { case: "event"; value: WorkflowEventActivation }
  | { case: undefined; value?: undefined };

export interface WorkflowActivation {
  id?: string | undefined;
  input?: WorkflowValue | undefined;
  paused?: boolean | undefined;
  schedule?: WorkflowScheduleActivation | undefined;
  event?: WorkflowEventActivation | undefined;
  trigger?: WorkflowActivationTriggerKind | undefined;
}

export interface WorkflowDefinitionSpec {
  id?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  activations?: readonly WorkflowActivation[] | undefined;
  paused?: boolean | undefined;
  runAs?: SubjectInput | undefined;
}

export interface WorkflowDefinition {
  id?: string | undefined;
  generation?: bigint | number | undefined;
  target?: BoundWorkflowTarget | undefined;
  activations?: readonly WorkflowActivation[] | undefined;
  paused?: boolean | undefined;
  createdBySubjectId?: string | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  providerName?: string | undefined;
  runAs?: Subject | undefined;
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
  activationId?: string | undefined;
  scheduledFor?: Date | undefined;
}

export interface WorkflowEventTriggerInvocation {
  activationId?: string | undefined;
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

export interface WorkflowStepAttempt {
  id?: string | undefined;
  status?: WorkflowStepStatus | undefined;
  idempotencyKey?: string | undefined;
  input?: JsonInput | undefined;
  output?: JsonInput | undefined;
  statusMessage?: string | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
}

export interface WorkflowStepExecution {
  stepId?: string | undefined;
  status?: WorkflowStepStatus | undefined;
  attempts?: readonly WorkflowStepAttempt[] | undefined;
  input?: JsonInput | undefined;
  output?: JsonInput | undefined;
  statusMessage?: string | undefined;
  skipReason?: string | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
}

export interface WorkflowRun {
  id?: string | undefined;
  status?: WorkflowRunStatus | undefined;
  target?: BoundWorkflowTarget | undefined;
  trigger?: WorkflowRunTrigger | undefined;
  createdAt?: Date | undefined;
  startedAt?: Date | undefined;
  completedAt?: Date | undefined;
  statusMessage?: string | undefined;
  output?: JsonInput | undefined;
  createdBySubjectId?: string | undefined;
  workflowKey?: string | undefined;
  providerName?: string | undefined;
  definitionId?: string | undefined;
  runAs?: Subject | undefined;
  input?: JsonObjectInput | undefined;
  definitionGeneration?: bigint | number | undefined;
  currentStepId?: string | undefined;
  steps?: readonly WorkflowStepExecution[] | undefined;
}

export interface WorkflowRunEvent {
  id?: string | undefined;
  runId?: string | undefined;
  stepId?: string | undefined;
  type?: string | undefined;
  data?: JsonObjectInput | undefined;
  createdAt?: Date | undefined;
}

export interface ListWorkflowProviderDefinitionsResponse {
  definitions: readonly WorkflowDefinition[];
}

export interface ListWorkflowProviderRunsResponse {
  runs: readonly WorkflowRun[];
  nextPageToken?: string | undefined;
}

export interface GetWorkflowProviderRunEventsResponse {
  events: readonly WorkflowRunEvent[];
}

export interface GetWorkflowProviderRunOutputResponse {
  output?: JsonInput | undefined;
}

export interface ApplyWorkflowProviderDefinitionRequest {
  spec?: WorkflowDefinitionSpec | undefined;
  idempotencyKey?: string | undefined;
  requestedBySubjectId?: string | undefined;
}

export interface GetWorkflowProviderDefinitionRequest {
  definitionId: string;
}

export interface ListWorkflowProviderDefinitionsRequest {}

export interface SetWorkflowProviderDefinitionPausedRequest {
  definitionId: string;
  paused: boolean;
  requestedBySubjectId?: string | undefined;
}

export interface SetWorkflowProviderActivationPausedRequest {
  definitionId: string;
  activationId: string;
  paused: boolean;
  requestedBySubjectId?: string | undefined;
}

export interface DeleteWorkflowProviderDefinitionRequest {
  definitionId: string;
}

export interface StartWorkflowProviderRunRequest {
  definitionId?: string | undefined;
  expectedDefinitionGeneration?: bigint | number | undefined;
  input?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  createdBySubjectId?: string | undefined;
  runAs?: SubjectInput | undefined;
  workflowKey?: string | undefined;
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

export interface GetWorkflowProviderRunEventsRequest {
  runId: string;
}

export interface GetWorkflowProviderRunOutputRequest {
  runId: string;
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
  definitionId?: string | undefined;
  expectedDefinitionGeneration?: bigint | number | undefined;
  input?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  createdBySubjectId?: string | undefined;
  runAs?: SubjectInput | undefined;
  signal?: WorkflowSignal | undefined;
}

export interface SignalWorkflowRunResponse {
  run?: WorkflowRun | undefined;
  signal?: WorkflowSignal | undefined;
  startedRun?: boolean | undefined;
  workflowKey?: string | undefined;
}

export interface DeliverWorkflowProviderEventRequest {
  appName?: string | undefined;
  event?: WorkflowEvent | undefined;
  deliveredBySubjectId?: string | undefined;
}

export function workflowText(input: WorkflowText | string = {}): WorkflowText {
  return typeof input === "string" ? { template: input } : { template: input.template ?? "" };
}

export function workflowTextInputFromText(input?: WorkflowText): WorkflowText | undefined {
  return input === undefined ? undefined : { template: input.template };
}

export function workflowPathSource(input: WorkflowPathSource | string = {}): WorkflowPathSource {
  return typeof input === "string" ? { path: input } : { path: input.path ?? "" };
}

export function workflowStepOutputSource(input: WorkflowStepOutputSource = {}): WorkflowStepOutputSource {
  return { stepId: input.stepId ?? "", path: input.path ?? "" };
}

export function workflowStepInputSource(input: WorkflowStepInputSource = {}): WorkflowStepInputSource {
  return { stepId: input.stepId ?? "", path: input.path ?? "" };
}

export function workflowValue(input: WorkflowValue = {}): WorkflowValue {
  if (input.kind !== undefined) return { kind: cloneWorkflowValueKind(input.kind) };
  const selected = [
    Object.prototype.hasOwnProperty.call(input, "literal") ? "literal" : undefined,
    input.object === undefined ? undefined : "object",
    input.array === undefined ? undefined : "array",
    input.template === undefined ? undefined : "template",
    input.input === undefined ? undefined : "input",
    input.signal === undefined ? undefined : "signal",
    input.stepOutput === undefined ? undefined : "stepOutput",
    input.stepInput === undefined ? undefined : "stepInput",
  ].filter((value): value is string => value !== undefined);
  if (selected.length === 0) return { kind: { case: undefined } };
  if (selected.length > 1) throw new Error("workflow value must set exactly one value kind");
  switch (selected[0]) {
    case "literal":
      return { kind: { case: "literal", value: input.literal ?? null } };
    case "object":
      return { kind: { case: "object", value: mapValues(input.object ?? {}, workflowValue) } };
    case "array":
      return { kind: { case: "array", value: (input.array ?? []).map(workflowValue) } };
    case "template":
      return { kind: { case: "template", value: workflowText(input.template ?? {}) } };
    case "input":
      return { kind: { case: "input", value: input.input ?? "" } };
    case "signal":
      return { kind: { case: "signal", value: input.signal ?? "" } };
    case "stepOutput":
      return { kind: { case: "stepOutput", value: workflowStepOutputSource(input.stepOutput) } };
    default:
      return { kind: { case: "stepInput", value: workflowStepInputSource(input.stepInput) } };
  }
}

export function workflowValueInputFromValue(input?: WorkflowValue): WorkflowValue | undefined {
  if (input === undefined) return undefined;
  const kind = input.kind;
  switch (kind?.case) {
    case "literal":
      return { literal: jsonClone(kind.value) };
    case "object":
      return { object: mapValues(kind.value, (value) => workflowValueInputFromValue(value)!) };
    case "array":
      return { array: kind.value.map((value) => workflowValueInputFromValue(value)!) };
    case "template":
      return { template: workflowTextInputFromText(workflowText(kind.value)) };
    case "input":
      return { input: kind.value };
    case "signal":
      return { signal: kind.value };
    case "stepOutput":
      return { stepOutput: workflowStepOutputSource(kind.value) };
    case "stepInput":
      return { stepInput: workflowStepInputSource(kind.value) };
    default:
      return {};
  }
}

export function workflowStepAppCall(input: WorkflowStepAppCall = {}): WorkflowStepAppCall {
  return {
    name: input.name ?? "",
    operation: input.operation ?? "",
    input: input.input === undefined ? undefined : workflowValue(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  };
}

export function workflowAgentMessage(input: WorkflowAgentMessage = {}): WorkflowAgentMessage {
  return {
    role: input.role ?? "",
    text: input.text === undefined ? undefined : workflowText(input.text),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
  };
}

export function workflowStepAgentTurn(input: WorkflowStepAgentTurn): WorkflowStepAgentTurn {
  return {
    provider: input.provider ?? "",
    model: input.model ?? "",
    sessionKey: input.sessionKey ?? "",
    prompt: input.prompt === undefined ? undefined : workflowText(input.prompt),
    messages: input.messages?.map(workflowAgentMessage) ?? [],
    tools: [...(input.tools ?? [])],
    output: input.output,
    modelOptions: input.modelOptions === undefined ? undefined : jsonObjectClone(input.modelOptions),
  };
}

export function workflowStepWhen(input: WorkflowStepWhen = {}): WorkflowStepWhen {
  const out: WorkflowStepWhen = {
    value: input.value === undefined ? undefined : workflowValue(input.value),
  };
  if (Object.prototype.hasOwnProperty.call(input, "equals")) {
    out.equals = input.equals === undefined ? null : jsonClone(input.equals);
  }
  return out;
}

export function workflowStep(input: WorkflowStep = {}): WorkflowStep {
  const app = input.app ?? (input.action?.case === "app" ? input.action.value : undefined);
  const agent = input.agent ?? (input.action?.case === "agent" ? input.action.value : undefined);
  const out: WorkflowStep = {
    id: input.id ?? "",
    inputs: input.inputs === undefined ? undefined : mapValues(input.inputs, workflowValue),
    app: app === undefined ? undefined : workflowStepAppCall(app),
    agent: agent === undefined ? undefined : workflowStepAgentTurn(agent),
    when: input.when === undefined ? undefined : workflowStepWhen(input.when),
    timeoutSeconds: input.timeoutSeconds ?? 0,
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
  };
  if (out.app !== undefined) out.action = { case: "app", value: out.app };
  if (out.agent !== undefined) out.action = { case: "agent", value: out.agent };
  return out;
}

export function boundWorkflowTarget(input: BoundWorkflowTarget = {}): BoundWorkflowTarget {
  return { steps: input.steps?.map(workflowStep) ?? [] };
}

export function boundWorkflowTargetInputFromTarget(input?: BoundWorkflowTarget): BoundWorkflowTarget | undefined {
  return input === undefined ? undefined : { steps: input.steps?.map((step) => workflowStep(step)) };
}

export function boundWorkflowTargetFromTarget(input: BoundWorkflowTarget): BoundWorkflowTarget {
  return boundWorkflowTarget(boundWorkflowTargetInputFromTarget(input) ?? {});
}

export function workflowScheduleActivation(input: WorkflowScheduleActivation = {}): WorkflowScheduleActivation {
  return { cron: input.cron ?? "", timezone: input.timezone ?? "" };
}

export function workflowEventMatch(input: WorkflowEventMatch = {}): WorkflowEventMatch {
  return { type: input.type ?? "", source: input.source ?? "", subject: input.subject ?? "" };
}

export function workflowEventActivation(input: WorkflowEventActivation = {}): WorkflowEventActivation {
  return { match: input.match === undefined ? undefined : workflowEventMatch(input.match) };
}

export function workflowActivation(input: WorkflowActivation = {}): WorkflowActivation {
  const schedule = input.schedule ?? (input.trigger?.case === "schedule" ? input.trigger.value : undefined);
  const event = input.event ?? (input.trigger?.case === "event" ? input.trigger.value : undefined);
  const out: WorkflowActivation = {
    id: input.id ?? "",
    input: input.input === undefined ? undefined : workflowValue(input.input),
    paused: input.paused ?? false,
    schedule: schedule === undefined ? undefined : workflowScheduleActivation(schedule),
    event: event === undefined ? undefined : workflowEventActivation(event),
  };
  if (out.schedule !== undefined) out.trigger = { case: "schedule", value: out.schedule };
  if (out.event !== undefined) out.trigger = { case: "event", value: out.event };
  return out;
}

export function workflowDefinitionSpec(input: WorkflowDefinitionSpec = {}): WorkflowDefinitionSpec {
  return {
    id: input.id ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    activations: input.activations?.map(workflowActivation) ?? [],
    paused: input.paused ?? false,
    runAs: input.runAs,
  };
}

export function workflowDefinition(input: WorkflowDefinition = {}): WorkflowDefinition {
  return {
    id: input.id ?? "",
    generation: BigInt(input.generation ?? 0),
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    activations: input.activations?.map(workflowActivation) ?? [],
    paused: input.paused ?? false,
    createdBySubjectId: input.createdBySubjectId ?? "",
    createdAt: input.createdAt,
    updatedAt: input.updatedAt,
    providerName: input.providerName ?? "",
    runAs: input.runAs,
  };
}

export function workflowSignal(input: WorkflowSignal = {}): WorkflowSignal {
  return {
    id: input.id ?? "",
    name: input.name ?? "",
    payload: input.payload === undefined ? undefined : jsonObjectClone(input.payload),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
    createdBySubjectId: input.createdBySubjectId ?? "",
    createdAt: input.createdAt,
    idempotencyKey: input.idempotencyKey ?? "",
    sequence: BigInt(input.sequence ?? 0),
  };
}

export function workflowEvent(input: WorkflowEvent = {}): WorkflowEvent {
  return {
    id: input.id ?? "",
    source: input.source ?? "",
    specVersion: input.specVersion ?? "",
    type: input.type ?? "",
    subject: input.subject ?? "",
    time: input.time,
    datacontenttype: input.datacontenttype ?? "",
    data: input.data === undefined ? undefined : jsonObjectClone(input.data),
    extensions: input.extensions === undefined ? undefined : mapValues(input.extensions, jsonClone),
  };
}

export function workflowRun(input: WorkflowRun = {}): WorkflowRun {
  return {
    id: input.id ?? "",
    status: input.status ?? WorkflowRunStatus.UNSPECIFIED,
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    trigger: input.trigger === undefined ? undefined : workflowRunTrigger(input.trigger),
    createdAt: input.createdAt,
    startedAt: input.startedAt,
    completedAt: input.completedAt,
    statusMessage: input.statusMessage ?? "",
    output: input.output === undefined ? undefined : jsonClone(input.output),
    createdBySubjectId: input.createdBySubjectId ?? "",
    workflowKey: input.workflowKey ?? "",
    providerName: input.providerName ?? "",
    definitionId: input.definitionId ?? "",
    runAs: input.runAs,
    input: input.input === undefined ? undefined : jsonObjectClone(input.input),
    definitionGeneration: BigInt(input.definitionGeneration ?? 0),
    currentStepId: input.currentStepId ?? "",
    steps: input.steps?.map(workflowStepExecution) ?? [],
  };
}

export function workflowRunTrigger(input: WorkflowRunTrigger = {}): WorkflowRunTrigger {
  if (input.kind !== undefined) return { kind: cloneWorkflowRunTriggerKind(input.kind) };
  if (input.manual) return { kind: { case: "manual", value: {} } };
  if (input.schedule !== undefined) return { kind: { case: "schedule", value: workflowScheduleTrigger(input.schedule) } };
  if (input.event !== undefined) return { kind: { case: "event", value: workflowEventTriggerInvocation(input.event) } };
  return { kind: { case: undefined } };
}

export function workflowScheduleTrigger(input: WorkflowScheduleTrigger = {}): WorkflowScheduleTrigger {
  return { activationId: input.activationId ?? "", scheduledFor: input.scheduledFor };
}

export function workflowEventTriggerInvocation(input: WorkflowEventTriggerInvocation = {}): WorkflowEventTriggerInvocation {
  return { activationId: input.activationId ?? "", event: input.event === undefined ? undefined : workflowEvent(input.event) };
}

export function workflowStepAttempt(input: WorkflowStepAttempt = {}): WorkflowStepAttempt {
  return {
    id: input.id ?? "",
    status: input.status ?? WorkflowStepStatus.UNSPECIFIED,
    idempotencyKey: input.idempotencyKey ?? "",
    input: input.input === undefined ? undefined : jsonClone(input.input),
    output: input.output === undefined ? undefined : jsonClone(input.output),
    statusMessage: input.statusMessage ?? "",
    startedAt: input.startedAt,
    completedAt: input.completedAt,
  };
}

export function workflowStepExecution(input: WorkflowStepExecution = {}): WorkflowStepExecution {
  return {
    stepId: input.stepId ?? "",
    status: input.status ?? WorkflowStepStatus.UNSPECIFIED,
    attempts: input.attempts?.map(workflowStepAttempt) ?? [],
    input: input.input === undefined ? undefined : jsonClone(input.input),
    output: input.output === undefined ? undefined : jsonClone(input.output),
    statusMessage: input.statusMessage ?? "",
    skipReason: input.skipReason ?? "",
    startedAt: input.startedAt,
    completedAt: input.completedAt,
  };
}

export function workflowRunEvent(input: WorkflowRunEvent = {}): WorkflowRunEvent {
  return {
    id: input.id ?? "",
    runId: input.runId ?? "",
    stepId: input.stepId ?? "",
    type: input.type ?? "",
    data: input.data === undefined ? undefined : jsonObjectClone(input.data),
    createdAt: input.createdAt,
  };
}

export interface WorkflowProviderOptions extends ProviderBaseOptions {
  applyDefinition: (request: ApplyWorkflowProviderDefinitionRequest) => MaybePromise<WorkflowDefinition>;
  getDefinition: (request: GetWorkflowProviderDefinitionRequest) => MaybePromise<WorkflowDefinition>;
  listDefinitions: (request: ListWorkflowProviderDefinitionsRequest) => MaybePromise<readonly WorkflowDefinition[] | ListWorkflowProviderDefinitionsResponse>;
  setDefinitionPaused: (request: SetWorkflowProviderDefinitionPausedRequest) => MaybePromise<WorkflowDefinition>;
  setActivationPaused: (request: SetWorkflowProviderActivationPausedRequest) => MaybePromise<WorkflowDefinition>;
  deleteDefinition: (request: DeleteWorkflowProviderDefinitionRequest) => MaybePromise<void>;
  startRun: (request: StartWorkflowProviderRunRequest) => MaybePromise<WorkflowRun>;
  getRun: (request: GetWorkflowProviderRunRequest) => MaybePromise<WorkflowRun>;
  listRuns: (request: ListWorkflowProviderRunsRequest) => MaybePromise<readonly WorkflowRun[] | ListWorkflowProviderRunsResponse>;
  getRunEvents: (request: GetWorkflowProviderRunEventsRequest) => MaybePromise<readonly WorkflowRunEvent[] | GetWorkflowProviderRunEventsResponse>;
  getRunOutput: (request: GetWorkflowProviderRunOutputRequest) => MaybePromise<JsonInput | GetWorkflowProviderRunOutputResponse>;
  cancelRun: (request: CancelWorkflowProviderRunRequest) => MaybePromise<WorkflowRun>;
  signalRun: (request: SignalWorkflowProviderRunRequest) => MaybePromise<SignalWorkflowRunResponse>;
  signalOrStartRun: (request: SignalOrStartWorkflowProviderRunRequest) => MaybePromise<SignalWorkflowRunResponse>;
  deliverEvent: (request: DeliverWorkflowProviderEventRequest) => MaybePromise<WorkflowEvent>;
}

export class WorkflowProvider extends ProviderBase {
  readonly kind = "workflow" as const;

  constructor(private readonly options: WorkflowProviderOptions) {
    super(options);
  }

  applyDefinition(request: ApplyWorkflowProviderDefinitionRequest): MaybePromise<WorkflowDefinition> {
    return this.options.applyDefinition(request);
  }
  getDefinition(request: GetWorkflowProviderDefinitionRequest): MaybePromise<WorkflowDefinition> {
    return this.options.getDefinition(request);
  }
  listDefinitions(request: ListWorkflowProviderDefinitionsRequest): MaybePromise<readonly WorkflowDefinition[] | ListWorkflowProviderDefinitionsResponse> {
    return this.options.listDefinitions(request);
  }
  setDefinitionPaused(request: SetWorkflowProviderDefinitionPausedRequest): MaybePromise<WorkflowDefinition> {
    return this.options.setDefinitionPaused(request);
  }
  setActivationPaused(request: SetWorkflowProviderActivationPausedRequest): MaybePromise<WorkflowDefinition> {
    return this.options.setActivationPaused(request);
  }
  deleteDefinition(request: DeleteWorkflowProviderDefinitionRequest): MaybePromise<void> {
    return this.options.deleteDefinition(request);
  }
  startRun(request: StartWorkflowProviderRunRequest): MaybePromise<WorkflowRun> {
    return this.options.startRun(request);
  }
  getRun(request: GetWorkflowProviderRunRequest): MaybePromise<WorkflowRun> {
    return this.options.getRun(request);
  }
  listRuns(request: ListWorkflowProviderRunsRequest): MaybePromise<readonly WorkflowRun[] | ListWorkflowProviderRunsResponse> {
    return this.options.listRuns(request);
  }
  getRunEvents(request: GetWorkflowProviderRunEventsRequest): MaybePromise<readonly WorkflowRunEvent[] | GetWorkflowProviderRunEventsResponse> {
    return this.options.getRunEvents(request);
  }
  getRunOutput(request: GetWorkflowProviderRunOutputRequest): MaybePromise<JsonInput | GetWorkflowProviderRunOutputResponse> {
    return this.options.getRunOutput(request);
  }
  cancelRun(request: CancelWorkflowProviderRunRequest): MaybePromise<WorkflowRun> {
    return this.options.cancelRun(request);
  }
  signalRun(request: SignalWorkflowProviderRunRequest): MaybePromise<SignalWorkflowRunResponse> {
    return this.options.signalRun(request);
  }
  signalOrStartRun(request: SignalOrStartWorkflowProviderRunRequest): MaybePromise<SignalWorkflowRunResponse> {
    return this.options.signalOrStartRun(request);
  }
  deliverEvent(request: DeliverWorkflowProviderEventRequest): MaybePromise<WorkflowEvent> {
    return this.options.deliverEvent(request);
  }
}

export function defineWorkflowProvider(options: WorkflowProviderOptions): WorkflowProvider {
  return new WorkflowProvider(options);
}

export function isWorkflowProvider(value: unknown): value is WorkflowProvider {
  return (
    value instanceof WorkflowProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "workflow" &&
      "applyDefinition" in value &&
      "getDefinition" in value &&
      "listDefinitions" in value &&
      "setDefinitionPaused" in value &&
      "setActivationPaused" in value &&
      "deleteDefinition" in value &&
      "startRun" in value &&
      "getRun" in value &&
      "listRuns" in value &&
      "getRunEvents" in value &&
      "getRunOutput" in value &&
      "cancelRun" in value &&
      "signalRun" in value &&
      "signalOrStartRun" in value &&
      "deliverEvent" in value)
  );
}

export function createWorkflowProviderService(provider: WorkflowProvider): WorkflowProviderServiceImpl {
  return {
    async applyDefinition(request) {
      return create(WorkflowDefinitionSchema, workflowDefinitionToProto(
        await invokeWorkflowProvider("apply definition", () => provider.applyDefinition(applyWorkflowProviderDefinitionRequestFromProto(request))),
      ));
    },
    async getDefinition(request) {
      return create(WorkflowDefinitionSchema, workflowDefinitionToProto(
        await invokeWorkflowProvider("get definition", () => provider.getDefinition(getWorkflowProviderDefinitionRequestFromProto(request))),
      ));
    },
    async listDefinitions(request) {
      const result = listDefinitionsResult(await invokeWorkflowProvider("list definitions", () => provider.listDefinitions({})));
      void request;
      return create(ListWorkflowProviderDefinitionsResponseSchema, {
        definitions: result.definitions.map(workflowDefinitionToProto),
      });
    },
    async setDefinitionPaused(request) {
      return create(WorkflowDefinitionSchema, workflowDefinitionToProto(
        await invokeWorkflowProvider("set definition paused", () => provider.setDefinitionPaused(setWorkflowProviderDefinitionPausedRequestFromProto(request))),
      ));
    },
    async setActivationPaused(request) {
      return create(WorkflowDefinitionSchema, workflowDefinitionToProto(
        await invokeWorkflowProvider("set activation paused", () => provider.setActivationPaused(setWorkflowProviderActivationPausedRequestFromProto(request))),
      ));
    },
    async deleteDefinition(request) {
      await invokeWorkflowProvider("delete definition", () => provider.deleteDefinition(deleteWorkflowProviderDefinitionRequestFromProto(request)));
      return create(EmptySchema, {});
    },
    async startRun(request) {
      return create(WorkflowRunSchema, workflowRunToProto(
        await invokeWorkflowProvider("start run", () => provider.startRun(startWorkflowProviderRunRequestFromProto(request))),
      ));
    },
    async getRun(request) {
      return create(WorkflowRunSchema, workflowRunToProto(
        await invokeWorkflowProvider("get run", () => provider.getRun(getWorkflowProviderRunRequestFromProto(request))),
      ));
    },
    async listRuns(request) {
      const result = listRunsResult(await invokeWorkflowProvider("list runs", () => provider.listRuns(listWorkflowProviderRunsRequestFromProto(request))));
      return create(ListWorkflowProviderRunsResponseSchema, {
        runs: result.runs.map(workflowRunToProto),
        nextPageToken: result.nextPageToken ?? "",
      });
    },
    async getRunEvents(request) {
      const result = runEventsResult(await invokeWorkflowProvider("get run events", () => provider.getRunEvents(getWorkflowProviderRunEventsRequestFromProto(request))));
      return create(GetWorkflowProviderRunEventsResponseSchema, {
        events: result.events.map(workflowRunEventToProto),
      });
    },
    async getRunOutput(request) {
      const result = runOutputResult(await invokeWorkflowProvider("get run output", () => provider.getRunOutput(getWorkflowProviderRunOutputRequestFromProto(request))));
      return create(GetWorkflowProviderRunOutputResponseSchema, {
        output: result.output === undefined ? undefined : valueFromJson(result.output),
      });
    },
    async cancelRun(request) {
      return create(WorkflowRunSchema, workflowRunToProto(
        await invokeWorkflowProvider("cancel run", () => provider.cancelRun(cancelWorkflowProviderRunRequestFromProto(request))),
      ));
    },
    async signalRun(request) {
      return create(SignalWorkflowRunResponseSchema, signalWorkflowRunResponseToProto(
        await invokeWorkflowProvider("signal run", () => provider.signalRun(signalWorkflowProviderRunRequestFromProto(request))),
      ));
    },
    async signalOrStartRun(request) {
      return create(SignalWorkflowRunResponseSchema, signalWorkflowRunResponseToProto(
        await invokeWorkflowProvider("signal or start run", () => provider.signalOrStartRun(signalOrStartWorkflowProviderRunRequestFromProto(request))),
      ));
    },
    async deliverEvent(request) {
      return workflowEventToProto(await invokeWorkflowProvider("deliver event", () => provider.deliverEvent(deliverWorkflowProviderEventRequestFromProto(request)))) ?? create(WorkflowEventSchema, {});
    },
  };
}

export function workflowTextToProto(input?: WorkflowText | string): ProtoWorkflowText | undefined {
  if (input === undefined) return undefined;
  const text = workflowText(input);
  return create(WorkflowTextSchema, { template: text.template ?? "" });
}

export function workflowTextFromProto(input?: ProtoWorkflowText): WorkflowText | undefined {
  return input === undefined ? undefined : { template: input.template };
}

export function workflowPathSourceToProto(input?: WorkflowPathSource | string): { path: string } | undefined {
  if (input === undefined) return undefined;
  const source = workflowPathSource(input);
  return create(WorkflowPathSourceSchema, { path: source.path ?? "" });
}

export function workflowStepOutputSourceToProto(input?: WorkflowStepOutputSource): ProtoWorkflowStepOutputSource | undefined {
  if (input === undefined) return undefined;
  const source = workflowStepOutputSource(input);
  return create(WorkflowStepOutputSourceSchema, {
    stepId: source.stepId ?? "",
    path: source.path ?? "",
  });
}

export function workflowStepOutputSourceFromProto(input?: ProtoWorkflowStepOutputSource): WorkflowStepOutputSource | undefined {
  return input === undefined ? undefined : { stepId: input.stepId, path: input.path };
}

export function workflowStepInputSourceToProto(input?: WorkflowStepInputSource): ProtoWorkflowStepInputSource | undefined {
  if (input === undefined) return undefined;
  const source = workflowStepInputSource(input);
  return create(WorkflowStepInputSourceSchema, {
    stepId: source.stepId ?? "",
    path: source.path ?? "",
  });
}

export function workflowStepInputSourceFromProto(input?: ProtoWorkflowStepInputSource): WorkflowStepInputSource | undefined {
  return input === undefined ? undefined : { stepId: input.stepId, path: input.path };
}

export function workflowValueToProto(input?: WorkflowValue): ProtoWorkflowValue | undefined {
  if (input === undefined) return undefined;
  const kind = workflowValue(input).kind;
  switch (kind?.case) {
    case "literal":
      return create(WorkflowValueSchema, { kind: { case: "literal", value: valueFromJson(kind.value) } });
    case "object":
      return create(WorkflowValueSchema, {
        kind: { case: "object", value: create(WorkflowObjectSchema, { fields: mapValues(kind.value, (value) => workflowValueToProto(value)!) }) },
      });
    case "array":
      return create(WorkflowValueSchema, {
        kind: { case: "array", value: create(WorkflowArraySchema, { values: kind.value.map((value) => workflowValueToProto(value)!) }) },
      });
    case "template":
      return create(WorkflowValueSchema, { kind: { case: "template", value: workflowTextToProto(kind.value)! } });
    case "input":
      return create(WorkflowValueSchema, { kind: { case: "input", value: workflowPathSourceToProto(kind.value)! } });
    case "signal":
      return create(WorkflowValueSchema, { kind: { case: "signal", value: workflowPathSourceToProto(kind.value)! } });
    case "stepOutput":
      return create(WorkflowValueSchema, { kind: { case: "stepOutput", value: workflowStepOutputSourceToProto(kind.value)! } });
    case "stepInput":
      return create(WorkflowValueSchema, { kind: { case: "stepInput", value: workflowStepInputSourceToProto(kind.value)! } });
    default:
      return create(WorkflowValueSchema);
  }
}

export function workflowValueFromProto(input?: ProtoWorkflowValue): WorkflowValue | undefined {
  if (input === undefined) return undefined;
  switch (input.kind.case) {
    case "literal":
      return { kind: { case: "literal", value: jsonFromValue(input.kind.value) as JsonInput } };
    case "object":
      return { kind: { case: "object", value: mapValues(input.kind.value.fields, (value) => workflowValueFromProto(value)!) } };
    case "array":
      return { kind: { case: "array", value: input.kind.value.values.map((value) => workflowValueFromProto(value)!) } };
    case "template":
      return { kind: { case: "template", value: workflowTextFromProto(input.kind.value)! } };
    case "input":
      return { kind: { case: "input", value: input.kind.value.path } };
    case "signal":
      return { kind: { case: "signal", value: input.kind.value.path } };
    case "stepOutput":
      return { kind: { case: "stepOutput", value: workflowStepOutputSourceFromProto(input.kind.value)! } };
    case "stepInput":
      return { kind: { case: "stepInput", value: workflowStepInputSourceFromProto(input.kind.value)! } };
    default:
      return { kind: { case: undefined } };
  }
}

export function workflowStepAppCallToProto(input?: WorkflowStepAppCall): ProtoWorkflowStepAppCall | undefined {
  if (input === undefined) return undefined;
  return create(WorkflowStepAppCallSchema, {
    name: input.name ?? "",
    operation: input.operation ?? "",
    input: workflowValueToProto(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  });
}

export function workflowStepAppCallFromProto(input?: ProtoWorkflowStepAppCall): WorkflowStepAppCall | undefined {
  if (input === undefined) return undefined;
  return {
    name: input.name,
    operation: input.operation,
    input: workflowValueFromProto(input.input),
    connection: input.connection,
    instance: input.instance,
    credentialMode: input.credentialMode,
  };
}

export function workflowAgentMessageToProto(input: WorkflowAgentMessage): ProtoWorkflowAgentMessage {
  return create(WorkflowAgentMessageSchema, {
    role: input.role ?? "",
    text: workflowTextToProto(input.text),
    metadata: optionalStruct(input.metadata),
  });
}

export function workflowAgentMessageFromProto(input?: ProtoWorkflowAgentMessage): WorkflowAgentMessage | undefined {
  if (input === undefined) return undefined;
  return {
    role: input.role,
    text: workflowTextFromProto(input.text),
    metadata: optionalObjectFromStruct(input.metadata),
  };
}

export function workflowStepAgentTurnToProto(input?: WorkflowStepAgentTurn): ProtoWorkflowStepAgentTurn | undefined {
  if (input === undefined) return undefined;
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

export function workflowStepAgentTurnFromProto(input?: ProtoWorkflowStepAgentTurn): WorkflowStepAgentTurn | undefined {
  if (input === undefined) return undefined;
  return {
    provider: input.provider,
    model: input.model,
    sessionKey: input.sessionKey,
    prompt: workflowTextFromProto(input.prompt),
    messages: input.messages.map((message) => workflowAgentMessageFromProto(message)!),
    tools: input.tools.map(agentToolRefFromProto),
    output: agentOutputFromProto(input.output),
    modelOptions: optionalObjectFromStruct(input.modelOptions),
  };
}

export function workflowStepWhenToProto(input?: WorkflowStepWhen): ProtoWorkflowStepWhen | undefined {
  if (input === undefined) return undefined;
  const out = create(WorkflowStepWhenSchema, { value: workflowValueToProto(input.value) });
  if (Object.prototype.hasOwnProperty.call(input, "equals")) {
    out.equals = valueFromJson(input.equals === undefined ? null : input.equals);
  }
  return out;
}

export function workflowStepWhenFromProto(input?: ProtoWorkflowStepWhen): WorkflowStepWhen | undefined {
  if (input === undefined) return undefined;
  const out: WorkflowStepWhen = { value: workflowValueFromProto(input.value) };
  if (input.equals !== undefined) out.equals = jsonFromValue(input.equals) as JsonInput;
  return out;
}

export function workflowStepToProto(input: WorkflowStep): ProtoWorkflowStep {
  const step = workflowStep(input);
  const app = step.app ?? (step.action?.case === "app" ? step.action.value : undefined);
  const agent = step.agent ?? (step.action?.case === "agent" ? step.action.value : undefined);
  return create(WorkflowStepSchema, {
    id: step.id ?? "",
    inputs: mapValues(step.inputs ?? {}, (value) => workflowValueToProto(value)!),
    when: workflowStepWhenToProto(step.when),
    timeoutSeconds: step.timeoutSeconds ?? 0,
    metadata: optionalStruct(step.metadata),
    action: app !== undefined
      ? { case: "app", value: workflowStepAppCallToProto(app)! }
      : agent !== undefined
        ? { case: "agent", value: workflowStepAgentTurnToProto(agent)! }
        : { case: undefined },
  });
}

export function workflowStepFromProto(input?: ProtoWorkflowStep): WorkflowStep | undefined {
  if (input === undefined) return undefined;
  const app = input.action.case === "app" ? workflowStepAppCallFromProto(input.action.value) : undefined;
  const agent = input.action.case === "agent" ? workflowStepAgentTurnFromProto(input.action.value) : undefined;
  return workflowStep({
    id: input.id,
    inputs: mapValues(input.inputs, (value) => workflowValueFromProto(value)!),
    when: workflowStepWhenFromProto(input.when),
    timeoutSeconds: input.timeoutSeconds,
    metadata: optionalObjectFromStruct(input.metadata),
    app,
    agent,
  });
}

export function boundWorkflowTargetToProto(input?: BoundWorkflowTarget): ProtoBoundWorkflowTarget | undefined {
  if (input === undefined) return undefined;
  return create(BoundWorkflowTargetSchema, { steps: input.steps?.map(workflowStepToProto) ?? [] });
}

export function boundWorkflowTargetFromProto(input?: ProtoBoundWorkflowTarget): BoundWorkflowTarget | undefined {
  if (input === undefined) return undefined;
  return { steps: input.steps.map((step) => workflowStepFromProto(step)!) };
}

export function workflowScheduleActivationToProto(input?: WorkflowScheduleActivation): ProtoWorkflowScheduleActivation | undefined {
  if (input === undefined) return undefined;
  const schedule = workflowScheduleActivation(input);
  return create(WorkflowScheduleActivationSchema, {
    cron: schedule.cron ?? "",
    timezone: schedule.timezone ?? "",
  });
}

export function workflowScheduleActivationFromProto(input?: ProtoWorkflowScheduleActivation): WorkflowScheduleActivation | undefined {
  return input === undefined ? undefined : { cron: input.cron, timezone: input.timezone };
}

export function workflowEventMatchToProto(input?: WorkflowEventMatch): ProtoWorkflowEventMatch | undefined {
  if (input === undefined) return undefined;
  const match = workflowEventMatch(input);
  return create(WorkflowEventMatchSchema, {
    type: match.type ?? "",
    source: match.source ?? "",
    subject: match.subject ?? "",
  });
}

export function workflowEventMatchFromProto(input?: ProtoWorkflowEventMatch): WorkflowEventMatch | undefined {
  return input === undefined ? undefined : { type: input.type, source: input.source, subject: input.subject };
}

export function workflowEventActivationToProto(input?: WorkflowEventActivation): ProtoWorkflowEventActivation | undefined {
  if (input === undefined) return undefined;
  return create(WorkflowEventActivationSchema, { match: workflowEventMatchToProto(input.match) });
}

export function workflowEventActivationFromProto(input?: ProtoWorkflowEventActivation): WorkflowEventActivation | undefined {
  return input === undefined ? undefined : { match: workflowEventMatchFromProto(input.match) };
}

export function workflowActivationToProto(input: WorkflowActivation): ProtoWorkflowActivation {
  const activation = workflowActivation(input);
  return create(WorkflowActivationSchema, {
    id: activation.id ?? "",
    input: workflowValueToProto(activation.input),
    paused: activation.paused ?? false,
    trigger: activation.trigger?.case === "schedule"
      ? { case: "schedule", value: workflowScheduleActivationToProto(activation.trigger.value)! }
      : activation.trigger?.case === "event"
        ? { case: "event", value: workflowEventActivationToProto(activation.trigger.value)! }
        : { case: undefined },
  });
}

export function workflowActivationFromProto(input?: ProtoWorkflowActivation): WorkflowActivation | undefined {
  if (input === undefined) return undefined;
  return workflowActivation({
    id: input.id,
    input: workflowValueFromProto(input.input),
    paused: input.paused,
    schedule: input.trigger.case === "schedule" ? workflowScheduleActivationFromProto(input.trigger.value) : undefined,
    event: input.trigger.case === "event" ? workflowEventActivationFromProto(input.trigger.value) : undefined,
  });
}

export function workflowDefinitionSpecToProto(input?: WorkflowDefinitionSpec): ProtoWorkflowDefinitionSpec | undefined {
  if (input === undefined) return undefined;
  const spec = workflowDefinitionSpec(input);
  return create(WorkflowDefinitionSpecSchema, {
    id: spec.id ?? "",
    target: boundWorkflowTargetToProto(spec.target),
    activations: spec.activations?.map(workflowActivationToProto) ?? [],
    paused: spec.paused ?? false,
    runAs: subjectToProto(spec.runAs),
  });
}

export function workflowDefinitionToProto(input: WorkflowDefinition): ProtoWorkflowDefinition {
  const definition = workflowDefinition(input);
  return create(WorkflowDefinitionSchema, {
    id: definition.id ?? "",
    generation: BigInt(definition.generation ?? 0),
    target: boundWorkflowTargetToProto(definition.target),
    activations: definition.activations?.map(workflowActivationToProto) ?? [],
    paused: definition.paused ?? false,
    createdBySubjectId: definition.createdBySubjectId ?? "",
    createdAt: optionalTimestamp(definition.createdAt),
    updatedAt: optionalTimestamp(definition.updatedAt),
    providerName: definition.providerName ?? "",
    runAs: subjectToProto(definition.runAs),
  });
}

export function workflowDefinitionFromProto(input?: ProtoWorkflowDefinition): WorkflowDefinition | undefined {
  if (input === undefined) return undefined;
  return workflowDefinition({
    id: input.id,
    generation: input.generation,
    target: boundWorkflowTargetFromProto(input.target),
    activations: input.activations.map((activation) => workflowActivationFromProto(activation)!),
    paused: input.paused,
    createdBySubjectId: input.createdBySubjectId,
    createdAt: optionalDate(input.createdAt),
    updatedAt: optionalDate(input.updatedAt),
    providerName: input.providerName,
    runAs: subjectFromProto(input.runAs),
  });
}

export function workflowSignalToProto(input?: WorkflowSignal): ProtoWorkflowSignal | undefined {
  if (input === undefined) return undefined;
  return create(WorkflowSignalSchema, {
    id: input.id ?? "",
    name: input.name ?? "",
    payload: optionalStruct(input.payload),
    metadata: optionalStruct(input.metadata),
    createdBySubjectId: input.createdBySubjectId ?? "",
    createdAt: optionalTimestamp(input.createdAt),
    idempotencyKey: input.idempotencyKey ?? "",
    sequence: BigInt(input.sequence ?? 0),
  });
}

export function workflowSignalFromProto(input?: ProtoWorkflowSignal): WorkflowSignal | undefined {
  if (input === undefined) return undefined;
  return workflowSignal({
    id: input.id,
    name: input.name,
    payload: optionalObjectFromStruct(input.payload),
    metadata: optionalObjectFromStruct(input.metadata),
    createdBySubjectId: input.createdBySubjectId,
    createdAt: optionalDate(input.createdAt),
    idempotencyKey: input.idempotencyKey,
    sequence: input.sequence,
  });
}

export function workflowEventToProto(input?: WorkflowEvent): ProtoWorkflowEvent | undefined {
  if (input === undefined) return undefined;
  return create(WorkflowEventSchema, {
    id: input.id ?? "",
    source: input.source ?? "",
    specVersion: input.specVersion ?? "",
    type: input.type ?? "",
    subject: input.subject ?? "",
    time: optionalTimestamp(input.time),
    datacontenttype: input.datacontenttype ?? "",
    data: optionalStruct(input.data),
    extensions: mapValues(input.extensions ?? {}, valueFromJson),
  });
}

export function workflowEventFromProto(input?: ProtoWorkflowEvent): WorkflowEvent | undefined {
  if (input === undefined) return undefined;
  return workflowEvent({
    id: input.id,
    source: input.source,
    specVersion: input.specVersion,
    type: input.type,
    subject: input.subject,
    time: optionalDate(input.time),
    datacontenttype: input.datacontenttype,
    data: optionalObjectFromStruct(input.data),
    extensions: mapValues(input.extensions, (value) => jsonFromValue(value) as JsonInput),
  });
}

export function workflowScheduleTriggerToProto(input?: WorkflowScheduleTrigger): ProtoWorkflowScheduleTrigger | undefined {
  if (input === undefined) return undefined;
  return create(WorkflowScheduleTriggerSchema, {
    activationId: input.activationId ?? "",
    scheduledFor: optionalTimestamp(input.scheduledFor),
  });
}

export function workflowScheduleTriggerFromProto(input?: ProtoWorkflowScheduleTrigger): WorkflowScheduleTrigger | undefined {
  return input === undefined ? undefined : workflowScheduleTrigger({ activationId: input.activationId, scheduledFor: optionalDate(input.scheduledFor) });
}

export function workflowEventTriggerInvocationToProto(input?: WorkflowEventTriggerInvocation): ProtoWorkflowEventTriggerInvocation | undefined {
  if (input === undefined) return undefined;
  return create(WorkflowEventTriggerInvocationSchema, {
    activationId: input.activationId ?? "",
    event: workflowEventToProto(input.event),
  });
}

export function workflowEventTriggerInvocationFromProto(input?: ProtoWorkflowEventTriggerInvocation): WorkflowEventTriggerInvocation | undefined {
  return input === undefined ? undefined : workflowEventTriggerInvocation({ activationId: input.activationId, event: workflowEventFromProto(input.event) });
}

export function workflowRunTriggerToProto(input?: WorkflowRunTrigger): ProtoWorkflowRunTrigger | undefined {
  if (input === undefined) return undefined;
  const trigger = workflowRunTrigger(input);
  switch (trigger.kind?.case) {
    case "manual":
      return create(WorkflowRunTriggerSchema, { kind: { case: "manual", value: create(WorkflowManualTriggerSchema) } });
    case "schedule":
      return create(WorkflowRunTriggerSchema, { kind: { case: "schedule", value: workflowScheduleTriggerToProto(trigger.kind.value)! } });
    case "event":
      return create(WorkflowRunTriggerSchema, { kind: { case: "event", value: workflowEventTriggerInvocationToProto(trigger.kind.value)! } });
    default:
      return create(WorkflowRunTriggerSchema);
  }
}

export function workflowRunTriggerFromProto(input?: ProtoWorkflowRunTrigger): WorkflowRunTrigger | undefined {
  if (input === undefined) return undefined;
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

export function workflowStepAttemptToProto(input: WorkflowStepAttempt): ProtoWorkflowStepAttempt {
  return create(WorkflowStepAttemptSchema, {
    id: input.id ?? "",
    status: input.status ?? WorkflowStepStatus.UNSPECIFIED,
    idempotencyKey: input.idempotencyKey ?? "",
    input: input.input === undefined ? undefined : valueFromJson(input.input),
    output: input.output === undefined ? undefined : valueFromJson(input.output),
    statusMessage: input.statusMessage ?? "",
    startedAt: optionalTimestamp(input.startedAt),
    completedAt: optionalTimestamp(input.completedAt),
  });
}

export function workflowStepAttemptFromProto(input?: ProtoWorkflowStepAttempt): WorkflowStepAttempt | undefined {
  if (input === undefined) return undefined;
  return workflowStepAttempt({
    id: input.id,
    status: input.status as WorkflowStepStatus,
    idempotencyKey: input.idempotencyKey,
    input: input.input === undefined ? undefined : jsonFromValue(input.input) as JsonInput,
    output: input.output === undefined ? undefined : jsonFromValue(input.output) as JsonInput,
    statusMessage: input.statusMessage,
    startedAt: optionalDate(input.startedAt),
    completedAt: optionalDate(input.completedAt),
  });
}

export function workflowStepExecutionToProto(input: WorkflowStepExecution): ProtoWorkflowStepExecution {
  return create(WorkflowStepExecutionSchema, {
    stepId: input.stepId ?? "",
    status: input.status ?? WorkflowStepStatus.UNSPECIFIED,
    attempts: input.attempts?.map(workflowStepAttemptToProto) ?? [],
    input: input.input === undefined ? undefined : valueFromJson(input.input),
    output: input.output === undefined ? undefined : valueFromJson(input.output),
    statusMessage: input.statusMessage ?? "",
    skipReason: input.skipReason ?? "",
    startedAt: optionalTimestamp(input.startedAt),
    completedAt: optionalTimestamp(input.completedAt),
  });
}

export function workflowStepExecutionFromProto(input?: ProtoWorkflowStepExecution): WorkflowStepExecution | undefined {
  if (input === undefined) return undefined;
  return workflowStepExecution({
    stepId: input.stepId,
    status: input.status as WorkflowStepStatus,
    attempts: input.attempts.map((attempt) => workflowStepAttemptFromProto(attempt)!),
    input: input.input === undefined ? undefined : jsonFromValue(input.input) as JsonInput,
    output: input.output === undefined ? undefined : jsonFromValue(input.output) as JsonInput,
    statusMessage: input.statusMessage,
    skipReason: input.skipReason,
    startedAt: optionalDate(input.startedAt),
    completedAt: optionalDate(input.completedAt),
  });
}

export function workflowRunToProto(input: WorkflowRun): ProtoWorkflowRun {
  const run = workflowRun(input);
  return create(WorkflowRunSchema, {
    id: run.id ?? "",
    status: run.status ?? WorkflowRunStatus.UNSPECIFIED,
    target: boundWorkflowTargetToProto(run.target),
    trigger: workflowRunTriggerToProto(run.trigger),
    createdAt: optionalTimestamp(run.createdAt),
    startedAt: optionalTimestamp(run.startedAt),
    completedAt: optionalTimestamp(run.completedAt),
    statusMessage: run.statusMessage ?? "",
    output: run.output === undefined ? undefined : valueFromJson(run.output),
    createdBySubjectId: run.createdBySubjectId ?? "",
    workflowKey: run.workflowKey ?? "",
    providerName: run.providerName ?? "",
    definitionId: run.definitionId ?? "",
    runAs: subjectToProto(run.runAs),
    input: optionalStruct(run.input),
    definitionGeneration: BigInt(run.definitionGeneration ?? 0),
    currentStepId: run.currentStepId ?? "",
    steps: run.steps?.map(workflowStepExecutionToProto) ?? [],
  });
}

export function workflowRunFromProto(input?: ProtoWorkflowRun): WorkflowRun | undefined {
  if (input === undefined) return undefined;
  return workflowRun({
    id: input.id,
    status: input.status as WorkflowRunStatus,
    target: boundWorkflowTargetFromProto(input.target),
    trigger: workflowRunTriggerFromProto(input.trigger),
    createdAt: optionalDate(input.createdAt),
    startedAt: optionalDate(input.startedAt),
    completedAt: optionalDate(input.completedAt),
    statusMessage: input.statusMessage,
    output: input.output === undefined ? undefined : jsonFromValue(input.output) as JsonInput,
    createdBySubjectId: input.createdBySubjectId,
    workflowKey: input.workflowKey,
    providerName: input.providerName,
    definitionId: input.definitionId,
    runAs: subjectFromProto(input.runAs),
    input: optionalObjectFromStruct(input.input),
    definitionGeneration: input.definitionGeneration,
    currentStepId: input.currentStepId,
    steps: input.steps.map((step) => workflowStepExecutionFromProto(step)!),
  });
}

export function workflowRunEventToProto(input: WorkflowRunEvent): ProtoWorkflowRunEvent {
  return create(WorkflowRunEventSchema, {
    id: input.id ?? "",
    runId: input.runId ?? "",
    stepId: input.stepId ?? "",
    type: input.type ?? "",
    data: optionalStruct(input.data),
    createdAt: optionalTimestamp(input.createdAt),
  });
}

export function workflowRunEventFromProto(input?: ProtoWorkflowRunEvent): WorkflowRunEvent | undefined {
  if (input === undefined) return undefined;
  return workflowRunEvent({
    id: input.id,
    runId: input.runId,
    stepId: input.stepId,
    type: input.type,
    data: optionalObjectFromStruct(input.data),
    createdAt: optionalDate(input.createdAt),
  });
}

export function signalWorkflowRunResponseToProto(input: SignalWorkflowRunResponse): ProtoSignalWorkflowRunResponse {
  return create(SignalWorkflowRunResponseSchema, {
    run: workflowRunToProto(input.run ?? {}),
    signal: workflowSignalToProto(input.signal),
    startedRun: input.startedRun ?? false,
    workflowKey: input.workflowKey ?? "",
  });
}

export function workflowRunSignalFromProto(input?: ProtoSignalWorkflowRunResponse): SignalWorkflowRunResponse | undefined {
  if (input === undefined) return undefined;
  return {
    run: workflowRunFromProto(input.run),
    signal: workflowSignalFromProto(input.signal),
    startedRun: input.startedRun,
    workflowKey: input.workflowKey,
  };
}

function applyWorkflowProviderDefinitionRequestFromProto(input: ProtoApplyWorkflowProviderDefinitionRequest): ApplyWorkflowProviderDefinitionRequest {
  return {
    spec: workflowDefinitionSpecFromProto(input.spec),
    idempotencyKey: input.idempotencyKey,
    requestedBySubjectId: input.requestedBySubjectId,
  };
}

function getWorkflowProviderDefinitionRequestFromProto(input: ProtoGetWorkflowProviderDefinitionRequest): GetWorkflowProviderDefinitionRequest {
  return { definitionId: input.definitionId };
}

function setWorkflowProviderDefinitionPausedRequestFromProto(input: ProtoSetWorkflowProviderDefinitionPausedRequest): SetWorkflowProviderDefinitionPausedRequest {
  return { definitionId: input.definitionId, paused: input.paused, requestedBySubjectId: input.requestedBySubjectId };
}

function setWorkflowProviderActivationPausedRequestFromProto(input: ProtoSetWorkflowProviderActivationPausedRequest): SetWorkflowProviderActivationPausedRequest {
  return { definitionId: input.definitionId, activationId: input.activationId, paused: input.paused, requestedBySubjectId: input.requestedBySubjectId };
}

function deleteWorkflowProviderDefinitionRequestFromProto(input: { definitionId: string }): DeleteWorkflowProviderDefinitionRequest {
  return { definitionId: input.definitionId };
}

function startWorkflowProviderRunRequestFromProto(input: ProtoStartWorkflowProviderRunRequest): StartWorkflowProviderRunRequest {
  return {
    definitionId: input.definitionId,
    expectedDefinitionGeneration: input.expectedDefinitionGeneration,
    input: optionalObjectFromStruct(input.input),
    idempotencyKey: input.idempotencyKey,
    createdBySubjectId: input.createdBySubjectId,
    runAs: subjectInputFromProto(input.runAs),
    workflowKey: input.workflowKey,
  };
}

function getWorkflowProviderRunRequestFromProto(input: ProtoGetWorkflowProviderRunRequest): GetWorkflowProviderRunRequest {
  return { runId: input.runId };
}

function listWorkflowProviderRunsRequestFromProto(input: ProtoListWorkflowProviderRunsRequest): ListWorkflowProviderRunsRequest {
  return {
    pageSize: input.pageSize,
    pageToken: input.pageToken,
    status: input.status as WorkflowRunStatus,
    targetApp: input.targetApp,
  };
}

function getWorkflowProviderRunEventsRequestFromProto(input: ProtoGetWorkflowProviderRunEventsRequest): GetWorkflowProviderRunEventsRequest {
  return { runId: input.runId };
}

function getWorkflowProviderRunOutputRequestFromProto(input: ProtoGetWorkflowProviderRunOutputRequest): GetWorkflowProviderRunOutputRequest {
  return { runId: input.runId };
}

function cancelWorkflowProviderRunRequestFromProto(input: ProtoCancelWorkflowProviderRunRequest): CancelWorkflowProviderRunRequest {
  return { runId: input.runId, reason: input.reason };
}

function signalWorkflowProviderRunRequestFromProto(input: ProtoSignalWorkflowProviderRunRequest): SignalWorkflowProviderRunRequest {
  return { runId: input.runId, signal: workflowSignalFromProto(input.signal) };
}

function signalOrStartWorkflowProviderRunRequestFromProto(input: ProtoSignalOrStartWorkflowProviderRunRequest): SignalOrStartWorkflowProviderRunRequest {
  return {
    workflowKey: input.workflowKey,
    definitionId: input.definitionId,
    expectedDefinitionGeneration: input.expectedDefinitionGeneration,
    input: optionalObjectFromStruct(input.input),
    idempotencyKey: input.idempotencyKey,
    createdBySubjectId: input.createdBySubjectId,
    runAs: subjectInputFromProto(input.runAs),
    signal: workflowSignalFromProto(input.signal),
  };
}

function deliverWorkflowProviderEventRequestFromProto(input: ProtoDeliverWorkflowProviderEventRequest): DeliverWorkflowProviderEventRequest {
  return {
    appName: input.appName,
    event: workflowEventFromProto(input.event),
    deliveredBySubjectId: input.deliveredBySubjectId,
  };
}

export function workflowDefinitionSpecFromProto(input?: ProtoWorkflowDefinitionSpec): WorkflowDefinitionSpec | undefined {
  if (input === undefined) return undefined;
  return workflowDefinitionSpec({
    id: input.id,
    target: boundWorkflowTargetFromProto(input.target),
    activations: input.activations.map((activation) => workflowActivationFromProto(activation)!),
    paused: input.paused,
    runAs: subjectInputFromProto(input.runAs),
  });
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
  steps?: Record<string, { inputs?: Record<string, unknown>; outputs?: unknown }> | undefined;
}

export interface WorkflowRunContextTrigger {
  kind: string;
  activationId: string;
  scheduledFor: string;
  event?: Record<string, JsonInput> | undefined;
}

export interface WorkflowRunContextSignal {
  id: string;
  name: string;
  payload: Record<string, JsonInput>;
  metadata: Record<string, JsonInput>;
  createdBySubjectId: string;
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
  createdBySubjectId: string;
  latestSignal?: WorkflowRunContextSignal | undefined;
}

export interface WorkflowEvalContext {
  request: WorkflowExecutionRequest;
  outputs?: Record<string, unknown> | undefined;
  inputs?: Record<string, unknown> | undefined;
  allowInputs?: boolean | undefined;
}

export class WorkflowValueError extends Error {}

export function evaluateWorkflowStepInputs(ctx: WorkflowEvalContext, values?: Record<string, WorkflowValue>): Record<string, unknown> | undefined {
  if (values === undefined || Object.keys(values).length === 0) return undefined;
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(values)) {
    const resolved = evaluateWorkflowValue(ctx, value);
    if (!resolved.ok) throw new WorkflowValueError(`inputs.${key} did not resolve`);
    out[key] = resolved.value;
  }
  return out;
}

export function evaluateWorkflowValue(ctx: WorkflowEvalContext, input: WorkflowValue): { value: unknown; ok: boolean } {
  const kind = workflowValue(input).kind;
  switch (kind?.case) {
    case "literal":
      return { value: jsonClone(kind.value), ok: true };
    case "object": {
      const out: Record<string, unknown> = {};
      for (const [key, value] of Object.entries(kind.value)) {
        const resolved = evaluateWorkflowValue(ctx, value);
        if (!resolved.ok) return resolved;
        out[key] = resolved.value;
      }
      return { value: out, ok: true };
    }
    case "array": {
      const out: unknown[] = [];
      for (const value of kind.value) {
        const resolved = evaluateWorkflowValue(ctx, value);
        if (!resolved.ok) return resolved;
        out.push(resolved.value);
      }
      return { value: out, ok: true };
    }
    case "template":
      return { value: renderWorkflowTemplate(ctx, workflowText(kind.value).template ?? ""), ok: true };
    case "input":
      return mapPathValue(ctx.request.input ?? {}, kind.value);
    case "signal": {
      const signal = latestWorkflowSignal(ctx.request.signals);
      return signal === undefined ? { value: undefined, ok: false } : pathValue(signal.payload, kind.value);
    }
    case "stepOutput":
      return pathValue(ctx.outputs?.[kind.value.stepId ?? ""], kind.value.path ?? "");
    case "stepInput":
      return pathValue(ctx.request.steps?.[kind.value.stepId ?? ""]?.inputs, kind.value.path ?? "");
    default:
      return { value: undefined, ok: false };
  }
}

export function renderWorkflowTemplate(ctx: WorkflowEvalContext, template: string): string {
  return template.replace(/\$\$\{\{/g, "\u0000{{").replace(/\$\{\{([^{}]+)\}\}/g, (_match, expr: string) => {
    const resolved = templateExpressionValue(ctx, expr.trim());
    if (!resolved.ok) throw new WorkflowValueError(`template expression "${expr.trim()}" did not resolve`);
    return renderTemplateValue(resolved.value);
  }).replace(/\u0000\{\{/g, "${{");
}

export function pathValue(value: unknown, path: string): { value: unknown; ok: boolean } {
  let current = value;
  if (path.trim() === "") return { value: current, ok: true };
  for (const segment of pathSegments(path)) {
    if (current !== null && typeof current === "object" && typeof segment === "string") {
      if (!Object.prototype.hasOwnProperty.call(current, segment)) return { value: undefined, ok: false };
      current = (current as Record<string, unknown>)[segment];
      continue;
    }
    if (typeof segment === "number" && Array.isArray(current)) {
      if (segment < 0 || segment >= current.length) return { value: undefined, ok: false };
      current = current[segment];
      continue;
    }
    return { value: undefined, ok: false };
  }
  return { value: current, ok: true };
}

export function workflowRunContext(input: WorkflowExecutionRequest = {}): WorkflowRunContext {
  const signals = workflowSignalsContext(input.signals);
  return {
    provider: input.providerName ?? "",
    runId: input.runId ?? "",
    target: workflowTargetContext(input.target),
    trigger: workflowTriggerContext(input.trigger),
    input: valueMapInput(input.input),
    metadata: valueMapInput(input.metadata),
    createdBySubjectId: input.createdBySubjectId ?? "",
    signals,
    latestSignal: signals.at(-1),
  };
}

export function parseWorkflowRunContext(value: unknown): WorkflowRunContext {
  const data = workflowRunContextData(value);
  const signals = Array.isArray(data.signals)
    ? data.signals.map(workflowRunContextSignal).filter((signal): signal is WorkflowRunContextSignal => signal !== undefined)
    : [];
  return {
    provider: workflowContextString(data.provider ?? data.providerName),
    runId: workflowContextString(data.runId),
    target: workflowContextOptionalObject(data.target),
    trigger: workflowRunContextTrigger(data.trigger),
    input: workflowContextObject(data.input),
    metadata: workflowContextObject(data.metadata),
    createdBySubjectId: workflowContextString(data.createdBySubjectId),
    signals,
    latestSignal: signals.at(-1),
  };
}

export function workflowSignalsContext(signals?: readonly WorkflowSignal[]): readonly WorkflowRunContextSignal[] {
  return (signals ?? []).map((signal) => ({
    id: signal.id ?? "",
    name: signal.name ?? "",
    payload: compactWorkflowSignalPayload(signal.payload ?? {}),
    metadata: valueMapInput(signal.metadata as Record<string, JsonInput> | undefined),
    createdBySubjectId: signal.createdBySubjectId ?? "",
    createdAt: signal.createdAt?.toISOString() ?? "",
    idempotencyKey: signal.idempotencyKey ?? "",
    sequence: typeof signal.sequence === "number" ? signal.sequence : typeof signal.sequence === "bigint" ? Number(signal.sequence) : undefined,
  }));
}

export function subjectToProto(input?: SubjectInput | Subject): SubjectContext | undefined {
  if (input === undefined) return undefined;
  return create(SubjectContextSchema, {
    id: input.id ?? "",
    credentialSubjectId: input.credentialSubjectId ?? "",
    email: input.email ?? "",
  });
}

export function subjectFromProto(input?: SubjectContext): Subject | undefined {
  if (input === undefined) return undefined;
  return {
    id: input.id,
    credentialSubjectId: input.credentialSubjectId,
    email: input.email,
  };
}

export function subjectInputFromProto(input?: SubjectContext): SubjectInput | undefined {
  if (input === undefined) return undefined;
  return {
    id: input.id,
    credentialSubjectId: input.credentialSubjectId,
    email: input.email,
  };
}

function listDefinitionsResult(value: readonly WorkflowDefinition[] | ListWorkflowProviderDefinitionsResponse): ListWorkflowProviderDefinitionsResponse {
  return "definitions" in value ? value : { definitions: value };
}

function listRunsResult(value: readonly WorkflowRun[] | ListWorkflowProviderRunsResponse): ListWorkflowProviderRunsResponse {
  return "runs" in value ? value : { runs: value };
}

function runEventsResult(value: readonly WorkflowRunEvent[] | GetWorkflowProviderRunEventsResponse): GetWorkflowProviderRunEventsResponse {
  return "events" in value ? value : { events: value };
}

function runOutputResult(value: JsonInput | GetWorkflowProviderRunOutputResponse): GetWorkflowProviderRunOutputResponse {
  return value !== null && typeof value === "object" && "output" in value ? value : { output: value };
}

async function invokeWorkflowProvider<T>(action: string, fn: () => Promise<T> | T): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    if (error instanceof ConnectError) throw error;
    throw new ConnectError(`workflow provider ${action}: ${errorMessage(error)}`, Code.Unknown);
  }
}

function jsonObjectClone(input: JsonObjectInput): JsonObjectInput {
  return structFromObject(jsonObjectFromStruct(input as JsonObject));
}

function jsonClone(input: JsonInput): JsonInput {
  return jsonFromValue(valueFromJson(input)) as JsonInput;
}

function optionalTimestamp(value?: Date) {
  return value === undefined ? undefined : timestampFromDate(value);
}

function optionalDate(timestamp?: Parameters<typeof dateFromTimestamp>[0]) {
  return timestamp === undefined ? undefined : dateFromTimestamp(timestamp);
}

function mapValues<T, U>(input: Record<string, T>, fn: (value: T, key: string) => U): Record<string, U> {
  return Object.fromEntries(Object.entries(input).map(([key, value]) => [key, fn(value, key)]));
}

function cloneWorkflowValueKind(kind: WorkflowValueKind): WorkflowValueKind {
  switch (kind.case) {
    case "literal":
      return { case: "literal", value: jsonClone(kind.value) };
    case "object":
      return { case: "object", value: mapValues(kind.value, workflowValue) };
    case "array":
      return { case: "array", value: kind.value.map(workflowValue) };
    case "template":
      return { case: "template", value: workflowText(kind.value) };
    case "input":
    case "signal":
      return { case: kind.case, value: kind.value };
    case "stepOutput":
      return { case: "stepOutput", value: workflowStepOutputSource(kind.value) };
    case "stepInput":
      return { case: "stepInput", value: workflowStepInputSource(kind.value) };
    default:
      return { case: undefined };
  }
}

function cloneWorkflowRunTriggerKind(kind: WorkflowRunTriggerKind): WorkflowRunTriggerKind {
  switch (kind.case) {
    case "manual":
      return { case: "manual", value: {} };
    case "schedule":
      return { case: "schedule", value: workflowScheduleTrigger(kind.value) };
    case "event":
      return { case: "event", value: workflowEventTriggerInvocation(kind.value) };
    default:
      return { case: undefined };
  }
}

function valueMapInput(input?: Record<string, JsonInput>): Record<string, JsonInput> {
  return input === undefined ? {} : { ...input };
}

function latestWorkflowSignal(signals?: readonly WorkflowSignal[]): WorkflowSignal | undefined {
  return signals === undefined || signals.length === 0 ? undefined : signals[signals.length - 1];
}

function templateExpressionValue(ctx: WorkflowEvalContext, expr: string): { value: unknown; ok: boolean } {
  if (expr.startsWith("inputs.")) {
    if (!ctx.allowInputs) throw new WorkflowValueError("inputs references are not allowed here");
    return mapPathValue(ctx.inputs, expr.slice("inputs.".length));
  }
  if (expr.startsWith("input.")) return mapPathValue(ctx.request.input, expr.slice("input.".length));
  if (expr.startsWith("signal.")) {
    const signal = latestWorkflowSignal(ctx.request.signals);
    return signal === undefined ? { value: undefined, ok: false } : pathValue(signal, expr.slice("signal.".length));
  }
  if (expr.startsWith("steps.")) {
    return pathValue(ctx.request.steps ?? {}, expr.slice("steps.".length));
  }
  throw new WorkflowValueError(`unsupported template expression "${expr}"`);
}

function mapPathValue(value: unknown, path: string): { value: unknown; ok: boolean } {
  return pathValue(value, path);
}

function renderTemplateValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}

function pathSegments(path: string): Array<string | number> {
  const out: Array<string | number> = [];
  for (let i = 0; i < path.length;) {
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
    out.push(path.slice(start, i));
  }
  return out.filter((segment) => segment !== "");
}

function parseBracketPathToken(token: string, path: string): string | number {
  if (/^-?\d+$/.test(token)) return Number.parseInt(token, 10);
  if ((token.startsWith("'") && token.endsWith("'")) || (token.startsWith("\"") && token.endsWith("\""))) {
    return token.slice(1, -1).replace(/\\'/g, "'").replace(/\\"/g, "\"").replace(/\\\\/g, "\\");
  }
  throw new WorkflowValueError(`invalid workflow path "${path}"`);
}

function workflowTargetContext(target?: BoundWorkflowTarget): Record<string, JsonInput> | undefined {
  if (target === undefined) return undefined;
  return {
    kind: "steps",
    steps: (target.steps ?? []).map((step) => {
      const app = step.app ?? (step.action?.case === "app" ? step.action.value : undefined);
      const agent = step.agent ?? (step.action?.case === "agent" ? step.action.value : undefined);
      if (app !== undefined) {
        return {
          id: step.id ?? "",
          kind: "app",
          app: app.name ?? "",
          operation: app.operation ?? "",
          credentialMode: app.credentialMode ?? "",
        };
      }
      return {
        id: step.id ?? "",
        kind: "agent",
        provider: agent?.provider ?? "",
        model: agent?.model ?? "",
      };
    }),
  };
}

function workflowTriggerContext(trigger?: WorkflowRunTrigger): WorkflowRunContextTrigger {
  const normalized = workflowRunTrigger(trigger);
  switch (normalized.kind?.case) {
    case "schedule":
      return {
        kind: "schedule",
        activationId: normalized.kind.value.activationId ?? "",
        scheduledFor: normalized.kind.value.scheduledFor?.toISOString() ?? "",
      };
    case "event":
      return {
        kind: "event",
        activationId: normalized.kind.value.activationId ?? "",
        scheduledFor: "",
        event: workflowEventContext(normalized.kind.value.event),
      };
    case "manual":
      return { kind: "manual", activationId: "", scheduledFor: "" };
    default:
      return { kind: "", activationId: "", scheduledFor: "" };
  }
}

function workflowEventContext(event?: WorkflowEvent): Record<string, JsonInput> | undefined {
  if (event === undefined) return undefined;
  return {
    id: event.id ?? "",
    source: event.source ?? "",
    specVersion: event.specVersion ?? "",
    type: event.type ?? "",
    subject: event.subject ?? "",
    datacontenttype: event.datacontenttype ?? "",
    data: valueMapInput(event.data as Record<string, JsonInput> | undefined),
  };
}

function workflowRunContextData(value: unknown): Record<string, unknown> {
  const root = workflowContextOptionalObject(value);
  const workflow = workflowContextOptionalObject(root?.workflow);
  return workflow ?? root ?? {};
}

function workflowRunContextTrigger(value: unknown): WorkflowRunContextTrigger {
  const data = workflowContextOptionalObject(value) ?? {};
  const event = workflowContextOptionalObject(data.event);
  return {
    kind: workflowContextString(data.kind),
    activationId: workflowContextString(data.activationId),
    scheduledFor: workflowContextString(data.scheduledFor),
    event: event === undefined ? undefined : event as Record<string, JsonInput>,
  };
}

function workflowRunContextSignal(value: unknown): WorkflowRunContextSignal | undefined {
  if (value === null || typeof value !== "object") return undefined;
  const record = value as Record<string, unknown>;
  const sequence = workflowContextNumber(record.sequence);
  return {
    id: workflowContextString(record.id),
    name: workflowContextString(record.name),
    payload: workflowContextObject(record.payload),
    metadata: workflowContextObject(record.metadata),
    createdBySubjectId: workflowContextString(record.createdBySubjectId),
    createdAt: workflowContextString(record.createdAt),
    idempotencyKey: workflowContextString(record.idempotencyKey),
    sequence,
  };
}

function workflowContextString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function workflowContextNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function workflowContextObject(value: unknown): Record<string, JsonInput> {
  return workflowContextOptionalObject(value) ?? {};
}

function workflowContextOptionalObject(value: unknown): Record<string, JsonInput> | undefined {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return undefined;
  return value as Record<string, JsonInput>;
}

function compactWorkflowSignalPayload(payload: JsonObjectInput): Record<string, JsonInput> {
  const source = payload as Record<string, JsonInput>;
  const out: Record<string, JsonInput> = {};
  const fields: Record<string, JsonInput> = {};
  for (const [key, value] of Object.entries(source)) {
    if (workflowSignalPayloadKeyExcluded(key)) {
      if (key === "payload") {
        out.payloadOmitted = true;
      } else {
        out[key] = value;
      }
    } else {
      fields[key] = value;
    }
  }
  if (Object.keys(fields).length > 0) out.fields = fields;
  return out;
}

function workflowSignalPayloadKeyExcluded(key: string): boolean {
  return [
    "delivery_id", "deliveryId", "payload", "raw", "github", "github_event", "githubEvent",
    "check_run", "checkRun", "check_suite", "checkSuite", "workflow_run", "workflowRun",
  ].includes(key);
}
