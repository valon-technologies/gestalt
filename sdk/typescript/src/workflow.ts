import { create, type JsonObject } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type ServiceImpl,
} from "@connectrpc/connect";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";

import {
  BoundWorkflowAgentTargetSchema,
  BoundWorkflowDefinitionSchema,
  BoundWorkflowEventTriggerSchema,
  BoundWorkflowPluginTargetSchema,
  BoundWorkflowRunSchema,
  BoundWorkflowScheduleSchema,
  BoundWorkflowTargetSchema,
  InvokeWorkflowOperationRequestSchema,
  ListWorkflowExecutionReferencesResponseSchema,
  ListWorkflowProviderEventTriggersResponseSchema,
  ListWorkflowProviderRunsResponseSchema,
  ListWorkflowProviderSchedulesResponseSchema,
  SignalWorkflowRunResponseSchema,
  WorkflowAccessPermissionSchema,
  WorkflowActorSchema,
  WorkflowEventMatchSchema,
  WorkflowEventSchema,
  WorkflowEventTriggerInvocationSchema,
  WorkflowExecutionReferenceSchema,
  WorkflowHost as WorkflowHostService,
  WorkflowManualTriggerSchema,
  WorkflowAgentStepSchema,
  WorkflowAgentStepWhenSchema,
  WorkflowOutputBindingSchema,
  WorkflowOutputDeliverySchema,
  WorkflowOutputValueSourceSchema,
  WorkflowProvider as WorkflowProviderService,
  WorkflowRunAsSubjectSchema,
  WorkflowRunStatus as ProtoWorkflowRunStatus,
  WorkflowRunTriggerSchema,
  WorkflowScheduleTriggerSchema,
  WorkflowSignalSchema,
  type BoundWorkflowAgentTarget as ProtoBoundWorkflowAgentTarget,
  type BoundWorkflowDefinition as ProtoBoundWorkflowDefinition,
  type BoundWorkflowEventTrigger as ProtoBoundWorkflowEventTrigger,
  type BoundWorkflowPluginTarget as ProtoBoundWorkflowPluginTarget,
  type BoundWorkflowRun as ProtoBoundWorkflowRun,
  type BoundWorkflowSchedule as ProtoBoundWorkflowSchedule,
  type BoundWorkflowTarget as ProtoBoundWorkflowTarget,
  type CancelWorkflowProviderRunRequest as ProtoCancelWorkflowProviderRunRequest,
  type CreateWorkflowProviderDefinitionRequest as ProtoCreateWorkflowProviderDefinitionRequest,
  type DeleteWorkflowProviderDefinitionRequest as ProtoDeleteWorkflowProviderDefinitionRequest,
  type DeleteWorkflowProviderEventTriggerRequest as ProtoDeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest as ProtoDeleteWorkflowProviderScheduleRequest,
  type GetWorkflowExecutionReferenceRequest as ProtoGetWorkflowExecutionReferenceRequest,
  type GetWorkflowProviderDefinitionRequest as ProtoGetWorkflowProviderDefinitionRequest,
  type GetWorkflowProviderEventTriggerRequest as ProtoGetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest as ProtoGetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest as ProtoGetWorkflowProviderScheduleRequest,
  type ListWorkflowExecutionReferencesRequest as ProtoListWorkflowExecutionReferencesRequest,
  type ListWorkflowProviderEventTriggersRequest as ProtoListWorkflowProviderEventTriggersRequest,
  type ListWorkflowProviderRunsRequest as ProtoListWorkflowProviderRunsRequest,
  type ListWorkflowProviderSchedulesRequest as ProtoListWorkflowProviderSchedulesRequest,
  type PauseWorkflowProviderEventTriggerRequest as ProtoPauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest as ProtoPauseWorkflowProviderScheduleRequest,
  type PublishWorkflowProviderEventRequest as ProtoPublishWorkflowProviderEventRequest,
  type PutWorkflowExecutionReferenceRequest as ProtoPutWorkflowExecutionReferenceRequest,
  type ResumeWorkflowProviderEventTriggerRequest as ProtoResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest as ProtoResumeWorkflowProviderScheduleRequest,
  type SignalOrStartWorkflowProviderRunRequest as ProtoSignalOrStartWorkflowProviderRunRequest,
  type SignalWorkflowProviderRunRequest as ProtoSignalWorkflowProviderRunRequest,
  type SignalWorkflowRunResponse as ProtoSignalWorkflowRunResponse,
  type StartWorkflowProviderRunRequest as ProtoStartWorkflowProviderRunRequest,
  type UpdateWorkflowProviderDefinitionRequest as ProtoUpdateWorkflowProviderDefinitionRequest,
  type UpsertWorkflowProviderEventTriggerRequest as ProtoUpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest as ProtoUpsertWorkflowProviderScheduleRequest,
  type WorkflowAccessPermission as ProtoWorkflowAccessPermission,
  type WorkflowActor as ProtoWorkflowActor,
  type WorkflowEvent as ProtoWorkflowEvent,
  type WorkflowEventMatch as ProtoWorkflowEventMatch,
  type WorkflowEventTriggerInvocation as ProtoWorkflowEventTriggerInvocation,
  type WorkflowExecutionReference as ProtoWorkflowExecutionReference,
  type WorkflowAgentStep as ProtoWorkflowAgentStep,
  type WorkflowAgentStepWhen as ProtoWorkflowAgentStepWhen,
  type WorkflowOutputBinding as ProtoWorkflowOutputBinding,
  type WorkflowOutputDelivery as ProtoWorkflowOutputDelivery,
  type WorkflowOutputValueSource as ProtoWorkflowOutputValueSource,
  type WorkflowRunAsSubject as ProtoWorkflowRunAsSubject,
  type WorkflowRunTrigger as ProtoWorkflowRunTrigger,
  type WorkflowScheduleTrigger as ProtoWorkflowScheduleTrigger,
  type WorkflowSignal as ProtoWorkflowSignal,
} from "./internal/gen/v1/workflow_pb.ts";
import type {
  AgentMessage,
  AgentToolRef,
} from "./agent.ts";
import {
  agentMessageFromProto,
  agentMessageToProto,
  agentToolRefFromProto,
  agentToolRefToProto,
} from "./agent-conversions.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
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

export interface BoundWorkflowPluginTarget {
  pluginName?: string | undefined;
  operation?: string | undefined;
  input?: JsonObjectInput | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  credentialMode?: string | undefined;
}

export type WorkflowOutputValueSourceKind =
  | { case: "agentOutput"; value: string }
  | { case: "signalPayload"; value: string }
  | { case: "signalMetadata"; value: string }
  | { case: "literal"; value: JsonInput }
  | { case: "agentSession"; value: string }
  | { case: undefined; value?: undefined };

export interface WorkflowOutputValueSource {
  agentOutput?: string | undefined;
  signalPayload?: string | undefined;
  signalMetadata?: string | undefined;
  literal?: JsonInput | undefined;
  agentSession?: string | undefined;
  kind?: WorkflowOutputValueSourceKind | undefined;
}

export interface WorkflowOutputBinding {
  inputField?: string | undefined;
  value?: WorkflowOutputValueSource | undefined;
}

export interface WorkflowOutputDelivery {
  target?: BoundWorkflowPluginTarget | undefined;
  inputBindings?: readonly WorkflowOutputBinding[] | undefined;
  credentialMode?: string | undefined;
}

export interface BoundWorkflowAgentTarget {
  providerName?: string | undefined;
  model?: string | undefined;
  prompt?: string | undefined;
  messages?: readonly AgentMessage[] | undefined;
  toolRefs?: readonly AgentToolRef[] | undefined;
  responseSchema?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  timeoutSeconds?: number | undefined;
  outputDelivery?: WorkflowOutputDelivery | undefined;
  modelOptions?: JsonObjectInput | undefined;
  sessionReadyDelivery?: WorkflowOutputDelivery | undefined;
  steps?: readonly WorkflowAgentStep[] | undefined;
}

export interface WorkflowAgentStep {
  id?: string | undefined;
  prompt?: string | undefined;
  messages?: readonly AgentMessage[] | undefined;
  toolRefs?: readonly AgentToolRef[] | undefined;
  responseSchema?: JsonObjectInput | undefined;
  modelOptions?: JsonObjectInput | undefined;
  timeoutSeconds?: number | undefined;
  outputDelivery?: WorkflowOutputDelivery | undefined;
  when?: WorkflowAgentStepWhen | undefined;
  metadata?: JsonObjectInput | undefined;
}

export interface WorkflowAgentStepWhen {
  stepId?: string | undefined;
  outputPath?: string | undefined;
  equals?: JsonInput | undefined;
}

export type BoundWorkflowTargetKind =
  | { case: "plugin"; value: BoundWorkflowPluginTarget }
  | { case: "agent"; value: BoundWorkflowAgentTarget }
  | { case: undefined; value?: undefined };

export interface BoundWorkflowTarget {
  plugin?: BoundWorkflowPluginTarget | undefined;
  agent?: BoundWorkflowAgentTarget | undefined;
  kind?: BoundWorkflowTargetKind | undefined;
}

export interface WorkflowActor {
  subjectId?: string | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
}

export interface WorkflowRunAsSubject {
  subjectId?: string | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
}

export interface WorkflowAccessPermission {
  plugin?: string | undefined;
  operations?: readonly string[] | undefined;
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
  createdBy?: WorkflowActor | undefined;
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
  createdBy?: WorkflowActor | undefined;
  executionRef?: string | undefined;
  workflowKey?: string | undefined;
  providerName?: string | undefined;
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
  createdBy?: WorkflowActor | undefined;
  executionRef?: string | undefined;
  providerName?: string | undefined;
}

export interface BoundWorkflowEventTrigger {
  id?: string | undefined;
  match?: WorkflowEventMatch | undefined;
  target?: BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  createdAt?: Date | undefined;
  updatedAt?: Date | undefined;
  createdBy?: WorkflowActor | undefined;
  executionRef?: string | undefined;
  providerName?: string | undefined;
}

export interface BoundWorkflowDefinition {
  id?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  createdBy?: WorkflowActor | undefined;
  createdAt?: Date | undefined;
  providerName?: string | undefined;
}

export interface WorkflowExecutionReference {
  id?: string | undefined;
  providerName?: string | undefined;
  target?: BoundWorkflowTarget | undefined;
  subjectId?: string | undefined;
  credentialSubjectId?: string | undefined;
  permissions?: readonly WorkflowAccessPermission[] | undefined;
  createdAt?: Date | undefined;
  revokedAt?: Date | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
  callerPluginName?: string | undefined;
  runAs?: WorkflowRunAsSubject | undefined;
  sourceDefinitionId?: string | undefined;
}

export interface StartWorkflowProviderRunRequest {
  target?: BoundWorkflowTarget | undefined;
  idempotencyKey: string;
  createdBy?: WorkflowActor | undefined;
  executionRef: string;
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
  targetPlugin?: string | undefined;
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
  createdBy?: WorkflowActor | undefined;
  executionRef: string;
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
}

export interface GetWorkflowProviderDefinitionRequest {
  definitionId: string;
}

export interface UpdateWorkflowProviderDefinitionRequest {
  definitionId: string;
  target?: BoundWorkflowTarget | undefined;
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
  requestedBy?: WorkflowActor | undefined;
  executionRef: string;
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
  requestedBy?: WorkflowActor | undefined;
  executionRef: string;
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

export interface PutWorkflowExecutionReferenceRequest {
  reference?: WorkflowExecutionReference | undefined;
}

export interface GetWorkflowExecutionReferenceRequest {
  id: string;
}

export interface ListWorkflowExecutionReferencesRequest {
  subjectId: string;
}

export interface PublishWorkflowProviderEventRequest {
  pluginName: string;
  event?: WorkflowEvent | undefined;
  publishedBy?: WorkflowActor | undefined;
}

/** Native input for invoking a workflow operation through the host service. */
export interface InvokeWorkflowOperationInput {
  target?: BoundWorkflowTarget | undefined;
  runId?: string | undefined;
  trigger?: WorkflowRunTrigger | undefined;
  input?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  createdBy?: WorkflowActor | undefined;
  executionRef?: string | undefined;
  signals?: readonly WorkflowSignal[] | undefined;
}

/** Native response returned after invoking a workflow operation. */
export interface InvokeWorkflowOperationResponse {
  status: number;
  body: string;
}

export interface WorkflowManagerSchedule {
  providerName?: string | undefined;
  schedule?: BoundWorkflowSchedule | undefined;
}

export interface WorkflowManagerEventTrigger {
  providerName?: string | undefined;
  trigger?: BoundWorkflowEventTrigger | undefined;
}

export interface WorkflowManagerDefinition {
  providerName?: string | undefined;
  definition?: BoundWorkflowDefinition | undefined;
}

export interface WorkflowManagerRun {
  providerName?: string | undefined;
  run?: BoundWorkflowRun | undefined;
}

export interface WorkflowManagerRunSignal {
  providerName?: string | undefined;
  run?: BoundWorkflowRun | undefined;
  signal?: WorkflowSignal | undefined;
  startedRun?: boolean | undefined;
  workflowKey?: string | undefined;
}

/** Creates workflow actor metadata from native input. */
export function workflowActor(input: WorkflowActor = {}): WorkflowActor {
  return {
    subjectId: input.subjectId ?? "",
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
  };
}

/** Returns native input copied from workflow actor metadata. */
export function workflowActorInputFromActor(input?: WorkflowActor): WorkflowActor | undefined {
  return input === undefined ? undefined : { ...input };
}

/** Creates workflow run-as metadata from native input. */
export function workflowRunAsSubject(
  input: WorkflowRunAsSubject = {},
): WorkflowRunAsSubject {
  return {
    subjectId: input.subjectId ?? "",
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
  };
}

/** Returns native input copied from workflow run-as metadata. */
export function workflowRunAsSubjectInputFromSubject(
  input?: WorkflowRunAsSubject,
): WorkflowRunAsSubject | undefined {
  return input === undefined ? undefined : { ...input };
}

/** Creates an execution-reference permission from native input. */
export function workflowAccessPermission(
  input: WorkflowAccessPermission = {},
): WorkflowAccessPermission {
  return {
    plugin: input.plugin ?? "",
    operations: [...(input.operations ?? [])],
  };
}

/** Returns native input copied from an execution-reference permission. */
export function workflowAccessPermissionInputFromPermission(
  input: WorkflowAccessPermission,
): WorkflowAccessPermission {
  return {
    plugin: input.plugin,
    operations: [...(input.operations ?? [])],
  };
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

/** Creates a workflow output value source from native input. */
export function workflowOutputValueSource(
  input: WorkflowOutputValueSource = {},
): WorkflowOutputValueSource {
  if ("kind" in input && input.kind !== undefined) {
    return { kind: cloneWorkflowOutputValueSourceKind(input.kind) };
  }
  const sourceInput = input as WorkflowOutputValueSource;
  const selected = [
    sourceInput.agentOutput === undefined ? undefined : "agentOutput",
    sourceInput.signalPayload === undefined ? undefined : "signalPayload",
    sourceInput.signalMetadata === undefined ? undefined : "signalMetadata",
    Object.prototype.hasOwnProperty.call(sourceInput, "literal") ? "literal" : undefined,
    sourceInput.agentSession === undefined ? undefined : "agentSession",
  ].filter((value): value is string => value !== undefined);
  if (selected.length === 0) {
    return { kind: { case: undefined } };
  }
  if (selected.length > 1) {
    throw new Error("workflow output value source must set exactly one source");
  }
  switch (selected[0]) {
    case "agentOutput":
      return { kind: { case: "agentOutput", value: sourceInput.agentOutput ?? "" } };
    case "signalPayload":
      return { kind: { case: "signalPayload", value: sourceInput.signalPayload ?? "" } };
    case "signalMetadata":
      return { kind: { case: "signalMetadata", value: sourceInput.signalMetadata ?? "" } };
    case "agentSession":
      return { kind: { case: "agentSession", value: sourceInput.agentSession ?? "" } };
    default:
      return { kind: { case: "literal", value: sourceInput.literal ?? null } };
  }
}

/** Returns native input copied from a workflow output value source. */
export function workflowOutputValueSourceInputFromSource(
  input?: WorkflowOutputValueSource,
): WorkflowOutputValueSource | undefined {
  if (input === undefined) {
    return undefined;
  }
  const kind = input.kind;
  switch (kind?.case) {
    case "agentOutput":
      return { agentOutput: kind.value };
    case "signalPayload":
      return { signalPayload: kind.value };
    case "signalMetadata":
      return { signalMetadata: kind.value };
    case "agentSession":
      return { agentSession: kind.value };
    case "literal":
      return { literal: kind.value };
    default:
      return {};
  }
}

/** Creates a workflow output binding from native input. */
export function workflowOutputBinding(
  input: WorkflowOutputBinding = {},
): WorkflowOutputBinding {
  return {
    inputField: input.inputField ?? "",
    value: input.value === undefined ? undefined : workflowOutputValueSource(input.value),
  };
}

/** Returns native input copied from a workflow output binding. */
export function workflowOutputBindingInputFromBinding(
  input: WorkflowOutputBinding,
): WorkflowOutputBinding {
  return {
    inputField: input.inputField,
    value: input.value === undefined
      ? undefined
      : workflowOutputValueSourceInputFromSource(workflowOutputValueSource(input.value)),
  };
}

/** Creates a workflow output delivery from native input. */
export function workflowOutputDelivery(
  input: WorkflowOutputDelivery = {},
): WorkflowOutputDelivery {
  return {
    target: input.target === undefined ? undefined : boundWorkflowPluginTarget(input.target),
    inputBindings: input.inputBindings?.map((binding) => workflowOutputBinding(binding)) ?? [],
    credentialMode: input.credentialMode ?? "",
  };
}

/** Returns native input copied from a workflow output delivery. */
export function workflowOutputDeliveryInputFromDelivery(
  input?: WorkflowOutputDelivery,
): WorkflowOutputDelivery | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    target: boundWorkflowPluginTargetInputFromTarget(input.target),
    inputBindings: input.inputBindings?.map((binding) => workflowOutputBindingInputFromBinding(binding)) ?? [],
    credentialMode: input.credentialMode,
  };
}

/** Creates a bound plugin workflow target from native input. */
export function boundWorkflowPluginTarget(
  input: BoundWorkflowPluginTarget = {},
): BoundWorkflowPluginTarget {
  return {
    pluginName: input.pluginName ?? "",
    operation: input.operation ?? "",
    input: input.input === undefined ? undefined : structFromObject(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  };
}

/** Returns native input copied from a bound plugin workflow target. */
export function boundWorkflowPluginTargetInputFromTarget(
  input?: BoundWorkflowPluginTarget,
): BoundWorkflowPluginTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    pluginName: input.pluginName,
    operation: input.operation,
    input: input.input === undefined ? undefined : jsonObjectClone(input.input),
    connection: input.connection,
    instance: input.instance,
    credentialMode: input.credentialMode,
  };
}

/** Creates a bound agent workflow target from native input. */
export function boundWorkflowAgentTarget(
  input: BoundWorkflowAgentTarget = {},
): BoundWorkflowAgentTarget {
  return {
    providerName: input.providerName ?? "",
    model: input.model ?? "",
    prompt: input.prompt ?? "",
    messages: [...(input.messages ?? [])],
    toolRefs: [...(input.toolRefs ?? [])],
    responseSchema: input.responseSchema === undefined ? undefined : structFromObject(input.responseSchema),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
    timeoutSeconds: input.timeoutSeconds ?? 0,
    outputDelivery: input.outputDelivery === undefined ? undefined : workflowOutputDelivery(input.outputDelivery),
    modelOptions: input.modelOptions === undefined ? undefined : structFromObject(input.modelOptions),
    sessionReadyDelivery: input.sessionReadyDelivery === undefined
      ? undefined
      : workflowOutputDelivery(input.sessionReadyDelivery),
    steps: (input.steps ?? []).map(workflowAgentStep),
  };
}

/** Returns native input copied from a bound agent workflow target. */
export function boundWorkflowAgentTargetInputFromTarget(
  input?: BoundWorkflowAgentTarget,
): BoundWorkflowAgentTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    providerName: input.providerName,
    model: input.model,
    prompt: input.prompt,
    messages: [...(input.messages ?? [])],
    toolRefs: [...(input.toolRefs ?? [])],
    responseSchema: input.responseSchema === undefined ? undefined : jsonObjectClone(input.responseSchema),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
    timeoutSeconds: input.timeoutSeconds,
    outputDelivery: workflowOutputDeliveryInputFromDelivery(input.outputDelivery),
    modelOptions: input.modelOptions === undefined ? undefined : jsonObjectClone(input.modelOptions),
    sessionReadyDelivery: workflowOutputDeliveryInputFromDelivery(input.sessionReadyDelivery),
    steps: (input.steps ?? []).map((step) => workflowAgentStepInputFromStep(step)!),
  };
}

/** Creates one bound workflow agent step from native input. */
export function workflowAgentStep(input: WorkflowAgentStep = {}): WorkflowAgentStep {
  return {
    id: input.id ?? "",
    prompt: input.prompt ?? "",
    messages: [...(input.messages ?? [])],
    toolRefs: [...(input.toolRefs ?? [])],
    responseSchema: input.responseSchema === undefined ? undefined : structFromObject(input.responseSchema),
    modelOptions: input.modelOptions === undefined ? undefined : structFromObject(input.modelOptions),
    timeoutSeconds: input.timeoutSeconds ?? 0,
    outputDelivery: input.outputDelivery === undefined ? undefined : workflowOutputDelivery(input.outputDelivery),
    when: input.when === undefined ? undefined : workflowAgentStepWhen(input.when),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
  };
}

/** Returns native input copied from one bound workflow agent step. */
export function workflowAgentStepInputFromStep(
  input?: WorkflowAgentStep,
): WorkflowAgentStep | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    prompt: input.prompt,
    messages: [...(input.messages ?? [])],
    toolRefs: [...(input.toolRefs ?? [])],
    responseSchema: input.responseSchema === undefined ? undefined : jsonObjectClone(input.responseSchema),
    modelOptions: input.modelOptions === undefined ? undefined : jsonObjectClone(input.modelOptions),
    timeoutSeconds: input.timeoutSeconds,
    outputDelivery: workflowOutputDeliveryInputFromDelivery(input.outputDelivery),
    when: input.when === undefined ? undefined : workflowAgentStepWhenInputFromWhen(input.when),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
  };
}

/** Creates a narrow condition for running one workflow agent step. */
export function workflowAgentStepWhen(input: WorkflowAgentStepWhen = {}): WorkflowAgentStepWhen {
  return {
    stepId: input.stepId ?? "",
    outputPath: input.outputPath ?? "",
    equals: input.equals,
  };
}

/** Returns native input copied from a workflow agent step condition. */
export function workflowAgentStepWhenInputFromWhen(
  input?: WorkflowAgentStepWhen,
): WorkflowAgentStepWhen | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    stepId: input.stepId,
    outputPath: input.outputPath,
    equals: input.equals,
  };
}

/** Creates a bound workflow target from native input. */
export function boundWorkflowTarget(
  input: BoundWorkflowTarget = {},
): BoundWorkflowTarget {
  if ("kind" in input && input.kind !== undefined) {
    return boundWorkflowTargetFromTarget({ kind: input.kind });
  }
  const targetInput = input as BoundWorkflowTarget;
  if (targetInput.plugin !== undefined && targetInput.agent !== undefined) {
    throw new Error("bound workflow target must set either plugin or agent");
  }
  if (targetInput.plugin !== undefined) {
    return { kind: { case: "plugin", value: boundWorkflowPluginTarget(targetInput.plugin) } };
  }
  if (targetInput.agent !== undefined) {
    return { kind: { case: "agent", value: boundWorkflowAgentTarget(targetInput.agent) } };
  }
  return { kind: { case: undefined } };
}

/** Returns native input copied from a bound workflow target. */
export function boundWorkflowTargetInputFromTarget(
  input?: BoundWorkflowTarget,
): BoundWorkflowTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  const kind = input.kind;
  switch (kind?.case) {
    case "plugin":
      return { plugin: boundWorkflowPluginTargetInputFromTarget(kind.value) };
    case "agent":
      return { agent: boundWorkflowAgentTargetInputFromTarget(kind.value) };
    default:
      return {};
  }
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
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
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
    createdBy: workflowActorInputFromActor(input.createdBy),
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
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
    workflowKey: input.workflowKey ?? "",
    providerName: input.providerName ?? "",
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
    createdBy: workflowActorInputFromActor(input.createdBy),
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
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
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
    createdBy: workflowActorInputFromActor(input.createdBy),
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
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
    providerName: input.providerName ?? "",
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
    createdBy: workflowActorInputFromActor(input.createdBy),
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
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
    providerName: input.providerName ?? "",
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
    createdBy: workflowActorInputFromActor(input.createdBy),
  };
}

/** Returns a deep copy of a workflow-provider event trigger. */
export function boundWorkflowEventTriggerFromTrigger(
  input: BoundWorkflowEventTrigger,
): BoundWorkflowEventTrigger {
  return boundWorkflowEventTrigger(boundWorkflowEventTriggerInputFromTrigger(input) ?? {});
}

/** Creates a workflow execution reference from native input. */
export function workflowExecutionReference(
  input: WorkflowExecutionReference = {},
): WorkflowExecutionReference {
  return {
    id: input.id ?? "",
    providerName: input.providerName ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    subjectId: input.subjectId ?? "",
    credentialSubjectId: input.credentialSubjectId ?? "",
    permissions: input.permissions?.map((permission) => workflowAccessPermission(permission)) ?? [],
    createdAt: input.createdAt,
    revokedAt: input.revokedAt,
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
    callerPluginName: input.callerPluginName ?? "",
    runAs: input.runAs === undefined ? undefined : workflowRunAsSubject(input.runAs),
    sourceDefinitionId: input.sourceDefinitionId ?? "",
  };
}

/** Returns native input copied from a workflow execution reference. */
export function workflowExecutionReferenceInputFromReference(
  input?: WorkflowExecutionReference,
): WorkflowExecutionReference | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    ...input,
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    permissions: input.permissions?.map((permission) => workflowAccessPermissionInputFromPermission(permission)) ?? [],
    runAs: workflowRunAsSubjectInputFromSubject(input.runAs),
  };
}

/** Returns a deep copy of a workflow execution reference. */
export function workflowExecutionReferenceFromReference(
  input: WorkflowExecutionReference,
): WorkflowExecutionReference {
  return workflowExecutionReference(workflowExecutionReferenceInputFromReference(input) ?? {});
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
  /** Store or update an execution reference for a workflow target. */
  putExecutionReference?: (
    request: PutWorkflowExecutionReferenceRequest,
  ) => MaybePromise<WorkflowExecutionReference>;
  /** Load one execution reference by provider-owned lookup fields. */
  getExecutionReference?: (
    request: GetWorkflowExecutionReferenceRequest,
  ) => MaybePromise<WorkflowExecutionReference>;
  /** List execution references for the requested scope. */
  listExecutionReferences?: (
    request: ListWorkflowExecutionReferencesRequest,
  ) => MaybePromise<readonly WorkflowExecutionReference[]>;
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
  private readonly putExecutionReferenceHandler: WorkflowProviderOptions["putExecutionReference"];
  private readonly getExecutionReferenceHandler: WorkflowProviderOptions["getExecutionReference"];
  private readonly listExecutionReferencesHandler: WorkflowProviderOptions["listExecutionReferences"];
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
    this.putExecutionReferenceHandler = options.putExecutionReference;
    this.getExecutionReferenceHandler = options.getExecutionReference;
    this.listExecutionReferencesHandler = options.listExecutionReferences;
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

  /** Store or update an execution reference for a workflow target. */
  async putExecutionReference(
    request: PutWorkflowExecutionReferenceRequest,
  ): Promise<WorkflowExecutionReference> {
    return await requireWorkflowProviderHandler(
      "put execution reference",
      this.putExecutionReferenceHandler,
      request,
    );
  }

  /** Load one execution reference by provider-owned lookup fields. */
  async getExecutionReference(
    request: GetWorkflowExecutionReferenceRequest,
  ): Promise<WorkflowExecutionReference> {
    return await requireWorkflowProviderHandler(
      "get execution reference",
      this.getExecutionReferenceHandler,
      request,
    );
  }

  /** List execution references for the requested scope. */
  async listExecutionReferences(
    request: ListWorkflowExecutionReferencesRequest,
  ): Promise<readonly WorkflowExecutionReference[]> {
    return await requireWorkflowProviderHandler(
      "list execution references",
      this.listExecutionReferencesHandler,
      request,
    );
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

/** Client for invoking operations from workflow provider code. */
export class WorkflowHost {
  private readonly client: Client<typeof WorkflowHostService>;

  constructor() {
    const target = process.env[ENV_HOST_SERVICE_SOCKET]?.trim();
    if (!target) {
      throw new Error(`workflow host: ${ENV_HOST_SERVICE_SOCKET} is not set`);
    }
    const relayToken = process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";
    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("workflow host", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(WorkflowHostService, transport);
  }

  /** Invokes an operation through the workflow host service. */
  async invokeOperation(
    input: InvokeWorkflowOperationInput,
  ): Promise<InvokeWorkflowOperationResponse> {
    const response = await this.client.invokeOperation(invokeWorkflowOperationRequestToProto(input));
    return { status: response.status, body: response.body };
  }
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
    async putExecutionReference(request) {
      return create(
        WorkflowExecutionReferenceSchema,
        workflowExecutionReferenceToProto(
          await invokeWorkflowProvider("put execution reference", () =>
            provider.putExecutionReference(putWorkflowExecutionReferenceRequestFromProto(request)),
          ),
        ),
      );
    },
    async getExecutionReference(request) {
      return create(
        WorkflowExecutionReferenceSchema,
        workflowExecutionReferenceToProto(
          await invokeWorkflowProvider("get execution reference", () =>
            provider.getExecutionReference(getWorkflowExecutionReferenceRequestFromProto(request)),
          ),
        ),
      );
    },
    async listExecutionReferences(request) {
      return create(ListWorkflowExecutionReferencesResponseSchema, {
        references: (
          await invokeWorkflowProvider("list execution references", () =>
            provider.listExecutionReferences(listWorkflowExecutionReferencesRequestFromProto(request)),
          )
        ).map(workflowExecutionReferenceToProto),
      });
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

export function workflowActorToProto(input?: WorkflowActor | undefined): ProtoWorkflowActor | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowActorSchema, {
    subjectId: input.subjectId ?? "",
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
  });
}

export function workflowActorFromProto(input?: ProtoWorkflowActor | undefined): WorkflowActor | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    subjectId: input.subjectId,
    subjectKind: input.subjectKind,
    displayName: input.displayName,
    authSource: input.authSource,
  };
}

export function workflowRunAsSubjectToProto(
  input?: WorkflowRunAsSubject | undefined,
): ProtoWorkflowRunAsSubject | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowRunAsSubjectSchema, {
    subjectId: input.subjectId ?? "",
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
  });
}

export function workflowRunAsSubjectFromProto(
  input?: ProtoWorkflowRunAsSubject | undefined,
): WorkflowRunAsSubject | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    subjectId: input.subjectId,
    subjectKind: input.subjectKind,
    displayName: input.displayName,
    authSource: input.authSource,
  };
}

export function workflowAccessPermissionToProto(input: WorkflowAccessPermission) {
  return create(WorkflowAccessPermissionSchema, {
    plugin: input.plugin ?? "",
    operations: [...(input.operations ?? [])],
  });
}

export function workflowAccessPermissionFromProto(
  input: ProtoWorkflowAccessPermission,
): WorkflowAccessPermission {
  return {
    plugin: input.plugin,
    operations: [...input.operations],
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

export function workflowOutputValueSourceToProto(
  input?: WorkflowOutputValueSource | undefined,
): ProtoWorkflowOutputValueSource | undefined {
  if (input === undefined) {
    return undefined;
  }
  const source = workflowOutputValueSource(input);
  const kind = source.kind;
  switch (kind?.case) {
    case "agentOutput":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "agentOutput", value: kind.value },
      });
    case "signalPayload":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "signalPayload", value: kind.value },
      });
    case "signalMetadata":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "signalMetadata", value: kind.value },
      });
    case "agentSession":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "agentSession", value: kind.value },
      });
    case "literal":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "literal", value: valueFromJson(kind.value) },
      });
    default:
      return create(WorkflowOutputValueSourceSchema);
  }
}

export function workflowOutputValueSourceFromProto(
  input?: ProtoWorkflowOutputValueSource | undefined,
): WorkflowOutputValueSource | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "agentOutput":
      return { kind: { case: "agentOutput", value: input.kind.value } };
    case "signalPayload":
      return { kind: { case: "signalPayload", value: input.kind.value } };
    case "signalMetadata":
      return { kind: { case: "signalMetadata", value: input.kind.value } };
    case "agentSession":
      return { kind: { case: "agentSession", value: input.kind.value } };
    case "literal":
      return { kind: { case: "literal", value: jsonFromValue(input.kind.value) as JsonInput } };
    default:
      return { kind: { case: undefined } };
  }
}

export function workflowOutputBindingToProto(input: WorkflowOutputBinding) {
  return create(WorkflowOutputBindingSchema, {
    inputField: input.inputField ?? "",
    value: workflowOutputValueSourceToProto(input.value),
  });
}

export function workflowOutputBindingFromProto(
  input: ProtoWorkflowOutputBinding,
): WorkflowOutputBinding {
  return {
    inputField: input.inputField,
    value: workflowOutputValueSourceFromProto(input.value),
  };
}

export function workflowOutputDeliveryToProto(
  input?: WorkflowOutputDelivery | undefined,
): ProtoWorkflowOutputDelivery | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowOutputDeliverySchema, {
    target: boundWorkflowPluginTargetToProto(input.target),
    inputBindings: input.inputBindings?.map(workflowOutputBindingToProto) ?? [],
    credentialMode: input.credentialMode ?? "",
  });
}

export function workflowOutputDeliveryFromProto(
  input?: ProtoWorkflowOutputDelivery | undefined,
): WorkflowOutputDelivery | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    target: boundWorkflowPluginTargetFromProto(input.target),
    inputBindings: input.inputBindings.map(workflowOutputBindingFromProto),
    credentialMode: input.credentialMode,
  };
}

export function boundWorkflowPluginTargetToProto(
  input?: BoundWorkflowPluginTarget | undefined,
): ProtoBoundWorkflowPluginTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(BoundWorkflowPluginTargetSchema, {
    pluginName: input.pluginName ?? "",
    operation: input.operation ?? "",
    input: optionalStruct(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  });
}

export function boundWorkflowPluginTargetFromProto(
  input?: ProtoBoundWorkflowPluginTarget | undefined,
): BoundWorkflowPluginTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    pluginName: input.pluginName,
    operation: input.operation,
    input: optionalObjectFromStruct(input.input),
    connection: input.connection,
    instance: input.instance,
    credentialMode: input.credentialMode,
  };
}

export function boundWorkflowAgentTargetToProto(
  input?: BoundWorkflowAgentTarget | undefined,
): ProtoBoundWorkflowAgentTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(BoundWorkflowAgentTargetSchema, {
    providerName: input.providerName ?? "",
    model: input.model ?? "",
    prompt: input.prompt ?? "",
    messages: input.messages?.map(agentMessageToProto) ?? [],
    toolRefs: input.toolRefs?.map(agentToolRefToProto) ?? [],
    responseSchema: optionalStruct(input.responseSchema),
    metadata: optionalStruct(input.metadata),
    timeoutSeconds: input.timeoutSeconds ?? 0,
    outputDelivery: workflowOutputDeliveryToProto(input.outputDelivery),
    modelOptions: optionalStruct(input.modelOptions),
    sessionReadyDelivery: workflowOutputDeliveryToProto(input.sessionReadyDelivery),
    steps: input.steps?.map(workflowAgentStepToProto) ?? [],
  });
}

export function boundWorkflowAgentTargetFromProto(
  input?: ProtoBoundWorkflowAgentTarget | undefined,
): BoundWorkflowAgentTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    providerName: input.providerName,
    model: input.model,
    prompt: input.prompt,
    messages: input.messages.map(agentMessageFromProto),
    toolRefs: input.toolRefs.map(agentToolRefFromProto),
    responseSchema: optionalObjectFromStruct(input.responseSchema),
    metadata: optionalObjectFromStruct(input.metadata),
    timeoutSeconds: input.timeoutSeconds,
    outputDelivery: workflowOutputDeliveryFromProto(input.outputDelivery),
    modelOptions: optionalObjectFromStruct(input.modelOptions),
    sessionReadyDelivery: workflowOutputDeliveryFromProto(input.sessionReadyDelivery),
    steps: input.steps.map(workflowAgentStepFromProto),
  };
}

export function workflowAgentStepToProto(input: WorkflowAgentStep): ProtoWorkflowAgentStep {
  return create(WorkflowAgentStepSchema, {
    id: input.id ?? "",
    prompt: input.prompt ?? "",
    messages: input.messages?.map(agentMessageToProto) ?? [],
    toolRefs: input.toolRefs?.map(agentToolRefToProto) ?? [],
    responseSchema: optionalStruct(input.responseSchema),
    modelOptions: optionalStruct(input.modelOptions),
    timeoutSeconds: input.timeoutSeconds ?? 0,
    outputDelivery: workflowOutputDeliveryToProto(input.outputDelivery),
    when: workflowAgentStepWhenToProto(input.when),
    metadata: optionalStruct(input.metadata),
  });
}

export function workflowAgentStepFromProto(input: ProtoWorkflowAgentStep): WorkflowAgentStep {
  return {
    id: input.id,
    prompt: input.prompt,
    messages: input.messages.map(agentMessageFromProto),
    toolRefs: input.toolRefs.map(agentToolRefFromProto),
    responseSchema: optionalObjectFromStruct(input.responseSchema),
    modelOptions: optionalObjectFromStruct(input.modelOptions),
    timeoutSeconds: input.timeoutSeconds,
    outputDelivery: workflowOutputDeliveryFromProto(input.outputDelivery),
    when: workflowAgentStepWhenFromProto(input.when),
    metadata: optionalObjectFromStruct(input.metadata),
  };
}

export function workflowAgentStepWhenToProto(
  input?: WorkflowAgentStepWhen | undefined,
): ProtoWorkflowAgentStepWhen | undefined {
  if (input === undefined) {
    return undefined;
  }
  return create(WorkflowAgentStepWhenSchema, {
    stepId: input.stepId ?? "",
    outputPath: input.outputPath ?? "",
    equals: input.equals === undefined ? undefined : valueFromJson(input.equals),
  });
}

export function workflowAgentStepWhenFromProto(
  input?: ProtoWorkflowAgentStepWhen | undefined,
): WorkflowAgentStepWhen | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    stepId: input.stepId,
    outputPath: input.outputPath,
    equals: input.equals === undefined ? undefined : jsonFromValue(input.equals),
  };
}

export function boundWorkflowTargetToProto(
  input?: BoundWorkflowTarget | undefined,
): ProtoBoundWorkflowTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  const target = boundWorkflowTarget(input);
  const kind = target.kind;
  switch (kind?.case) {
    case "plugin":
      return create(BoundWorkflowTargetSchema, {
        kind: { case: "plugin", value: boundWorkflowPluginTargetToProto(kind.value)! },
      });
    case "agent":
      return create(BoundWorkflowTargetSchema, {
        kind: { case: "agent", value: boundWorkflowAgentTargetToProto(kind.value)! },
      });
    default:
      return create(BoundWorkflowTargetSchema);
  }
}

export function boundWorkflowTargetFromProto(
  input?: ProtoBoundWorkflowTarget | undefined,
): BoundWorkflowTarget | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "plugin":
      return { kind: { case: "plugin", value: boundWorkflowPluginTargetFromProto(input.kind.value)! } };
    case "agent":
      return { kind: { case: "agent", value: boundWorkflowAgentTargetFromProto(input.kind.value)! } };
    default:
      return { kind: { case: undefined } };
  }
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
    createdBy: workflowActorToProto(input.createdBy),
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
    createdBy: workflowActorFromProto(input.createdBy),
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
    createdBy: workflowActorToProto(input.createdBy),
    executionRef: input.executionRef ?? "",
    workflowKey: input.workflowKey ?? "",
    providerName: input.providerName ?? "",
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
    createdBy: workflowActorFromProto(input.createdBy),
    executionRef: input.executionRef,
    workflowKey: input.workflowKey,
    providerName: input.providerName,
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
    createdBy: workflowActorToProto(input.createdBy),
    executionRef: input.executionRef ?? "",
    providerName: input.providerName ?? "",
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
    createdBy: workflowActorFromProto(input.createdBy),
    executionRef: input.executionRef,
    providerName: input.providerName,
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
    createdBy: workflowActorToProto(input.createdBy),
    executionRef: input.executionRef ?? "",
    providerName: input.providerName ?? "",
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
    createdBy: workflowActorFromProto(input.createdBy),
    executionRef: input.executionRef,
    providerName: input.providerName,
  };
}

export function boundWorkflowDefinitionToProto(input: BoundWorkflowDefinition) {
  return create(BoundWorkflowDefinitionSchema, {
    id: input.id ?? "",
    target: boundWorkflowTargetToProto(input.target),
    createdBy: workflowActorToProto(input.createdBy),
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
    createdBy: workflowActorFromProto(input.createdBy),
    createdAt: optionalDate(input.createdAt),
    providerName: input.providerName,
  };
}

export function workflowExecutionReferenceToProto(
  input: WorkflowExecutionReference,
): ProtoWorkflowExecutionReference {
  return create(WorkflowExecutionReferenceSchema, {
    id: input.id ?? "",
    providerName: input.providerName ?? "",
    target: boundWorkflowTargetToProto(input.target),
    subjectId: input.subjectId ?? "",
    credentialSubjectId: input.credentialSubjectId ?? "",
    permissions: input.permissions?.map(workflowAccessPermissionToProto) ?? [],
    createdAt: optionalTimestamp(input.createdAt),
    revokedAt: optionalTimestamp(input.revokedAt),
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
    callerPluginName: input.callerPluginName ?? "",
    runAs: workflowRunAsSubjectToProto(input.runAs),
    sourceDefinitionId: input.sourceDefinitionId ?? "",
  });
}

export function workflowExecutionReferenceFromProto(
  input?: ProtoWorkflowExecutionReference | undefined,
): WorkflowExecutionReference | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    providerName: input.providerName,
    target: boundWorkflowTargetFromProto(input.target),
    subjectId: input.subjectId,
    credentialSubjectId: input.credentialSubjectId,
    permissions: input.permissions.map(workflowAccessPermissionFromProto),
    createdAt: optionalDate(input.createdAt),
    revokedAt: optionalDate(input.revokedAt),
    subjectKind: input.subjectKind,
    displayName: input.displayName,
    authSource: input.authSource,
    callerPluginName: input.callerPluginName,
    runAs: workflowRunAsSubjectFromProto(input.runAs),
    sourceDefinitionId: input.sourceDefinitionId,
  };
}

function invokeWorkflowOperationRequestToProto(input: InvokeWorkflowOperationInput) {
  return create(InvokeWorkflowOperationRequestSchema, {
    target: boundWorkflowTargetToProto(input.target),
    runId: input.runId ?? "",
    trigger: workflowRunTriggerToProto(input.trigger),
    input: optionalStruct(input.input),
    metadata: optionalStruct(input.metadata),
    createdBy: workflowActorToProto(input.createdBy),
    executionRef: input.executionRef ?? "",
    signals: input.signals?.map((signal) => workflowSignalToProto(signal)!) ?? [],
  });
}

function startWorkflowProviderRunRequestFromProto(
  input: ProtoStartWorkflowProviderRunRequest,
): StartWorkflowProviderRunRequest {
  return {
    target: boundWorkflowTargetFromProto(input.target),
    idempotencyKey: input.idempotencyKey,
    createdBy: workflowActorFromProto(input.createdBy),
    executionRef: input.executionRef,
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
    targetPlugin: input.targetPlugin,
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
    createdBy: workflowActorFromProto(input.createdBy),
    executionRef: input.executionRef,
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
    requestedBy: workflowActorFromProto(input.requestedBy),
    executionRef: input.executionRef,
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
    requestedBy: workflowActorFromProto(input.requestedBy),
    executionRef: input.executionRef,
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

function putWorkflowExecutionReferenceRequestFromProto(
  input: ProtoPutWorkflowExecutionReferenceRequest,
): PutWorkflowExecutionReferenceRequest {
  return { reference: workflowExecutionReferenceFromProto(input.reference) };
}

function getWorkflowExecutionReferenceRequestFromProto(
  input: ProtoGetWorkflowExecutionReferenceRequest,
): GetWorkflowExecutionReferenceRequest {
  return { id: input.id };
}

function listWorkflowExecutionReferencesRequestFromProto(
  input: ProtoListWorkflowExecutionReferencesRequest,
): ListWorkflowExecutionReferencesRequest {
  return { subjectId: input.subjectId };
}

function publishWorkflowProviderEventRequestFromProto(
  input: ProtoPublishWorkflowProviderEventRequest,
): PublishWorkflowProviderEventRequest {
  return {
    pluginName: input.pluginName,
    event: workflowEventFromProto(input.event),
    publishedBy: workflowActorFromProto(input.publishedBy),
  };
}

export function workflowManagerScheduleFromProto(
  input: ProtoBoundWorkflowSchedule,
): WorkflowManagerSchedule {
  return {
    providerName: input.providerName,
    schedule: boundWorkflowScheduleFromProto(input),
  };
}

export function workflowManagerEventTriggerFromProto(
  input: ProtoBoundWorkflowEventTrigger,
): WorkflowManagerEventTrigger {
  return {
    providerName: input.providerName,
    trigger: boundWorkflowEventTriggerFromProto(input),
  };
}

export function workflowManagerDefinitionFromProto(
  input: ProtoBoundWorkflowDefinition,
): WorkflowManagerDefinition {
  return {
    providerName: input.providerName,
    definition: boundWorkflowDefinitionFromProto(input),
  };
}

export function workflowManagerRunFromProto(
  input: ProtoBoundWorkflowRun,
): WorkflowManagerRun {
  return {
    providerName: input.providerName,
    run: boundWorkflowRunFromProto(input),
  };
}

export function workflowManagerRunSignalFromProto(
  input: ProtoSignalWorkflowRunResponse,
): WorkflowManagerRunSignal {
  return {
    providerName: input.run?.providerName,
    run: boundWorkflowRunFromProto(input.run),
    signal: workflowSignalFromProto(input.signal),
    startedRun: input.startedRun,
    workflowKey: input.workflowKey,
  };
}

function cloneWorkflowOutputValueSourceKind(
  kind: WorkflowOutputValueSourceKind,
): WorkflowOutputValueSourceKind {
  switch (kind.case) {
    case "literal":
      return { case: "literal", value: kind.value };
    case "agentOutput":
    case "signalPayload":
    case "signalMetadata":
    case "agentSession":
      return { case: kind.case, value: kind.value };
    default:
      return { case: undefined };
  }
}

function valueMapInput(input?: Record<string, JsonInput>): Record<string, JsonInput> {
  return input === undefined ? {} : { ...input };
}

function jsonObjectClone(input: JsonObjectInput): JsonObjectInput {
  return structFromObject(jsonObjectFromStruct(input as JsonObject));
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

async function requireWorkflowProviderHandler<Request, Response>(
  action: string,
  fn: ((request: Request) => MaybePromise<Response>) | undefined,
  request: Request,
): Promise<Response> {
  if (!fn) {
    throw new ConnectError(
      `workflow provider ${action} is not implemented`,
      Code.Unimplemented,
    );
  }
  return await fn(request);
}
