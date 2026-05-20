import {
  create,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
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
  ApplyWorkflowDeploymentRequestSchema,
  BoundWorkflowTargetSchema,
  CancelWorkflowRunRequestSchema,
  DeleteWorkflowDeploymentRequestSchema,
  DeliverWorkflowEventRequestSchema,
  DeliverWorkflowEventResponseSchema,
  GetWorkflowDeploymentRequestSchema,
  GetWorkflowRunEventsRequestSchema,
  GetWorkflowRunOutputRequestSchema,
  GetWorkflowRunRequestSchema,
  InvokeWorkflowActionRequestSchema,
  ListWorkflowDeploymentsResponseSchema,
  ListWorkflowRunEventsResponseSchema,
  ListWorkflowRunsResponseSchema,
  PlanWorkflowResponseSchema,
  SetWorkflowActivationPausedRequestSchema,
  SetWorkflowDeploymentPausedRequestSchema,
  SignalOrStartWorkflowRunRequestSchema,
  SignalWorkflowRunRequestSchema,
  StartWorkflowRunRequestSchema,
  WorkflowAccessPermissionSchema,
  WorkflowActionDescriptorSchema,
  WorkflowActionKind as ProtoWorkflowActionKind,
  WorkflowActionResultSchema,
  WorkflowActionTableSchema,
  WorkflowActivationMode as ProtoWorkflowActivationMode,
  WorkflowActivationSchema,
  WorkflowActorSchema,
  WorkflowAgentMessageSchema,
  WorkflowAgentTurnPayloadSchema,
  WorkflowArraySchema,
  WorkflowDeploymentBindingSchema,
  WorkflowDeploymentSchema,
  WorkflowDeploymentSpecSchema,
  WorkflowDeploymentStatus as ProtoWorkflowDeploymentStatus,
  WorkflowEventActivationSchema,
  WorkflowEventDeliveryResultSchema,
  WorkflowEventMatchSchema,
  WorkflowEventSchema,
  WorkflowEventTriggerSchema,
  WorkflowHost as WorkflowHostService,
  WorkflowHostActionSelectorSchema,
  WorkflowManualActivationSchema,
  WorkflowManualTriggerSchema,
  WorkflowObjectSchema,
  WorkflowOutputSummarySchema,
  WorkflowPathSourceSchema,
  WorkflowPluginActionPayloadSchema,
  WorkflowProvider as WorkflowProviderService,
  WorkflowRunErrorSchema,
  WorkflowRunEventSchema,
  WorkflowRunEventType as ProtoWorkflowRunEventType,
  WorkflowRunOutputSchema,
  WorkflowRunSchema,
  WorkflowRunSignalSchema,
  WorkflowRunStatus as ProtoWorkflowRunStatus,
  WorkflowRunTriggerSchema,
  WorkflowRunAsSubjectSchema,
  WorkflowScheduleActivationSchema,
  WorkflowScheduleTriggerSchema,
  WorkflowSignalSchema,
  WorkflowStepAgentTurnSchema,
  WorkflowStepDeliverySchema,
  WorkflowStepOutputSourceSchema,
  WorkflowStepPluginCallSchema,
  WorkflowStepSchema,
  WorkflowStepStateSchema,
  WorkflowStepStatus as ProtoWorkflowStepStatus,
  WorkflowStepWhenSchema,
  WorkflowTextSchema,
  WorkflowUnsupportedFeatureSchema,
  WorkflowValueSchema,
  type ApplyWorkflowDeploymentRequest as ProtoApplyWorkflowDeploymentRequest,
  type BoundWorkflowTarget as ProtoBoundWorkflowTarget,
  type CancelWorkflowRunRequest as ProtoCancelWorkflowRunRequest,
  type DeleteWorkflowDeploymentRequest as ProtoDeleteWorkflowDeploymentRequest,
  type DeliverWorkflowEventRequest as ProtoDeliverWorkflowEventRequest,
  type DeliverWorkflowEventResponse as ProtoDeliverWorkflowEventResponse,
  type GetWorkflowDeploymentRequest as ProtoGetWorkflowDeploymentRequest,
  type GetWorkflowRunEventsRequest as ProtoGetWorkflowRunEventsRequest,
  type GetWorkflowRunOutputRequest as ProtoGetWorkflowRunOutputRequest,
  type GetWorkflowRunRequest as ProtoGetWorkflowRunRequest,
  type InvokeWorkflowActionRequest as ProtoInvokeWorkflowActionRequest,
  type ListWorkflowDeploymentsRequest as ProtoListWorkflowDeploymentsRequest,
  type ListWorkflowDeploymentsResponse as ProtoListWorkflowDeploymentsResponse,
  type ListWorkflowRunEventsResponse as ProtoListWorkflowRunEventsResponse,
  type ListWorkflowRunsRequest as ProtoListWorkflowRunsRequest,
  type ListWorkflowRunsResponse as ProtoListWorkflowRunsResponse,
  type ManagedWorkflowDeployment as ProtoManagedWorkflowDeployment,
  type ManagedWorkflowRun as ProtoManagedWorkflowRun,
  type ManagedWorkflowRunSignal as ProtoManagedWorkflowRunSignal,
  type PlanWorkflowRequest as ProtoPlanWorkflowRequest,
  type PlanWorkflowResponse as ProtoPlanWorkflowResponse,
  type SetWorkflowActivationPausedRequest as ProtoSetWorkflowActivationPausedRequest,
  type SetWorkflowDeploymentPausedRequest as ProtoSetWorkflowDeploymentPausedRequest,
  type SignalOrStartWorkflowRunRequest as ProtoSignalOrStartWorkflowRunRequest,
  type SignalWorkflowRunRequest as ProtoSignalWorkflowRunRequest,
  type StartWorkflowRunRequest as ProtoStartWorkflowRunRequest,
  type WorkflowAccessPermission as ProtoWorkflowAccessPermission,
  type WorkflowActionDescriptor as ProtoWorkflowActionDescriptor,
  type WorkflowActionResult as ProtoWorkflowActionResult,
  type WorkflowActionTable as ProtoWorkflowActionTable,
  type WorkflowActivation as ProtoWorkflowActivation,
  type WorkflowActor as ProtoWorkflowActor,
  type WorkflowAgentMessage as ProtoWorkflowAgentMessage,
  type WorkflowAgentTurnPayload as ProtoWorkflowAgentTurnPayload,
  type WorkflowArray as ProtoWorkflowArray,
  type WorkflowDeployment as ProtoWorkflowDeployment,
  type WorkflowDeploymentBinding as ProtoWorkflowDeploymentBinding,
  type WorkflowDeploymentSpec as ProtoWorkflowDeploymentSpec,
  type WorkflowEvent as ProtoWorkflowEvent,
  type WorkflowEventActivation as ProtoWorkflowEventActivation,
  type WorkflowEventDeliveryResult as ProtoWorkflowEventDeliveryResult,
  type WorkflowEventMatch as ProtoWorkflowEventMatch,
  type WorkflowEventTrigger as ProtoWorkflowEventTrigger,
  type WorkflowHostActionSelector as ProtoWorkflowHostActionSelector,
  type WorkflowManualActivation as ProtoWorkflowManualActivation,
  type WorkflowManualTrigger as ProtoWorkflowManualTrigger,
  type WorkflowManagerDeliverEventResponse as ProtoWorkflowManagerDeliverEventResponse,
  type WorkflowObject as ProtoWorkflowObject,
  type WorkflowOutputSummary as ProtoWorkflowOutputSummary,
  type WorkflowPathSource as ProtoWorkflowPathSource,
  type WorkflowPluginActionPayload as ProtoWorkflowPluginActionPayload,
  type WorkflowRun as ProtoWorkflowRun,
  type WorkflowRunError as ProtoWorkflowRunError,
  type WorkflowRunEvent as ProtoWorkflowRunEvent,
  type WorkflowRunOutput as ProtoWorkflowRunOutput,
  type WorkflowRunSignal as ProtoWorkflowRunSignal,
  type WorkflowRunTrigger as ProtoWorkflowRunTrigger,
  type WorkflowRunAsSubject as ProtoWorkflowRunAsSubject,
  type WorkflowScheduleActivation as ProtoWorkflowScheduleActivation,
  type WorkflowScheduleTrigger as ProtoWorkflowScheduleTrigger,
  type WorkflowSignal as ProtoWorkflowSignal,
  type WorkflowStep as ProtoWorkflowStep,
  type WorkflowStepAgentTurn as ProtoWorkflowStepAgentTurn,
  type WorkflowStepDelivery as ProtoWorkflowStepDelivery,
  type WorkflowStepOutputSource as ProtoWorkflowStepOutputSource,
  type WorkflowStepPluginCall as ProtoWorkflowStepPluginCall,
  type WorkflowStepState as ProtoWorkflowStepState,
  type WorkflowStepWhen as ProtoWorkflowStepWhen,
  type WorkflowText as ProtoWorkflowText,
  type WorkflowUnsupportedFeature as ProtoWorkflowUnsupportedFeature,
  type WorkflowValue as ProtoWorkflowValue,
} from "./internal/gen/v1/workflow_pb.ts";
import { errorMessage, type MaybePromise } from "./api.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import {
  valueFromJson,
  type JsonInput,
} from "./protocol.ts";

type WorkflowProviderServiceImpl = Partial<
  ServiceImpl<typeof WorkflowProviderService>
>;

/**
 * Environment variable containing the workflow-host service target.
 * @internal
 */
export const ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET";
/**
 * Environment variable containing the optional workflow-host relay token.
 * @internal
 */
export const ENV_WORKFLOW_HOST_SOCKET_TOKEN = `${ENV_WORKFLOW_HOST_SOCKET}_TOKEN`;
const WORKFLOW_HOST_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";

/** Workflow activation mode constants. */
export const WorkflowActivationMode = {
  UNSPECIFIED: ProtoWorkflowActivationMode.UNSPECIFIED,
  START: ProtoWorkflowActivationMode.START,
  SIGNAL: ProtoWorkflowActivationMode.SIGNAL,
  SIGNAL_OR_START: ProtoWorkflowActivationMode.SIGNAL_OR_START,
} as const;
export type WorkflowActivationMode =
  (typeof WorkflowActivationMode)[keyof typeof WorkflowActivationMode];

/** Workflow action kind constants. */
export const WorkflowActionKind = {
  UNSPECIFIED: ProtoWorkflowActionKind.UNSPECIFIED,
  PLUGIN: ProtoWorkflowActionKind.PLUGIN,
  AGENT_TURN: ProtoWorkflowActionKind.AGENT_TURN,
  DELIVERY: ProtoWorkflowActionKind.DELIVERY,
} as const;
export type WorkflowActionKind =
  (typeof WorkflowActionKind)[keyof typeof WorkflowActionKind];

/** Workflow deployment status constants. */
export const WorkflowDeploymentStatus = {
  UNSPECIFIED: ProtoWorkflowDeploymentStatus.UNSPECIFIED,
  PENDING: ProtoWorkflowDeploymentStatus.PENDING,
  ACTIVE: ProtoWorkflowDeploymentStatus.ACTIVE,
  PAUSED: ProtoWorkflowDeploymentStatus.PAUSED,
  DELETED: ProtoWorkflowDeploymentStatus.DELETED,
  FAILED: ProtoWorkflowDeploymentStatus.FAILED,
} as const;
export type WorkflowDeploymentStatus =
  (typeof WorkflowDeploymentStatus)[keyof typeof WorkflowDeploymentStatus];

/** Workflow run status constants. */
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

/** Workflow step status constants. */
export const WorkflowStepStatus = {
  UNSPECIFIED: ProtoWorkflowStepStatus.UNSPECIFIED,
  PENDING: ProtoWorkflowStepStatus.PENDING,
  RUNNING: ProtoWorkflowStepStatus.RUNNING,
  SUCCEEDED: ProtoWorkflowStepStatus.SUCCEEDED,
  FAILED: ProtoWorkflowStepStatus.FAILED,
  SKIPPED: ProtoWorkflowStepStatus.SKIPPED,
  CANCELED: ProtoWorkflowStepStatus.CANCELED,
} as const;
export type WorkflowStepStatus =
  (typeof WorkflowStepStatus)[keyof typeof WorkflowStepStatus];

/** Workflow run event type constants. */
export const WorkflowRunEventType = {
  UNSPECIFIED: ProtoWorkflowRunEventType.UNSPECIFIED,
  RUN_STARTED: ProtoWorkflowRunEventType.RUN_STARTED,
  RUN_COMPLETED: ProtoWorkflowRunEventType.RUN_COMPLETED,
  RUN_FAILED: ProtoWorkflowRunEventType.RUN_FAILED,
  RUN_CANCELED: ProtoWorkflowRunEventType.RUN_CANCELED,
  SIGNAL_RECEIVED: ProtoWorkflowRunEventType.SIGNAL_RECEIVED,
  STEP_STARTED: ProtoWorkflowRunEventType.STEP_STARTED,
  STEP_SUCCEEDED: ProtoWorkflowRunEventType.STEP_SUCCEEDED,
  STEP_FAILED: ProtoWorkflowRunEventType.STEP_FAILED,
  STEP_SKIPPED: ProtoWorkflowRunEventType.STEP_SKIPPED,
  ACTION_INVOKED: ProtoWorkflowRunEventType.ACTION_INVOKED,
  ACTION_COMPLETED: ProtoWorkflowRunEventType.ACTION_COMPLETED,
  ACTION_FAILED: ProtoWorkflowRunEventType.ACTION_FAILED,
} as const;
export type WorkflowRunEventType =
  (typeof WorkflowRunEventType)[keyof typeof WorkflowRunEventType];

export type BoundWorkflowTarget = ProtoBoundWorkflowTarget;
export type WorkflowStep = ProtoWorkflowStep;
export type WorkflowStepPluginCall = ProtoWorkflowStepPluginCall;
export type WorkflowStepDelivery = ProtoWorkflowStepDelivery;
export type WorkflowStepAgentTurn = ProtoWorkflowStepAgentTurn;
export type WorkflowAgentMessage = ProtoWorkflowAgentMessage;
export type WorkflowText = ProtoWorkflowText;
export type WorkflowStepWhen = ProtoWorkflowStepWhen;
export type WorkflowValue = ProtoWorkflowValue;
export type WorkflowObject = ProtoWorkflowObject;
export type WorkflowArray = ProtoWorkflowArray;
export type WorkflowPathSource = ProtoWorkflowPathSource;
export type WorkflowStepOutputSource = ProtoWorkflowStepOutputSource;
export type WorkflowActor = ProtoWorkflowActor;
export type WorkflowRunAsSubject = ProtoWorkflowRunAsSubject;
export type WorkflowEvent = ProtoWorkflowEvent;
export type WorkflowEventMatch = ProtoWorkflowEventMatch;
export type WorkflowManualActivation = ProtoWorkflowManualActivation;
export type WorkflowScheduleActivation = ProtoWorkflowScheduleActivation;
export type WorkflowEventActivation = ProtoWorkflowEventActivation;
export type WorkflowActivation = ProtoWorkflowActivation;
export type WorkflowAccessPermission = ProtoWorkflowAccessPermission;
export type WorkflowDeploymentSpec = ProtoWorkflowDeploymentSpec;
export type WorkflowActionDescriptor = ProtoWorkflowActionDescriptor;
export type WorkflowActionTable = ProtoWorkflowActionTable;
export type WorkflowUnsupportedFeature = ProtoWorkflowUnsupportedFeature;
export type PlanWorkflowRequest = ProtoPlanWorkflowRequest;
export type PlanWorkflowResponse = ProtoPlanWorkflowResponse;
export type WorkflowDeploymentBinding = ProtoWorkflowDeploymentBinding;
export type WorkflowDeployment = ProtoWorkflowDeployment;
export type ApplyWorkflowDeploymentRequest = ProtoApplyWorkflowDeploymentRequest;
export type GetWorkflowDeploymentRequest = ProtoGetWorkflowDeploymentRequest;
export type ListWorkflowDeploymentsRequest = ProtoListWorkflowDeploymentsRequest;
export type ListWorkflowDeploymentsResponse = ProtoListWorkflowDeploymentsResponse;
export type DeleteWorkflowDeploymentRequest = ProtoDeleteWorkflowDeploymentRequest;
export type SetWorkflowDeploymentPausedRequest =
  ProtoSetWorkflowDeploymentPausedRequest;
export type SetWorkflowActivationPausedRequest =
  ProtoSetWorkflowActivationPausedRequest;
export type WorkflowManualTrigger = ProtoWorkflowManualTrigger;
export type WorkflowScheduleTrigger = ProtoWorkflowScheduleTrigger;
export type WorkflowEventTrigger = ProtoWorkflowEventTrigger;
export type WorkflowRunTrigger = ProtoWorkflowRunTrigger;
export type WorkflowSignal = ProtoWorkflowSignal;
export type WorkflowOutputSummary = ProtoWorkflowOutputSummary;
export type WorkflowRunError = ProtoWorkflowRunError;
export type WorkflowStepState = ProtoWorkflowStepState;
export type WorkflowRun = ProtoWorkflowRun;
export type StartWorkflowRunRequest = ProtoStartWorkflowRunRequest;
export type SignalWorkflowRunRequest = ProtoSignalWorkflowRunRequest;
export type SignalOrStartWorkflowRunRequest =
  ProtoSignalOrStartWorkflowRunRequest;
export type CancelWorkflowRunRequest = ProtoCancelWorkflowRunRequest;
export type GetWorkflowRunRequest = ProtoGetWorkflowRunRequest;
export type ListWorkflowRunsRequest = ProtoListWorkflowRunsRequest;
export type ListWorkflowRunsResponse = ProtoListWorkflowRunsResponse;
export type WorkflowRunSignal = ProtoWorkflowRunSignal;
export type DeliverWorkflowEventRequest = ProtoDeliverWorkflowEventRequest;
export type WorkflowEventDeliveryResult = ProtoWorkflowEventDeliveryResult;
export type DeliverWorkflowEventResponse = ProtoDeliverWorkflowEventResponse;
export type WorkflowRunEvent = ProtoWorkflowRunEvent;
export type GetWorkflowRunEventsRequest = ProtoGetWorkflowRunEventsRequest;
export type ListWorkflowRunEventsResponse =
  ProtoListWorkflowRunEventsResponse;
export type GetWorkflowRunOutputRequest = ProtoGetWorkflowRunOutputRequest;
export type WorkflowRunOutput = ProtoWorkflowRunOutput;
export type WorkflowHostActionSelector = ProtoWorkflowHostActionSelector;
export type WorkflowPluginActionPayload = ProtoWorkflowPluginActionPayload;
export type WorkflowAgentTurnPayload = ProtoWorkflowAgentTurnPayload;
export type InvokeWorkflowActionRequest = ProtoInvokeWorkflowActionRequest;
export type WorkflowActionResult = ProtoWorkflowActionResult;
export type ManagedWorkflowDeployment = ProtoManagedWorkflowDeployment;
export type ManagedWorkflowRun = ProtoManagedWorkflowRun;
export type ManagedWorkflowRunSignal = ProtoManagedWorkflowRunSignal;

export function boundWorkflowTarget(
  input: MessageInitShape<typeof BoundWorkflowTargetSchema> = {},
): BoundWorkflowTarget {
  return create(BoundWorkflowTargetSchema, input);
}

export function workflowStep(
  input: MessageInitShape<typeof WorkflowStepSchema> = {},
): WorkflowStep {
  return create(WorkflowStepSchema, input);
}

export function workflowStepPluginCall(
  input: MessageInitShape<typeof WorkflowStepPluginCallSchema> = {},
): WorkflowStepPluginCall {
  return create(WorkflowStepPluginCallSchema, input);
}

export function workflowStepDelivery(
  input: MessageInitShape<typeof WorkflowStepDeliverySchema> = {},
): WorkflowStepDelivery {
  return create(WorkflowStepDeliverySchema, input);
}

export function workflowStepAgentTurn(
  input: MessageInitShape<typeof WorkflowStepAgentTurnSchema> = {},
): WorkflowStepAgentTurn {
  return create(WorkflowStepAgentTurnSchema, input);
}

export function workflowAgentMessage(
  input: MessageInitShape<typeof WorkflowAgentMessageSchema> = {},
): WorkflowAgentMessage {
  return create(WorkflowAgentMessageSchema, input);
}

export function workflowText(
  input: MessageInitShape<typeof WorkflowTextSchema> | string = {},
): WorkflowText {
  if (typeof input === "string") {
    return create(WorkflowTextSchema, { template: input });
  }
  return create(WorkflowTextSchema, input);
}

export function workflowStepWhen(
  input: MessageInitShape<typeof WorkflowStepWhenSchema> = {},
): WorkflowStepWhen {
  return create(WorkflowStepWhenSchema, input);
}

export function workflowValue(
  input: MessageInitShape<typeof WorkflowValueSchema> = {},
): WorkflowValue {
  return create(WorkflowValueSchema, input);
}

export function workflowLiteral(value: JsonInput): WorkflowValue {
  return create(WorkflowValueSchema, {
    kind: { case: "literal", value: valueFromJson(value) },
  });
}

export function workflowTemplate(template: string): WorkflowValue {
  return create(WorkflowValueSchema, {
    kind: {
      case: "template",
      value: create(WorkflowTextSchema, { template }),
    },
  });
}

export function workflowRunInput(path = ""): WorkflowValue {
  return workflowPathValue("runInput", path);
}

export function workflowSignalPayload(path = ""): WorkflowValue {
  return workflowPathValue("signalPayload", path);
}

export function workflowStepOutput(stepId: string, path = ""): WorkflowValue {
  return create(WorkflowValueSchema, {
    kind: {
      case: "stepOutput",
      value: create(WorkflowStepOutputSourceSchema, { stepId, path }),
    },
  });
}

export function workflowObject(
  fields: Record<string, WorkflowValue> = {},
): WorkflowValue {
  return create(WorkflowValueSchema, {
    kind: {
      case: "object",
      value: create(WorkflowObjectSchema, { fields }),
    },
  });
}

export function workflowArray(values: readonly WorkflowValue[] = []): WorkflowValue {
  return create(WorkflowValueSchema, {
    kind: {
      case: "array",
      value: create(WorkflowArraySchema, { values: [...values] }),
    },
  });
}

export function workflowPathSource(
  input: MessageInitShape<typeof WorkflowPathSourceSchema> | string = {},
): WorkflowPathSource {
  if (typeof input === "string") {
    return create(WorkflowPathSourceSchema, { path: input });
  }
  return create(WorkflowPathSourceSchema, input);
}

export function workflowStepOutputSource(
  input: MessageInitShape<typeof WorkflowStepOutputSourceSchema> = {},
): WorkflowStepOutputSource {
  return create(WorkflowStepOutputSourceSchema, input);
}

export function workflowActor(
  input: MessageInitShape<typeof WorkflowActorSchema> = {},
): WorkflowActor {
  return create(WorkflowActorSchema, input);
}

export function workflowRunAsSubject(
  input: MessageInitShape<typeof WorkflowRunAsSubjectSchema> = {},
): WorkflowRunAsSubject {
  return create(WorkflowRunAsSubjectSchema, input);
}

export function workflowEvent(
  input: MessageInitShape<typeof WorkflowEventSchema> = {},
): WorkflowEvent {
  return create(WorkflowEventSchema, input);
}

export function workflowEventMatch(
  input: MessageInitShape<typeof WorkflowEventMatchSchema> = {},
): WorkflowEventMatch {
  return create(WorkflowEventMatchSchema, input);
}

export function workflowManualActivation(
  input: MessageInitShape<typeof WorkflowManualActivationSchema> = {},
): WorkflowManualActivation {
  return create(WorkflowManualActivationSchema, input);
}

export function workflowScheduleActivation(
  input: MessageInitShape<typeof WorkflowScheduleActivationSchema> = {},
): WorkflowScheduleActivation {
  return create(WorkflowScheduleActivationSchema, input);
}

export function workflowEventActivation(
  input: MessageInitShape<typeof WorkflowEventActivationSchema> = {},
): WorkflowEventActivation {
  return create(WorkflowEventActivationSchema, input);
}

export function workflowActivation(
  input: MessageInitShape<typeof WorkflowActivationSchema> = {},
): WorkflowActivation {
  return create(WorkflowActivationSchema, input);
}

export function workflowAccessPermission(
  input: MessageInitShape<typeof WorkflowAccessPermissionSchema> = {},
): WorkflowAccessPermission {
  return create(WorkflowAccessPermissionSchema, input);
}

export function workflowDeploymentSpec(
  input: MessageInitShape<typeof WorkflowDeploymentSpecSchema> = {},
): WorkflowDeploymentSpec {
  return create(WorkflowDeploymentSpecSchema, input);
}

export function workflowActionDescriptor(
  input: MessageInitShape<typeof WorkflowActionDescriptorSchema> = {},
): WorkflowActionDescriptor {
  return create(WorkflowActionDescriptorSchema, input);
}

export function workflowActionTable(
  input: MessageInitShape<typeof WorkflowActionTableSchema> = {},
): WorkflowActionTable {
  return create(WorkflowActionTableSchema, input);
}

export function workflowUnsupportedFeature(
  input: MessageInitShape<typeof WorkflowUnsupportedFeatureSchema> = {},
): WorkflowUnsupportedFeature {
  return create(WorkflowUnsupportedFeatureSchema, input);
}

export function planWorkflowResponse(
  input: MessageInitShape<typeof PlanWorkflowResponseSchema> = {},
): PlanWorkflowResponse {
  return create(PlanWorkflowResponseSchema, input);
}

export function workflowDeploymentBinding(
  input: MessageInitShape<typeof WorkflowDeploymentBindingSchema> = {},
): WorkflowDeploymentBinding {
  return create(WorkflowDeploymentBindingSchema, input);
}

export function workflowDeployment(
  input: MessageInitShape<typeof WorkflowDeploymentSchema> = {},
): WorkflowDeployment {
  return create(WorkflowDeploymentSchema, input);
}

export function workflowManualTrigger(
  input: MessageInitShape<typeof WorkflowManualTriggerSchema> = {},
): WorkflowManualTrigger {
  return create(WorkflowManualTriggerSchema, input);
}

export function workflowScheduleTrigger(
  input: MessageInitShape<typeof WorkflowScheduleTriggerSchema> = {},
): WorkflowScheduleTrigger {
  return create(WorkflowScheduleTriggerSchema, input);
}

export function workflowEventTrigger(
  input: MessageInitShape<typeof WorkflowEventTriggerSchema> = {},
): WorkflowEventTrigger {
  return create(WorkflowEventTriggerSchema, input);
}

export function workflowRunTrigger(
  input: MessageInitShape<typeof WorkflowRunTriggerSchema> = {},
): WorkflowRunTrigger {
  return create(WorkflowRunTriggerSchema, input);
}

export function workflowSignal(
  input: MessageInitShape<typeof WorkflowSignalSchema> = {},
): WorkflowSignal {
  return create(WorkflowSignalSchema, input);
}

export function workflowOutputSummary(
  input: MessageInitShape<typeof WorkflowOutputSummarySchema> = {},
): WorkflowOutputSummary {
  return create(WorkflowOutputSummarySchema, input);
}

export function workflowRunError(
  input: MessageInitShape<typeof WorkflowRunErrorSchema> = {},
): WorkflowRunError {
  return create(WorkflowRunErrorSchema, input);
}

export function workflowStepState(
  input: MessageInitShape<typeof WorkflowStepStateSchema> = {},
): WorkflowStepState {
  return create(WorkflowStepStateSchema, input);
}

export function workflowRun(
  input: MessageInitShape<typeof WorkflowRunSchema> = {},
): WorkflowRun {
  return create(WorkflowRunSchema, input);
}

export function workflowRunSignal(
  input: MessageInitShape<typeof WorkflowRunSignalSchema> = {},
): WorkflowRunSignal {
  return create(WorkflowRunSignalSchema, input);
}

export function workflowEventDeliveryResult(
  input: MessageInitShape<typeof WorkflowEventDeliveryResultSchema> = {},
): WorkflowEventDeliveryResult {
  return create(WorkflowEventDeliveryResultSchema, input);
}

export function deliverWorkflowEventResponse(
  input: MessageInitShape<typeof DeliverWorkflowEventResponseSchema> = {},
): DeliverWorkflowEventResponse {
  return create(DeliverWorkflowEventResponseSchema, input);
}

export function workflowRunEvent(
  input: MessageInitShape<typeof WorkflowRunEventSchema> = {},
): WorkflowRunEvent {
  return create(WorkflowRunEventSchema, input);
}

export function workflowRunOutput(
  input: MessageInitShape<typeof WorkflowRunOutputSchema> = {},
): WorkflowRunOutput {
  return create(WorkflowRunOutputSchema, input);
}

export function workflowHostActionSelector(
  input: MessageInitShape<typeof WorkflowHostActionSelectorSchema> = {},
): WorkflowHostActionSelector {
  return create(WorkflowHostActionSelectorSchema, input);
}

export function workflowPluginActionPayload(
  input: MessageInitShape<typeof WorkflowPluginActionPayloadSchema> = {},
): WorkflowPluginActionPayload {
  return create(WorkflowPluginActionPayloadSchema, input);
}

export function workflowAgentTurnPayload(
  input: MessageInitShape<typeof WorkflowAgentTurnPayloadSchema> = {},
): WorkflowAgentTurnPayload {
  return create(WorkflowAgentTurnPayloadSchema, input);
}

export function invokeWorkflowActionRequest(
  input: MessageInitShape<typeof InvokeWorkflowActionRequestSchema> = {},
): InvokeWorkflowActionRequest {
  return create(InvokeWorkflowActionRequestSchema, input);
}

export function workflowActionResult(
  input: MessageInitShape<typeof WorkflowActionResultSchema> = {},
): WorkflowActionResult {
  return create(WorkflowActionResultSchema, input);
}

export const boundWorkflowTargetToProto = boundWorkflowTarget;
export const boundWorkflowTargetFromProto = boundWorkflowTarget;
export const workflowDeploymentSpecToProto = workflowDeploymentSpec;
export const workflowDeploymentSpecFromProto = workflowDeploymentSpec;
export const workflowSignalToProto = workflowSignal;
export const workflowSignalFromProto = workflowSignal;
export const workflowEventToProto = workflowEvent;
export const workflowEventFromProto = workflowEvent;

export function planWorkflowResponseFromProto(
  input: MessageInitShape<typeof PlanWorkflowResponseSchema>,
): PlanWorkflowResponse {
  return create(PlanWorkflowResponseSchema, input);
}

export function managedWorkflowDeploymentFromProto(
  input: ProtoManagedWorkflowDeployment,
): ManagedWorkflowDeployment {
  return input;
}

export function managedWorkflowRunFromProto(
  input: ProtoManagedWorkflowRun,
): ManagedWorkflowRun {
  return input;
}

export function managedWorkflowRunSignalFromProto(
  input: ProtoManagedWorkflowRunSignal,
): ManagedWorkflowRunSignal {
  return input;
}

export function workflowManagerDeliverEventResponseFromProto(
  input: ProtoWorkflowManagerDeliverEventResponse,
): DeliverWorkflowEventResponse {
  return create(DeliverWorkflowEventResponseSchema, {
    results: input.results,
  });
}

/** Handlers and runtime metadata for a workflow provider. */
export interface WorkflowProviderOptions extends ProviderBaseOptions {
  planWorkflow: (request: PlanWorkflowRequest) => MaybePromise<PlanWorkflowResponse>;
  applyDeployment: (
    request: ApplyWorkflowDeploymentRequest,
  ) => MaybePromise<WorkflowDeployment>;
  getDeployment: (
    request: GetWorkflowDeploymentRequest,
  ) => MaybePromise<WorkflowDeployment>;
  listDeployments: (
    request: ListWorkflowDeploymentsRequest,
  ) => MaybePromise<readonly WorkflowDeployment[] | ListWorkflowDeploymentsResponse>;
  deleteDeployment: (request: DeleteWorkflowDeploymentRequest) => MaybePromise<void>;
  setDeploymentPaused: (
    request: SetWorkflowDeploymentPausedRequest,
  ) => MaybePromise<WorkflowDeployment>;
  setActivationPaused: (
    request: SetWorkflowActivationPausedRequest,
  ) => MaybePromise<WorkflowDeployment>;
  startRun: (request: StartWorkflowRunRequest) => MaybePromise<WorkflowRun>;
  signalRun: (request: SignalWorkflowRunRequest) => MaybePromise<WorkflowRunSignal>;
  signalOrStartRun: (
    request: SignalOrStartWorkflowRunRequest,
  ) => MaybePromise<WorkflowRunSignal>;
  cancelRun: (request: CancelWorkflowRunRequest) => MaybePromise<WorkflowRun>;
  deliverEvent: (
    request: DeliverWorkflowEventRequest,
  ) => MaybePromise<DeliverWorkflowEventResponse>;
  getRun: (request: GetWorkflowRunRequest) => MaybePromise<WorkflowRun>;
  listRuns: (
    request: ListWorkflowRunsRequest,
  ) => MaybePromise<readonly WorkflowRun[] | ListWorkflowRunsResponse>;
  getRunEvents: (
    request: GetWorkflowRunEventsRequest,
  ) => MaybePromise<readonly WorkflowRunEvent[] | ListWorkflowRunEventsResponse>;
  getRunOutput: (request: GetWorkflowRunOutputRequest) => MaybePromise<WorkflowRunOutput>;
}

/** Runtime provider implementation for the Gestalt workflow host contract. */
export class WorkflowProvider extends ProviderBase {
  readonly kind = "workflow" as const;

  constructor(private readonly options: WorkflowProviderOptions) {
    super(options);
  }

  async planWorkflow(request: PlanWorkflowRequest): Promise<PlanWorkflowResponse> {
    return await this.options.planWorkflow(request);
  }

  async applyDeployment(
    request: ApplyWorkflowDeploymentRequest,
  ): Promise<WorkflowDeployment> {
    return await this.options.applyDeployment(request);
  }

  async getDeployment(
    request: GetWorkflowDeploymentRequest,
  ): Promise<WorkflowDeployment> {
    return await this.options.getDeployment(request);
  }

  async listDeployments(
    request: ListWorkflowDeploymentsRequest,
  ): Promise<readonly WorkflowDeployment[] | ListWorkflowDeploymentsResponse> {
    return await this.options.listDeployments(request);
  }

  async deleteDeployment(request: DeleteWorkflowDeploymentRequest): Promise<void> {
    await this.options.deleteDeployment(request);
  }

  async setDeploymentPaused(
    request: SetWorkflowDeploymentPausedRequest,
  ): Promise<WorkflowDeployment> {
    return await this.options.setDeploymentPaused(request);
  }

  async setActivationPaused(
    request: SetWorkflowActivationPausedRequest,
  ): Promise<WorkflowDeployment> {
    return await this.options.setActivationPaused(request);
  }

  async startRun(request: StartWorkflowRunRequest): Promise<WorkflowRun> {
    return await this.options.startRun(request);
  }

  async signalRun(request: SignalWorkflowRunRequest): Promise<WorkflowRunSignal> {
    return await this.options.signalRun(request);
  }

  async signalOrStartRun(
    request: SignalOrStartWorkflowRunRequest,
  ): Promise<WorkflowRunSignal> {
    return await this.options.signalOrStartRun(request);
  }

  async cancelRun(request: CancelWorkflowRunRequest): Promise<WorkflowRun> {
    return await this.options.cancelRun(request);
  }

  async deliverEvent(
    request: DeliverWorkflowEventRequest,
  ): Promise<DeliverWorkflowEventResponse> {
    return await this.options.deliverEvent(request);
  }

  async getRun(request: GetWorkflowRunRequest): Promise<WorkflowRun> {
    return await this.options.getRun(request);
  }

  async listRuns(
    request: ListWorkflowRunsRequest,
  ): Promise<readonly WorkflowRun[] | ListWorkflowRunsResponse> {
    return await this.options.listRuns(request);
  }

  async getRunEvents(
    request: GetWorkflowRunEventsRequest,
  ): Promise<readonly WorkflowRunEvent[] | ListWorkflowRunEventsResponse> {
    return await this.options.getRunEvents(request);
  }

  async getRunOutput(
    request: GetWorkflowRunOutputRequest,
  ): Promise<WorkflowRunOutput> {
    return await this.options.getRunOutput(request);
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
      "planWorkflow" in value &&
      "applyDeployment" in value &&
      "getDeployment" in value &&
      "listDeployments" in value &&
      "deleteDeployment" in value &&
      "setDeploymentPaused" in value &&
      "setActivationPaused" in value &&
      "startRun" in value &&
      "signalRun" in value &&
      "signalOrStartRun" in value &&
      "cancelRun" in value &&
      "deliverEvent" in value &&
      "getRun" in value &&
      "listRuns" in value &&
      "getRunEvents" in value &&
      "getRunOutput" in value)
  );
}

/** Client for invoking workflow actions from workflow provider code. */
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

  /** Invokes one workflow action through the workflow host service. */
  async invokeWorkflowAction(
    input: MessageInitShape<typeof InvokeWorkflowActionRequestSchema>,
  ): Promise<WorkflowActionResult> {
    return workflowActionResult(
      await this.client.invokeWorkflowAction(invokeWorkflowActionRequest(input)),
    );
  }
}

/** Builds the Connect service implementation used by the TypeScript runtime. */
export function createWorkflowProviderService(
  provider: WorkflowProvider,
): WorkflowProviderServiceImpl {
  return {
    async planWorkflow(request) {
      return create(
        PlanWorkflowResponseSchema,
        await invokeWorkflowProvider("plan workflow", () =>
          provider.planWorkflow(request),
        ),
      );
    },
    async applyWorkflowDeployment(request) {
      return create(
        WorkflowDeploymentSchema,
        await invokeWorkflowProvider("apply deployment", () =>
          provider.applyDeployment(create(ApplyWorkflowDeploymentRequestSchema, request)),
        ),
      );
    },
    async getWorkflowDeployment(request) {
      return create(
        WorkflowDeploymentSchema,
        await invokeWorkflowProvider("get deployment", () =>
          provider.getDeployment(create(GetWorkflowDeploymentRequestSchema, request)),
        ),
      );
    },
    async listWorkflowDeployments(request) {
      const response = await invokeWorkflowProvider("list deployments", () =>
        provider.listDeployments(request),
      );
      const result = listDeploymentsResult(response);
      return create(ListWorkflowDeploymentsResponseSchema, {
        deployments: [...result.deployments],
        nextPageToken: result.nextPageToken,
      });
    },
    async deleteWorkflowDeployment(request) {
      await invokeWorkflowProvider("delete deployment", () =>
        provider.deleteDeployment(create(DeleteWorkflowDeploymentRequestSchema, request)),
      );
      return create(EmptySchema, {});
    },
    async setWorkflowDeploymentPaused(request) {
      return create(
        WorkflowDeploymentSchema,
        await invokeWorkflowProvider("set deployment paused", () =>
          provider.setDeploymentPaused(create(SetWorkflowDeploymentPausedRequestSchema, request)),
        ),
      );
    },
    async setWorkflowActivationPaused(request) {
      return create(
        WorkflowDeploymentSchema,
        await invokeWorkflowProvider("set activation paused", () =>
          provider.setActivationPaused(create(SetWorkflowActivationPausedRequestSchema, request)),
        ),
      );
    },
    async startWorkflowRun(request) {
      return create(
        WorkflowRunSchema,
        await invokeWorkflowProvider("start run", () =>
          provider.startRun(create(StartWorkflowRunRequestSchema, request)),
        ),
      );
    },
    async signalWorkflowRun(request) {
      return create(
        WorkflowRunSignalSchema,
        await invokeWorkflowProvider("signal run", () =>
          provider.signalRun(create(SignalWorkflowRunRequestSchema, request)),
        ),
      );
    },
    async signalOrStartWorkflowRun(request) {
      return create(
        WorkflowRunSignalSchema,
        await invokeWorkflowProvider("signal or start run", () =>
          provider.signalOrStartRun(create(SignalOrStartWorkflowRunRequestSchema, request)),
        ),
      );
    },
    async cancelWorkflowRun(request) {
      return create(
        WorkflowRunSchema,
        await invokeWorkflowProvider("cancel run", () =>
          provider.cancelRun(create(CancelWorkflowRunRequestSchema, request)),
        ),
      );
    },
    async deliverWorkflowEvent(request) {
      return create(
        DeliverWorkflowEventResponseSchema,
        await invokeWorkflowProvider("deliver event", () =>
          provider.deliverEvent(create(DeliverWorkflowEventRequestSchema, request)),
        ),
      );
    },
    async getWorkflowRun(request) {
      return create(
        WorkflowRunSchema,
        await invokeWorkflowProvider("get run", () =>
          provider.getRun(create(GetWorkflowRunRequestSchema, request)),
        ),
      );
    },
    async listWorkflowRuns(request) {
      const response = await invokeWorkflowProvider("list runs", () =>
        provider.listRuns(request),
      );
      const result = listRunsResult(response);
      return create(ListWorkflowRunsResponseSchema, {
        runs: [...result.runs],
        nextPageToken: result.nextPageToken,
      });
    },
    async getWorkflowRunEvents(request) {
      const response = await invokeWorkflowProvider("get run events", () =>
        provider.getRunEvents(create(GetWorkflowRunEventsRequestSchema, request)),
      );
      const result = listRunEventsResult(response);
      return create(ListWorkflowRunEventsResponseSchema, {
        events: [...result.events],
        nextPageToken: result.nextPageToken,
      });
    },
    async getWorkflowRunOutput(request) {
      return create(
        WorkflowRunOutputSchema,
        await invokeWorkflowProvider("get run output", () =>
          provider.getRunOutput(create(GetWorkflowRunOutputRequestSchema, request)),
        ),
      );
    },
  };
}

function workflowPathValue(
  kind: "runInput" | "signalPayload",
  path: string,
): WorkflowValue {
  return create(WorkflowValueSchema, {
    kind: {
      case: kind,
      value: create(WorkflowPathSourceSchema, { path }),
    },
  });
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

function listDeploymentsResult(
  value: readonly WorkflowDeployment[] | ListWorkflowDeploymentsResponse,
): ListWorkflowDeploymentsResponse {
  return "deployments" in value
    ? value
    : create(ListWorkflowDeploymentsResponseSchema, { deployments: [...value] });
}

function listRunsResult(
  value: readonly WorkflowRun[] | ListWorkflowRunsResponse,
): ListWorkflowRunsResponse {
  return "runs" in value
    ? value
    : create(ListWorkflowRunsResponseSchema, { runs: [...value] });
}

function listRunEventsResult(
  value: readonly WorkflowRunEvent[] | ListWorkflowRunEventsResponse,
): ListWorkflowRunEventsResponse {
  return "events" in value
    ? value
    : create(ListWorkflowRunEventsResponseSchema, { events: [...value] });
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
