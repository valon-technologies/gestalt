import {
  create,
  type JsonObject,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import {
  EmptySchema,
  TimestampSchema,
  ValueSchema,
  type Timestamp,
  type Value,
} from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createClient,
  type Client,
  type Interceptor,
  type ServiceImpl,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  BoundWorkflowAgentTargetSchema,
  BoundWorkflowDefinitionSchema,
  BoundWorkflowEventTriggerSchema,
  BoundWorkflowPluginTargetSchema,
  BoundWorkflowRunSchema,
  BoundWorkflowScheduleSchema,
  BoundWorkflowTargetSchema,
  ListWorkflowExecutionReferencesResponseSchema,
  ListWorkflowProviderEventTriggersResponseSchema,
  ListWorkflowProviderRunsResponseSchema,
  ListWorkflowProviderSchedulesResponseSchema,
  WorkflowAccessPermissionSchema,
  WorkflowActorSchema,
  WorkflowEventMatchSchema,
  WorkflowEventSchema,
  WorkflowEventTriggerInvocationSchema,
  WorkflowExecutionReferenceSchema,
  WorkflowHost as WorkflowHostService,
  InvokeWorkflowOperationRequestSchema,
  WorkflowManualTriggerSchema,
  WorkflowOutputBindingSchema,
  WorkflowOutputDeliverySchema,
  WorkflowOutputValueSourceSchema,
  WorkflowProvider as WorkflowProviderService,
  WorkflowRunAsSubjectSchema,
  WorkflowRunTriggerSchema,
  WorkflowScheduleTriggerSchema,
  WorkflowSignalSchema,
  type BoundWorkflowAgentTarget,
  type BoundWorkflowDefinition,
  type BoundWorkflowEventTrigger,
  type BoundWorkflowPluginTarget,
  type BoundWorkflowRun,
  type BoundWorkflowSchedule,
  type BoundWorkflowTarget,
  type CancelWorkflowProviderRunRequest,
  type DeleteWorkflowProviderEventTriggerRequest,
  type DeleteWorkflowProviderScheduleRequest,
  type GetWorkflowExecutionReferenceRequest,
  type GetWorkflowProviderEventTriggerRequest,
  type GetWorkflowProviderRunRequest,
  type GetWorkflowProviderScheduleRequest,
  type ListWorkflowExecutionReferencesResponse,
  type ListWorkflowExecutionReferencesRequest,
  type ListWorkflowProviderEventTriggersRequest,
  type ListWorkflowProviderRunsRequest,
  type ListWorkflowProviderSchedulesRequest,
  type PauseWorkflowProviderEventTriggerRequest,
  type PauseWorkflowProviderScheduleRequest,
  type PublishWorkflowProviderEventRequest,
  type PutWorkflowExecutionReferenceRequest,
  type ResumeWorkflowProviderEventTriggerRequest,
  type ResumeWorkflowProviderScheduleRequest,
  type StartWorkflowProviderRunRequest,
  type UpsertWorkflowProviderEventTriggerRequest,
  type UpsertWorkflowProviderScheduleRequest,
  type WorkflowAccessPermission,
  type WorkflowActor,
  type WorkflowEvent,
  type WorkflowEventMatch,
  type WorkflowEventTriggerInvocation,
  type WorkflowExecutionReference,
  type WorkflowOutputBinding,
  type WorkflowOutputDelivery,
  type WorkflowOutputValueSource,
  type WorkflowRunAsSubject,
  type WorkflowRunTrigger,
  type WorkflowScheduleTrigger,
  type WorkflowSignal,
} from "./internal/gen/v1/workflow_pb.ts";
import {
  AgentMessageSchema,
  AgentToolRefSchema,
  type AgentMessage,
  type AgentToolRef,
} from "./internal/gen/v1/agent_pb.ts";
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

/** Environment variable containing the workflow-host service target. */
export const ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET";
/** Environment variable containing the optional workflow-host relay token. */
export const ENV_WORKFLOW_HOST_SOCKET_TOKEN = `${ENV_WORKFLOW_HOST_SOCKET}_TOKEN`;
const WORKFLOW_HOST_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

/**
 * Generated workflow protocol message types commonly used by providers.
 *
 * These are re-exported so workflow provider code can type runs, schedules,
 * triggers, and operation-invocation requests without importing from `gen`.
 */
export type {
  BoundWorkflowAgentTarget,
  BoundWorkflowDefinition,
  BoundWorkflowEventTrigger,
  BoundWorkflowPluginTarget,
  BoundWorkflowRun,
  BoundWorkflowSchedule,
  BoundWorkflowTarget,
  CancelWorkflowProviderRunRequest,
  DeleteWorkflowProviderEventTriggerRequest,
  DeleteWorkflowProviderScheduleRequest,
  GetWorkflowExecutionReferenceRequest,
  GetWorkflowProviderEventTriggerRequest,
  GetWorkflowProviderRunRequest,
  GetWorkflowProviderScheduleRequest,
  ListWorkflowExecutionReferencesResponse,
  ListWorkflowExecutionReferencesRequest,
  ListWorkflowProviderEventTriggersRequest,
  ListWorkflowProviderRunsRequest,
  ListWorkflowProviderSchedulesRequest,
  PauseWorkflowProviderEventTriggerRequest,
  PauseWorkflowProviderScheduleRequest,
  PublishWorkflowProviderEventRequest,
  PutWorkflowExecutionReferenceRequest,
  ResumeWorkflowProviderEventTriggerRequest,
  ResumeWorkflowProviderScheduleRequest,
  StartWorkflowProviderRunRequest,
  UpsertWorkflowProviderEventTriggerRequest,
  UpsertWorkflowProviderScheduleRequest,
  WorkflowAccessPermission,
  WorkflowActor,
  WorkflowEvent,
  WorkflowEventMatch,
  WorkflowExecutionReference,
  WorkflowOutputBinding,
  WorkflowOutputDelivery,
  WorkflowOutputValueSource,
  WorkflowRunAsSubject,
  WorkflowRunTrigger,
  WorkflowScheduleTrigger,
  WorkflowSignal,
};

/** Native workflow run-status constants. */
export const WorkflowRunStatus = {
  UNSPECIFIED: 0,
  PENDING: 1,
  RUNNING: 2,
  SUCCEEDED: 3,
  FAILED: 4,
  CANCELED: 5,
} as const;
export type WorkflowRunStatus =
  (typeof WorkflowRunStatus)[keyof typeof WorkflowRunStatus];

type TimestampInput = Date | Timestamp;
type AgentMessageInput = AgentMessage | MessageInitShape<typeof AgentMessageSchema>;
type AgentToolRefInput = AgentToolRef | MessageInitShape<typeof AgentToolRefSchema>;

/** Native input for a bound plugin workflow target. */
export interface BoundWorkflowPluginTargetInput {
  pluginName?: string | undefined;
  operation?: string | undefined;
  input?: JsonObjectInput | undefined;
  connection?: string | undefined;
  instance?: string | undefined;
  credentialMode?: string | undefined;
}

/** Native input for a workflow output value source. */
export interface WorkflowOutputValueSourceInput {
  agentOutput?: string | undefined;
  signalPayload?: string | undefined;
  signalMetadata?: string | undefined;
  literal?: JsonInput | Value | undefined;
  agentSession?: string | undefined;
}

/** Native input for one workflow output binding. */
export interface WorkflowOutputBindingInput {
  inputField?: string | undefined;
  value?: WorkflowOutputValueSourceInput | WorkflowOutputValueSource | undefined;
}

/** Native input for a workflow output delivery. */
export interface WorkflowOutputDeliveryInput {
  target?: BoundWorkflowPluginTargetInput | BoundWorkflowPluginTarget | undefined;
  inputBindings?: readonly (WorkflowOutputBindingInput | WorkflowOutputBinding)[] | undefined;
  credentialMode?: string | undefined;
}

/** Native input for a bound agent workflow target. */
export interface BoundWorkflowAgentTargetInput {
  providerName?: string | undefined;
  model?: string | undefined;
  prompt?: string | undefined;
  messages?: readonly AgentMessageInput[] | undefined;
  toolRefs?: readonly AgentToolRefInput[] | undefined;
  responseSchema?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  timeoutSeconds?: number | undefined;
  outputDelivery?: WorkflowOutputDeliveryInput | WorkflowOutputDelivery | undefined;
  modelOptions?: JsonObjectInput | undefined;
  sessionReadyDelivery?: WorkflowOutputDeliveryInput | WorkflowOutputDelivery | undefined;
}

/** Native input for a bound workflow target. */
export interface BoundWorkflowTargetInput {
  plugin?: BoundWorkflowPluginTargetInput | BoundWorkflowPluginTarget | undefined;
  agent?: BoundWorkflowAgentTargetInput | BoundWorkflowAgentTarget | undefined;
}

/** Native input for workflow actor metadata. */
export interface WorkflowActorInput {
  subjectId?: string | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
}

/** Native input for workflow run-as metadata. */
export interface WorkflowRunAsSubjectInput {
  subjectId?: string | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
}

/** Native input for an execution-reference permission. */
export interface WorkflowAccessPermissionInput {
  plugin?: string | undefined;
  operations?: readonly string[] | undefined;
}

/** Native input for a workflow event. */
export interface WorkflowEventInput {
  id?: string | undefined;
  source?: string | undefined;
  specVersion?: string | undefined;
  type?: string | undefined;
  subject?: string | undefined;
  time?: TimestampInput | undefined;
  datacontenttype?: string | undefined;
  data?: JsonObjectInput | undefined;
  extensions?: Record<string, JsonInput | Value> | undefined;
}

/** Native input for workflow event matching fields. */
export interface WorkflowEventMatchInput {
  type?: string | undefined;
  source?: string | undefined;
  subject?: string | undefined;
}

/** Native input for a workflow signal. */
export interface WorkflowSignalInput {
  id?: string | undefined;
  name?: string | undefined;
  payload?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  createdBy?: WorkflowActorInput | WorkflowActor | undefined;
  createdAt?: TimestampInput | undefined;
  idempotencyKey?: string | undefined;
  sequence?: bigint | number | undefined;
}

/** Native input for a schedule-triggered workflow run. */
export interface WorkflowScheduleTriggerInput {
  scheduleId?: string | undefined;
  scheduledFor?: TimestampInput | undefined;
}

/** Native input for an event-triggered workflow run. */
export interface WorkflowEventTriggerInvocationInput {
  triggerId?: string | undefined;
  event?: WorkflowEventInput | WorkflowEvent | undefined;
}

/** Native input for a workflow run trigger. */
export interface WorkflowRunTriggerInput {
  manual?: boolean | undefined;
  schedule?: WorkflowScheduleTriggerInput | WorkflowScheduleTrigger | undefined;
  event?: WorkflowEventTriggerInvocationInput | WorkflowEventTriggerInvocation | undefined;
}

/** Native input for a workflow-provider run. */
export interface BoundWorkflowRunInput {
  id?: string | undefined;
  status?: WorkflowRunStatus | undefined;
  target?: BoundWorkflowTargetInput | BoundWorkflowTarget | undefined;
  trigger?: WorkflowRunTriggerInput | WorkflowRunTrigger | undefined;
  createdAt?: TimestampInput | undefined;
  startedAt?: TimestampInput | undefined;
  completedAt?: TimestampInput | undefined;
  statusMessage?: string | undefined;
  resultBody?: string | undefined;
  createdBy?: WorkflowActorInput | WorkflowActor | undefined;
  executionRef?: string | undefined;
  workflowKey?: string | undefined;
}

/** Native input copied from a workflow-provider definition. */
export interface BoundWorkflowDefinitionInput {
  id?: string | undefined;
  target?: BoundWorkflowTargetInput | BoundWorkflowTarget | undefined;
  createdBy?: WorkflowActorInput | WorkflowActor | undefined;
  createdAt?: TimestampInput | undefined;
}

/** Native input for a workflow-provider schedule. */
export interface BoundWorkflowScheduleInput {
  id?: string | undefined;
  cron?: string | undefined;
  timezone?: string | undefined;
  target?: BoundWorkflowTargetInput | BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  createdAt?: TimestampInput | undefined;
  updatedAt?: TimestampInput | undefined;
  nextRunAt?: TimestampInput | undefined;
  createdBy?: WorkflowActorInput | WorkflowActor | undefined;
  executionRef?: string | undefined;
}

/** Native input for a workflow-provider event trigger. */
export interface BoundWorkflowEventTriggerInput {
  id?: string | undefined;
  match?: WorkflowEventMatchInput | WorkflowEventMatch | undefined;
  target?: BoundWorkflowTargetInput | BoundWorkflowTarget | undefined;
  paused?: boolean | undefined;
  createdAt?: TimestampInput | undefined;
  updatedAt?: TimestampInput | undefined;
  createdBy?: WorkflowActorInput | WorkflowActor | undefined;
  executionRef?: string | undefined;
}

/** Native input for a workflow execution reference. */
export interface WorkflowExecutionReferenceInput {
  id?: string | undefined;
  providerName?: string | undefined;
  target?: BoundWorkflowTargetInput | BoundWorkflowTarget | undefined;
  subjectId?: string | undefined;
  credentialSubjectId?: string | undefined;
  permissions?: readonly (WorkflowAccessPermissionInput | WorkflowAccessPermission)[] | undefined;
  createdAt?: TimestampInput | undefined;
  revokedAt?: TimestampInput | undefined;
  subjectKind?: string | undefined;
  displayName?: string | undefined;
  authSource?: string | undefined;
  callerPluginName?: string | undefined;
  runAs?: WorkflowRunAsSubjectInput | WorkflowRunAsSubject | undefined;
  sourceDefinitionId?: string | undefined;
}

/** Native input for invoking a workflow operation through the host service. */
export interface InvokeWorkflowOperationInput {
  target?: BoundWorkflowTargetInput | BoundWorkflowTarget | undefined;
  runId?: string | undefined;
  trigger?: WorkflowRunTriggerInput | WorkflowRunTrigger | undefined;
  input?: JsonObjectInput | undefined;
  metadata?: JsonObjectInput | undefined;
  createdBy?: WorkflowActorInput | WorkflowActor | undefined;
  executionRef?: string | undefined;
  signals?: readonly (WorkflowSignalInput | WorkflowSignal)[] | undefined;
}

/** Native response returned after invoking a workflow operation. */
export interface InvokeWorkflowOperationResponse {
  status: number;
  body: string;
}

/** Creates workflow actor metadata from native input. */
export function workflowActor(input: WorkflowActorInput | WorkflowActor = {}): WorkflowActor {
  return create(WorkflowActorSchema, {
    subjectId: input.subjectId ?? "",
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
  });
}

/** Returns native input copied from workflow actor metadata. */
export function workflowActorInputFromActor(input?: WorkflowActor): WorkflowActorInput | undefined {
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

/** Creates workflow run-as metadata from native input. */
export function workflowRunAsSubject(
  input: WorkflowRunAsSubjectInput | WorkflowRunAsSubject = {},
): WorkflowRunAsSubject {
  return create(WorkflowRunAsSubjectSchema, {
    subjectId: input.subjectId ?? "",
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
  });
}

/** Returns native input copied from workflow run-as metadata. */
export function workflowRunAsSubjectInputFromSubject(
  input?: WorkflowRunAsSubject,
): WorkflowRunAsSubjectInput | undefined {
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

/** Creates an execution-reference permission from native input. */
export function workflowAccessPermission(
  input: WorkflowAccessPermissionInput | WorkflowAccessPermission = {},
): WorkflowAccessPermission {
  return create(WorkflowAccessPermissionSchema, {
    plugin: input.plugin ?? "",
    operations: [...(input.operations ?? [])],
  });
}

/** Returns native input copied from an execution-reference permission. */
export function workflowAccessPermissionInputFromPermission(
  input: WorkflowAccessPermission,
): WorkflowAccessPermissionInput {
  return {
    plugin: input.plugin,
    operations: [...input.operations],
  };
}

/** Creates workflow event-match fields from native input. */
export function workflowEventMatch(
  input: WorkflowEventMatchInput | WorkflowEventMatch = {},
): WorkflowEventMatch {
  return create(WorkflowEventMatchSchema, {
    type: input.type ?? "",
    source: input.source ?? "",
    subject: input.subject ?? "",
  });
}

/** Returns native input copied from workflow event-match fields. */
export function workflowEventMatchInputFromMatch(
  input?: WorkflowEventMatch,
): WorkflowEventMatchInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    type: input.type,
    source: input.source,
    subject: input.subject,
  };
}

/** Creates a workflow output value source from native input. */
export function workflowOutputValueSource(
  input: WorkflowOutputValueSourceInput | WorkflowOutputValueSource = {},
): WorkflowOutputValueSource {
  if ("kind" in input) {
    return create(WorkflowOutputValueSourceSchema, input);
  }
  const selected: string[] = [];
  if (input.agentOutput !== undefined) {
    selected.push("agentOutput");
  }
  if (input.signalPayload !== undefined) {
    selected.push("signalPayload");
  }
  if (input.signalMetadata !== undefined) {
    selected.push("signalMetadata");
  }
  if (Object.prototype.hasOwnProperty.call(input, "literal")) {
    selected.push("literal");
  }
  if (input.agentSession !== undefined) {
    selected.push("agentSession");
  }
  if (selected.length === 0) {
    return create(WorkflowOutputValueSourceSchema);
  }
  if (selected.length > 1) {
    throw new Error("workflow output value source must set exactly one source");
  }
  switch (selected[0]) {
    case "agentOutput":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "agentOutput", value: input.agentOutput ?? "" },
      });
    case "signalPayload":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "signalPayload", value: input.signalPayload ?? "" },
      });
    case "signalMetadata":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "signalMetadata", value: input.signalMetadata ?? "" },
      });
    case "agentSession":
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "agentSession", value: input.agentSession ?? "" },
      });
    default:
      return create(WorkflowOutputValueSourceSchema, {
        kind: { case: "literal", value: valueInput(input.literal) },
      });
  }
}

/** Returns native input copied from a workflow output value source. */
export function workflowOutputValueSourceInputFromSource(
  input?: WorkflowOutputValueSource,
): WorkflowOutputValueSourceInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "agentOutput":
      return { agentOutput: input.kind.value };
    case "signalPayload":
      return { signalPayload: input.kind.value };
    case "signalMetadata":
      return { signalMetadata: input.kind.value };
    case "agentSession":
      return { agentSession: input.kind.value };
    case "literal":
      return { literal: jsonFromValue(input.kind.value) as JsonInput };
    default:
      return {};
  }
}

/** Creates a workflow output binding from native input. */
export function workflowOutputBinding(
  input: WorkflowOutputBindingInput | WorkflowOutputBinding = {},
): WorkflowOutputBinding {
  return create(WorkflowOutputBindingSchema, {
    inputField: input.inputField ?? "",
    value: input.value === undefined ? undefined : workflowOutputValueSource(input.value),
  });
}

/** Returns native input copied from a workflow output binding. */
export function workflowOutputBindingInputFromBinding(
  input: WorkflowOutputBinding,
): WorkflowOutputBindingInput {
  return {
    inputField: input.inputField,
    value: workflowOutputValueSourceInputFromSource(input.value),
  };
}

/** Creates a workflow output delivery from native input. */
export function workflowOutputDelivery(
  input: WorkflowOutputDeliveryInput | WorkflowOutputDelivery = {},
): WorkflowOutputDelivery {
  return create(WorkflowOutputDeliverySchema, {
    target: input.target === undefined ? undefined : boundWorkflowPluginTarget(input.target),
    inputBindings: (input.inputBindings ?? []).map((binding) => workflowOutputBinding(binding)),
    credentialMode: input.credentialMode ?? "",
  });
}

/** Returns native input copied from a workflow output delivery. */
export function workflowOutputDeliveryInputFromDelivery(
  input?: WorkflowOutputDelivery,
): WorkflowOutputDeliveryInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    target: boundWorkflowPluginTargetInputFromTarget(input.target),
    inputBindings: input.inputBindings.map((binding) => workflowOutputBindingInputFromBinding(binding)),
    credentialMode: input.credentialMode,
  };
}

/** Creates a bound plugin workflow target from native input. */
export function boundWorkflowPluginTarget(
  input: BoundWorkflowPluginTargetInput | BoundWorkflowPluginTarget = {},
): BoundWorkflowPluginTarget {
  return create(BoundWorkflowPluginTargetSchema, {
    pluginName: input.pluginName ?? "",
    operation: input.operation ?? "",
    input: input.input === undefined ? undefined : structFromObject(input.input),
    connection: input.connection ?? "",
    instance: input.instance ?? "",
    credentialMode: input.credentialMode ?? "",
  });
}

/** Returns native input copied from a bound plugin workflow target. */
export function boundWorkflowPluginTargetInputFromTarget(
  input?: BoundWorkflowPluginTarget,
): BoundWorkflowPluginTargetInput | undefined {
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
  input: BoundWorkflowAgentTargetInput | BoundWorkflowAgentTarget = {},
): BoundWorkflowAgentTarget {
  return create(BoundWorkflowAgentTargetSchema, {
    providerName: input.providerName ?? "",
    model: input.model ?? "",
    prompt: input.prompt ?? "",
    messages: (input.messages ?? []).map((message) => create(AgentMessageSchema, message)),
    toolRefs: (input.toolRefs ?? []).map((toolRef) => create(AgentToolRefSchema, toolRef)),
    responseSchema: input.responseSchema === undefined ? undefined : structFromObject(input.responseSchema),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
    timeoutSeconds: input.timeoutSeconds ?? 0,
    outputDelivery: input.outputDelivery === undefined ? undefined : workflowOutputDelivery(input.outputDelivery),
    modelOptions: input.modelOptions === undefined ? undefined : structFromObject(input.modelOptions),
    sessionReadyDelivery: input.sessionReadyDelivery === undefined
      ? undefined
      : workflowOutputDelivery(input.sessionReadyDelivery),
  });
}

/** Returns native input copied from a bound agent workflow target. */
export function boundWorkflowAgentTargetInputFromTarget(
  input?: BoundWorkflowAgentTarget,
): BoundWorkflowAgentTargetInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    providerName: input.providerName,
    model: input.model,
    prompt: input.prompt,
    messages: input.messages.map((message) => create(AgentMessageSchema, message)),
    toolRefs: input.toolRefs.map((toolRef) => create(AgentToolRefSchema, toolRef)),
    responseSchema: input.responseSchema === undefined ? undefined : jsonObjectClone(input.responseSchema),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
    timeoutSeconds: input.timeoutSeconds,
    outputDelivery: workflowOutputDeliveryInputFromDelivery(input.outputDelivery),
    modelOptions: input.modelOptions === undefined ? undefined : jsonObjectClone(input.modelOptions),
    sessionReadyDelivery: workflowOutputDeliveryInputFromDelivery(input.sessionReadyDelivery),
  };
}

/** Creates a bound workflow target from native input. */
export function boundWorkflowTarget(
  input: BoundWorkflowTargetInput | BoundWorkflowTarget = {},
): BoundWorkflowTarget {
  if ("kind" in input) {
    return boundWorkflowTargetFromTarget(input);
  }
  if (input.plugin !== undefined && input.agent !== undefined) {
    throw new Error("bound workflow target must set either plugin or agent");
  }
  if (input.plugin !== undefined) {
    return create(BoundWorkflowTargetSchema, {
      kind: { case: "plugin", value: boundWorkflowPluginTarget(input.plugin) },
    });
  }
  if (input.agent !== undefined) {
    return create(BoundWorkflowTargetSchema, {
      kind: { case: "agent", value: boundWorkflowAgentTarget(input.agent) },
    });
  }
  return create(BoundWorkflowTargetSchema);
}

/** Returns native input copied from a bound workflow target. */
export function boundWorkflowTargetInputFromTarget(
  input?: BoundWorkflowTarget,
): BoundWorkflowTargetInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "plugin":
      return { plugin: boundWorkflowPluginTargetInputFromTarget(input.kind.value) };
    case "agent":
      return { agent: boundWorkflowAgentTargetInputFromTarget(input.kind.value) };
    default:
      return {};
  }
}

/** Returns a deep copy of a bound workflow target. */
export function boundWorkflowTargetFromTarget(input: BoundWorkflowTarget): BoundWorkflowTarget {
  return boundWorkflowTarget(boundWorkflowTargetInputFromTarget(input) ?? {});
}

/** Creates a workflow event from native input. */
export function workflowEvent(input: WorkflowEventInput | WorkflowEvent = {}): WorkflowEvent {
  if ("extensions" in input && "$typeName" in input) {
    return workflowEvent(workflowEventInputFromEvent(input as WorkflowEvent));
  }
  return create(WorkflowEventSchema, {
    id: input.id ?? "",
    source: input.source ?? "",
    specVersion: input.specVersion ?? "",
    type: input.type ?? "",
    subject: input.subject ?? "",
    time: timestampInput(input.time),
    datacontenttype: input.datacontenttype ?? "",
    data: input.data === undefined ? undefined : structFromObject(input.data),
    extensions: valueMapInput(input.extensions),
  });
}

/** Returns native input copied from a workflow event. */
export function workflowEventInputFromEvent(input?: WorkflowEvent): WorkflowEventInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    source: input.source,
    specVersion: input.specVersion,
    type: input.type,
    subject: input.subject,
    time: input.time === undefined ? undefined : dateFromTimestamp(input.time),
    datacontenttype: input.datacontenttype,
    data: input.data === undefined ? undefined : jsonObjectClone(input.data),
    extensions: Object.fromEntries(
      Object.entries(input.extensions).map(([key, value]) => [key, jsonFromValue(value) as JsonInput]),
    ),
  };
}

/** Returns a deep copy of a workflow event. */
export function workflowEventFromEvent(input: WorkflowEvent): WorkflowEvent {
  return workflowEvent(workflowEventInputFromEvent(input) ?? {});
}

/** Creates a workflow signal from native input. */
export function workflowSignal(input: WorkflowSignalInput | WorkflowSignal = {}): WorkflowSignal {
  return create(WorkflowSignalSchema, {
    id: input.id ?? "",
    name: input.name ?? "",
    payload: input.payload === undefined ? undefined : structFromObject(input.payload),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    createdAt: timestampInput(input.createdAt),
    idempotencyKey: input.idempotencyKey ?? "",
    sequence: input.sequence === undefined ? 0n : BigInt(input.sequence),
  });
}

/** Returns native input copied from a workflow signal. */
export function workflowSignalInputFromSignal(input?: WorkflowSignal): WorkflowSignalInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    name: input.name,
    payload: input.payload === undefined ? undefined : jsonObjectClone(input.payload),
    metadata: input.metadata === undefined ? undefined : jsonObjectClone(input.metadata),
    createdBy: workflowActorInputFromActor(input.createdBy),
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
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
  input: WorkflowScheduleTriggerInput | WorkflowScheduleTrigger = {},
): WorkflowScheduleTrigger {
  return create(WorkflowScheduleTriggerSchema, {
    scheduleId: input.scheduleId ?? "",
    scheduledFor: timestampInput(input.scheduledFor),
  });
}

/** Creates a workflow event-trigger invocation from native input. */
export function workflowEventTriggerInvocation(
  input: WorkflowEventTriggerInvocationInput | WorkflowEventTriggerInvocation = {},
): WorkflowEventTriggerInvocation {
  return create(WorkflowEventTriggerInvocationSchema, {
    triggerId: input.triggerId ?? "",
    event: input.event === undefined ? undefined : workflowEvent(input.event),
  });
}

/** Creates a workflow run trigger from native input. */
export function workflowRunTrigger(
  input: WorkflowRunTriggerInput | WorkflowRunTrigger = {},
): WorkflowRunTrigger {
  if ("kind" in input) {
    return workflowRunTriggerFromTrigger(input);
  }
  const selected = [
    input.manual === true ? "manual" : undefined,
    input.schedule === undefined ? undefined : "schedule",
    input.event === undefined ? undefined : "event",
  ].filter((value): value is string => value !== undefined);
  if (selected.length === 0) {
    return create(WorkflowRunTriggerSchema);
  }
  if (selected.length > 1) {
    throw new Error("workflow run trigger must set exactly one trigger kind");
  }
  switch (selected[0]) {
    case "manual":
      return create(WorkflowRunTriggerSchema, {
        kind: { case: "manual", value: create(WorkflowManualTriggerSchema) },
      });
    case "schedule":
      return create(WorkflowRunTriggerSchema, {
        kind: { case: "schedule", value: workflowScheduleTrigger(input.schedule!) },
      });
    default:
      return create(WorkflowRunTriggerSchema, {
        kind: { case: "event", value: workflowEventTriggerInvocation(input.event!) },
      });
  }
}

/** Returns native input copied from a workflow run trigger. */
export function workflowRunTriggerInputFromTrigger(
  input?: WorkflowRunTrigger,
): WorkflowRunTriggerInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  switch (input.kind.case) {
    case "manual":
      return { manual: true };
    case "schedule":
      return {
        schedule: {
          scheduleId: input.kind.value.scheduleId,
          scheduledFor: input.kind.value.scheduledFor === undefined
            ? undefined
            : dateFromTimestamp(input.kind.value.scheduledFor),
        },
      };
    case "event":
      return {
        event: {
          triggerId: input.kind.value.triggerId,
          event: workflowEventInputFromEvent(input.kind.value.event),
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

function invokeWorkflowOperationRequest(input: InvokeWorkflowOperationInput) {
  return create(InvokeWorkflowOperationRequestSchema, {
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    runId: input.runId ?? "",
    trigger: input.trigger === undefined ? undefined : workflowRunTrigger(input.trigger),
    input: input.input === undefined ? undefined : structFromObject(input.input),
    metadata: input.metadata === undefined ? undefined : structFromObject(input.metadata),
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
    signals: (input.signals ?? []).map((signal) => workflowSignal(signal)),
  });
}

/** Creates a workflow-provider run from native input. */
export function boundWorkflowRun(input: BoundWorkflowRunInput | BoundWorkflowRun = {}): BoundWorkflowRun {
  return create(BoundWorkflowRunSchema, {
    id: input.id ?? "",
    status: input.status ?? WorkflowRunStatus.UNSPECIFIED,
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    trigger: input.trigger === undefined ? undefined : workflowRunTrigger(input.trigger),
    createdAt: timestampInput(input.createdAt),
    startedAt: timestampInput(input.startedAt),
    completedAt: timestampInput(input.completedAt),
    statusMessage: input.statusMessage ?? "",
    resultBody: input.resultBody ?? "",
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
    workflowKey: input.workflowKey ?? "",
  });
}

/** Returns native input copied from a workflow-provider run. */
export function boundWorkflowRunInputFromRun(input?: BoundWorkflowRun): BoundWorkflowRunInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    status: input.status,
    target: boundWorkflowTargetInputFromTarget(input.target),
    trigger: workflowRunTriggerInputFromTrigger(input.trigger),
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
    startedAt: input.startedAt === undefined ? undefined : dateFromTimestamp(input.startedAt),
    completedAt: input.completedAt === undefined ? undefined : dateFromTimestamp(input.completedAt),
    statusMessage: input.statusMessage,
    resultBody: input.resultBody,
    createdBy: workflowActorInputFromActor(input.createdBy),
    executionRef: input.executionRef,
    workflowKey: input.workflowKey,
  };
}

/** Returns a deep copy of a workflow-provider run. */
export function boundWorkflowRunFromRun(input: BoundWorkflowRun): BoundWorkflowRun {
  return boundWorkflowRun(boundWorkflowRunInputFromRun(input) ?? {});
}

/** Creates a workflow-provider definition from native input. */
export function boundWorkflowDefinition(
  input: BoundWorkflowDefinitionInput | BoundWorkflowDefinition = {},
): BoundWorkflowDefinition {
  return create(BoundWorkflowDefinitionSchema, {
    id: input.id ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    createdAt: timestampInput(input.createdAt),
  });
}

/** Returns native input copied from a workflow-provider definition. */
export function boundWorkflowDefinitionInputFromDefinition(
  input?: BoundWorkflowDefinition,
): BoundWorkflowDefinitionInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    target: boundWorkflowTargetInputFromTarget(input.target),
    createdBy: workflowActorInputFromActor(input.createdBy),
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
  };
}

/** Creates a workflow-provider schedule from native input. */
export function boundWorkflowSchedule(
  input: BoundWorkflowScheduleInput | BoundWorkflowSchedule = {},
): BoundWorkflowSchedule {
  return create(BoundWorkflowScheduleSchema, {
    id: input.id ?? "",
    cron: input.cron ?? "",
    timezone: input.timezone ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    paused: input.paused ?? false,
    createdAt: timestampInput(input.createdAt),
    updatedAt: timestampInput(input.updatedAt),
    nextRunAt: timestampInput(input.nextRunAt),
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
  });
}

/** Returns native input copied from a workflow-provider schedule. */
export function boundWorkflowScheduleInputFromSchedule(
  input?: BoundWorkflowSchedule,
): BoundWorkflowScheduleInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    cron: input.cron,
    timezone: input.timezone,
    target: boundWorkflowTargetInputFromTarget(input.target),
    paused: input.paused,
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
    updatedAt: input.updatedAt === undefined ? undefined : dateFromTimestamp(input.updatedAt),
    nextRunAt: input.nextRunAt === undefined ? undefined : dateFromTimestamp(input.nextRunAt),
    createdBy: workflowActorInputFromActor(input.createdBy),
    executionRef: input.executionRef,
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
  input: BoundWorkflowEventTriggerInput | BoundWorkflowEventTrigger = {},
): BoundWorkflowEventTrigger {
  return create(BoundWorkflowEventTriggerSchema, {
    id: input.id ?? "",
    match: input.match === undefined ? undefined : workflowEventMatch(input.match),
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    paused: input.paused ?? false,
    createdAt: timestampInput(input.createdAt),
    updatedAt: timestampInput(input.updatedAt),
    createdBy: input.createdBy === undefined ? undefined : workflowActor(input.createdBy),
    executionRef: input.executionRef ?? "",
  });
}

/** Returns native input copied from a workflow-provider event trigger. */
export function boundWorkflowEventTriggerInputFromTrigger(
  input?: BoundWorkflowEventTrigger,
): BoundWorkflowEventTriggerInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    match: workflowEventMatchInputFromMatch(input.match),
    target: boundWorkflowTargetInputFromTarget(input.target),
    paused: input.paused,
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
    updatedAt: input.updatedAt === undefined ? undefined : dateFromTimestamp(input.updatedAt),
    createdBy: workflowActorInputFromActor(input.createdBy),
    executionRef: input.executionRef,
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
  input: WorkflowExecutionReferenceInput | WorkflowExecutionReference = {},
): WorkflowExecutionReference {
  return create(WorkflowExecutionReferenceSchema, {
    id: input.id ?? "",
    providerName: input.providerName ?? "",
    target: input.target === undefined ? undefined : boundWorkflowTarget(input.target),
    subjectId: input.subjectId ?? "",
    credentialSubjectId: input.credentialSubjectId ?? "",
    permissions: (input.permissions ?? []).map((permission) => workflowAccessPermission(permission)),
    createdAt: timestampInput(input.createdAt),
    revokedAt: timestampInput(input.revokedAt),
    subjectKind: input.subjectKind ?? "",
    displayName: input.displayName ?? "",
    authSource: input.authSource ?? "",
    callerPluginName: input.callerPluginName ?? "",
    runAs: input.runAs === undefined ? undefined : workflowRunAsSubject(input.runAs),
    sourceDefinitionId: input.sourceDefinitionId ?? "",
  });
}

/** Returns native input copied from a workflow execution reference. */
export function workflowExecutionReferenceInputFromReference(
  input?: WorkflowExecutionReference,
): WorkflowExecutionReferenceInput | undefined {
  if (input === undefined) {
    return undefined;
  }
  return {
    id: input.id,
    providerName: input.providerName,
    target: boundWorkflowTargetInputFromTarget(input.target),
    subjectId: input.subjectId,
    credentialSubjectId: input.credentialSubjectId,
    permissions: input.permissions.map((permission) => workflowAccessPermissionInputFromPermission(permission)),
    createdAt: input.createdAt === undefined ? undefined : dateFromTimestamp(input.createdAt),
    revokedAt: input.revokedAt === undefined ? undefined : dateFromTimestamp(input.revokedAt),
    subjectKind: input.subjectKind,
    displayName: input.displayName,
    authSource: input.authSource,
    callerPluginName: input.callerPluginName,
    runAs: workflowRunAsSubjectInputFromSubject(input.runAs),
    sourceDefinitionId: input.sourceDefinitionId,
  };
}

/** Returns a deep copy of a workflow execution reference. */
export function workflowExecutionReferenceFromReference(
  input: WorkflowExecutionReference,
): WorkflowExecutionReference {
  return workflowExecutionReference(workflowExecutionReferenceInputFromReference(input) ?? {});
}

function timestampInput(input?: TimestampInput): Timestamp | undefined {
  if (input === undefined) {
    return undefined;
  }
  if (input instanceof Date) {
    return timestampFromDate(input);
  }
  return create(TimestampSchema, input);
}

function valueInput(input: JsonInput | Value | undefined): Value {
  if (isValue(input)) {
    return create(ValueSchema, input);
  }
  return valueFromJson(input as JsonInput);
}

function valueMapInput(input?: Record<string, JsonInput | Value>): Record<string, Value> {
  if (input === undefined) {
    return {};
  }
  return Object.fromEntries(
    Object.entries(input).map(([key, value]) => [key, valueInput(value)]),
  );
}

function jsonObjectClone(input: JsonObject): JsonObject {
  return structFromObject(jsonObjectFromStruct(input));
}

function isValue(input: unknown): input is Value {
  return (
    typeof input === "object"
    && input !== null
    && "$typeName" in input
    && (input as { $typeName?: unknown }).$typeName === "google.protobuf.Value"
  );
}

/** Handlers and runtime metadata for a workflow provider. */
export interface WorkflowProviderOptions extends ProviderBaseOptions {
  startRun: (
    request: StartWorkflowProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>>;
  getRun: (
    request: GetWorkflowProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>>;
  listRuns: (
    request: ListWorkflowProviderRunsRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>[]>;
  cancelRun: (
    request: CancelWorkflowProviderRunRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowRunSchema>>;
  upsertSchedule: (
    request: UpsertWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  getSchedule: (
    request: GetWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  listSchedules: (
    request: ListWorkflowProviderSchedulesRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>[]>;
  deleteSchedule: (
    request: DeleteWorkflowProviderScheduleRequest,
  ) => MaybePromise<void>;
  pauseSchedule: (
    request: PauseWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  resumeSchedule: (
    request: ResumeWorkflowProviderScheduleRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowScheduleSchema>>;
  upsertEventTrigger: (
    request: UpsertWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  getEventTrigger: (
    request: GetWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  listEventTriggers: (
    request: ListWorkflowProviderEventTriggersRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>[]>;
  deleteEventTrigger: (
    request: DeleteWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<void>;
  pauseEventTrigger: (
    request: PauseWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  resumeEventTrigger: (
    request: ResumeWorkflowProviderEventTriggerRequest,
  ) => MaybePromise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>>;
  /** Store or update an execution reference for a workflow target. */
  putExecutionReference?: (
    request: PutWorkflowExecutionReferenceRequest,
  ) => MaybePromise<MessageInitShape<typeof WorkflowExecutionReferenceSchema>>;
  /** Load one execution reference by provider-owned lookup fields. */
  getExecutionReference?: (
    request: GetWorkflowExecutionReferenceRequest,
  ) => MaybePromise<MessageInitShape<typeof WorkflowExecutionReferenceSchema>>;
  /** List execution references for the requested scope. */
  listExecutionReferences?: (
    request: ListWorkflowExecutionReferencesRequest,
  ) => MaybePromise<MessageInitShape<typeof WorkflowExecutionReferenceSchema>[]>;
  publishEvent: (
    request: PublishWorkflowProviderEventRequest,
  ) => MaybePromise<void>;
}

/** Runtime provider implementation for the Gestalt workflow host contract. */
export class WorkflowProvider extends ProviderBase {
  readonly kind = "workflow" as const;

  private readonly startRunHandler: WorkflowProviderOptions["startRun"];
  private readonly getRunHandler: WorkflowProviderOptions["getRun"];
  private readonly listRunsHandler: WorkflowProviderOptions["listRuns"];
  private readonly cancelRunHandler: WorkflowProviderOptions["cancelRun"];
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
    this.startRunHandler = options.startRun;
    this.getRunHandler = options.getRun;
    this.listRunsHandler = options.listRuns;
    this.cancelRunHandler = options.cancelRun;
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

  async startRun(
    request: StartWorkflowProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>> {
    return await this.startRunHandler(request);
  }

  async getRun(
    request: GetWorkflowProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>> {
    return await this.getRunHandler(request);
  }

  async listRuns(
    request: ListWorkflowProviderRunsRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>[]> {
    return await this.listRunsHandler(request);
  }

  async cancelRun(
    request: CancelWorkflowProviderRunRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowRunSchema>> {
    return await this.cancelRunHandler(request);
  }

  async upsertSchedule(
    request: UpsertWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.upsertScheduleHandler(request);
  }

  async getSchedule(
    request: GetWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.getScheduleHandler(request);
  }

  async listSchedules(
    request: ListWorkflowProviderSchedulesRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>[]> {
    return await this.listSchedulesHandler(request);
  }

  async deleteSchedule(
    request: DeleteWorkflowProviderScheduleRequest,
  ): Promise<void> {
    await this.deleteScheduleHandler(request);
  }

  async pauseSchedule(
    request: PauseWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.pauseScheduleHandler(request);
  }

  async resumeSchedule(
    request: ResumeWorkflowProviderScheduleRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowScheduleSchema>> {
    return await this.resumeScheduleHandler(request);
  }

  async upsertEventTrigger(
    request: UpsertWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.upsertEventTriggerHandler(request);
  }

  async getEventTrigger(
    request: GetWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.getEventTriggerHandler(request);
  }

  async listEventTriggers(
    request: ListWorkflowProviderEventTriggersRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>[]> {
    return await this.listEventTriggersHandler(request);
  }

  async deleteEventTrigger(
    request: DeleteWorkflowProviderEventTriggerRequest,
  ): Promise<void> {
    await this.deleteEventTriggerHandler(request);
  }

  async pauseEventTrigger(
    request: PauseWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.pauseEventTriggerHandler(request);
  }

  async resumeEventTrigger(
    request: ResumeWorkflowProviderEventTriggerRequest,
  ): Promise<MessageInitShape<typeof BoundWorkflowEventTriggerSchema>> {
    return await this.resumeEventTriggerHandler(request);
  }

  /** Store or update an execution reference for a workflow target. */
  async putExecutionReference(
    request: PutWorkflowExecutionReferenceRequest,
  ): Promise<MessageInitShape<typeof WorkflowExecutionReferenceSchema>> {
    return await requireWorkflowProviderHandler(
      "put execution reference",
      this.putExecutionReferenceHandler,
      request,
    );
  }

  /** Load one execution reference by provider-owned lookup fields. */
  async getExecutionReference(
    request: GetWorkflowExecutionReferenceRequest,
  ): Promise<MessageInitShape<typeof WorkflowExecutionReferenceSchema>> {
    return await requireWorkflowProviderHandler(
      "get execution reference",
      this.getExecutionReferenceHandler,
      request,
    );
  }

  /** List execution references for the requested scope. */
  async listExecutionReferences(
    request: ListWorkflowExecutionReferencesRequest,
  ): Promise<MessageInitShape<typeof WorkflowExecutionReferenceSchema>[]> {
    return await requireWorkflowProviderHandler(
      "list execution references",
      this.listExecutionReferencesHandler,
      request,
    );
  }

  async publishEvent(
    request: PublishWorkflowProviderEventRequest,
  ): Promise<void> {
    await this.publishEventHandler(request);
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
      "startRun" in value &&
      "getRun" in value &&
      "listRuns" in value &&
      "cancelRun" in value &&
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
    const target = process.env[ENV_WORKFLOW_HOST_SOCKET];
    if (!target) {
      throw new Error(`workflow host: ${ENV_WORKFLOW_HOST_SOCKET} is not set`);
    }
    const relayToken = process.env[ENV_WORKFLOW_HOST_SOCKET_TOKEN]?.trim() ?? "";
    const transport = createGrpcTransport({
      ...workflowHostTransportOptions(target),
      interceptors: relayToken
        ? [workflowHostRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(WorkflowHostService, transport);
  }

  /** Invokes an operation through the workflow host service. */
  async invokeOperation(
    input: InvokeWorkflowOperationInput,
  ): Promise<InvokeWorkflowOperationResponse> {
    const response = await this.client.invokeOperation(invokeWorkflowOperationRequest(input));
    return { status: response.status, body: response.body };
  }
}

function workflowHostTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("workflow host: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `workflow host: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `workflow host: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `workflow host: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `workflow host: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function workflowHostRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(WORKFLOW_HOST_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}

/** Builds the Connect service implementation used by the TypeScript runtime. */
export function createWorkflowProviderService(
  provider: WorkflowProvider,
): Partial<ServiceImpl<typeof WorkflowProviderService>> {
  return {
    async startRun(request) {
      return create(
        BoundWorkflowRunSchema,
        await invokeWorkflowProvider("start run", () => provider.startRun(request)),
      );
    },
    async getRun(request) {
      return create(
        BoundWorkflowRunSchema,
        await invokeWorkflowProvider("get run", () => provider.getRun(request)),
      );
    },
    async listRuns(request) {
      return create(ListWorkflowProviderRunsResponseSchema, {
        runs: await invokeWorkflowProvider("list runs", () =>
          provider.listRuns(request),
        ),
      });
    },
    async cancelRun(request) {
      return create(
        BoundWorkflowRunSchema,
        await invokeWorkflowProvider("cancel run", () => provider.cancelRun(request)),
      );
    },
    async upsertSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("upsert schedule", () =>
          provider.upsertSchedule(request),
        ),
      );
    },
    async getSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("get schedule", () =>
          provider.getSchedule(request),
        ),
      );
    },
    async listSchedules(request) {
      return create(ListWorkflowProviderSchedulesResponseSchema, {
        schedules: await invokeWorkflowProvider("list schedules", () =>
          provider.listSchedules(request),
        ),
      });
    },
    async deleteSchedule(request) {
      await invokeWorkflowProvider("delete schedule", () =>
        provider.deleteSchedule(request),
      );
      return create(EmptySchema, {});
    },
    async pauseSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("pause schedule", () =>
          provider.pauseSchedule(request),
        ),
      );
    },
    async resumeSchedule(request) {
      return create(
        BoundWorkflowScheduleSchema,
        await invokeWorkflowProvider("resume schedule", () =>
          provider.resumeSchedule(request),
        ),
      );
    },
    async upsertEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("upsert event trigger", () =>
          provider.upsertEventTrigger(request),
        ),
      );
    },
    async getEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("get event trigger", () =>
          provider.getEventTrigger(request),
        ),
      );
    },
    async listEventTriggers(request) {
      return create(ListWorkflowProviderEventTriggersResponseSchema, {
        triggers: await invokeWorkflowProvider("list event triggers", () =>
          provider.listEventTriggers(request),
        ),
      });
    },
    async deleteEventTrigger(request) {
      await invokeWorkflowProvider("delete event trigger", () =>
        provider.deleteEventTrigger(request),
      );
      return create(EmptySchema, {});
    },
    async pauseEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("pause event trigger", () =>
          provider.pauseEventTrigger(request),
        ),
      );
    },
    async resumeEventTrigger(request) {
      return create(
        BoundWorkflowEventTriggerSchema,
        await invokeWorkflowProvider("resume event trigger", () =>
          provider.resumeEventTrigger(request),
        ),
      );
    },
    async putExecutionReference(request) {
      return create(
        WorkflowExecutionReferenceSchema,
        await invokeWorkflowProvider("put execution reference", () =>
          provider.putExecutionReference(request),
        ),
      );
    },
    async getExecutionReference(request) {
      return create(
        WorkflowExecutionReferenceSchema,
        await invokeWorkflowProvider("get execution reference", () =>
          provider.getExecutionReference(request),
        ),
      );
    },
    async listExecutionReferences(request) {
      return create(ListWorkflowExecutionReferencesResponseSchema, {
        references: await invokeWorkflowProvider("list execution references", () =>
          provider.listExecutionReferences(request),
        ),
      });
    },
    async publishEvent(request) {
      await invokeWorkflowProvider("publish event", () =>
        provider.publishEvent(request),
      );
      return create(EmptySchema, {});
    },
  };
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
