import {
  createClient,
  type Client,
} from "@connectrpc/connect";

import {
  Workflow as WorkflowProviderService,
} from "./internal/gen/v1/workflow_pb.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  ENV_HOST_SERVICE_SOCKET,
  ENV_HOST_SERVICE_TOKEN,
} from "./host-service.ts";
import type { Request, SubjectInput } from "./api.ts";
import { jsonFromValue, structFromObject, type JsonObjectInput } from "./protocol.ts";
import {
  subjectToProto,
  workflowDefinitionFromProto,
  workflowDefinitionSpecToProto,
  workflowEventFromProto,
  workflowEventToProto,
  workflowRunEventFromProto,
  workflowRunFromProto,
  workflowRunSignalFromProto,
  workflowSignalToProto,
  type GetWorkflowProviderRunOutputResponse,
  type SignalWorkflowRunResponse,
  type WorkflowDefinition,
  type WorkflowDefinitionSpec,
  type WorkflowEvent,
  type WorkflowRun,
  type WorkflowRunEvent,
  type WorkflowRunStatus,
  type WorkflowSignal,
} from "./providers/workflow.ts";

/** Shape accepted when applying a workflow definition. */
export interface WorkflowApplyDefinition {
  providerName: string;
  spec?: WorkflowDefinitionSpec | undefined;
  idempotencyKey?: string | undefined;
  requestedBySubjectId?: string | undefined;
}

/** Shape accepted when fetching a workflow definition. */
export interface WorkflowGetDefinition {
  definitionId: string;
}

/** Shape accepted when listing workflow definitions. */
export interface WorkflowListDefinitions {}

/** Shape accepted when pausing or resuming a workflow definition. */
export interface WorkflowSetDefinitionPaused {
  definitionId: string;
  paused: boolean;
  requestedBySubjectId?: string | undefined;
}

/** Shape accepted when pausing or resuming a workflow activation. */
export interface WorkflowSetActivationPaused {
  definitionId: string;
  activationId: string;
  paused: boolean;
  requestedBySubjectId?: string | undefined;
}

/** Shape accepted when deleting a workflow definition. */
export interface WorkflowDeleteDefinition {
  definitionId: string;
}

/** Shape accepted when starting a workflow run. */
export interface WorkflowStartRun {
  providerName: string;
  definitionId: string;
  expectedDefinitionGeneration?: bigint | number | undefined;
  input?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  workflowKey?: string | undefined;
  createdBySubjectId?: string | undefined;
  runAs?: SubjectInput | undefined;
}

/** Shape accepted when fetching one workflow run. */
export interface WorkflowGetRun {
  runId: string;
}

/** Shape accepted when listing workflow runs. */
export interface WorkflowListRuns {
  pageSize?: number | undefined;
  pageToken?: string | undefined;
  status?: WorkflowRunStatus | undefined;
  targetApp?: string | undefined;
}

/** Shape accepted when fetching workflow run events. */
export interface WorkflowGetRunEvents {
  runId: string;
}

/** Shape accepted when fetching the terminal workflow run output. */
export interface WorkflowGetRunOutput {
  runId: string;
}

/** Shape accepted when canceling a workflow run. */
export interface WorkflowCancelRun {
  runId: string;
  reason?: string | undefined;
}

/** Shape accepted when signaling an existing workflow run. */
export interface WorkflowSignalRun {
  runId: string;
  signal?: WorkflowSignal | undefined;
}

/** Shape accepted when signaling a run or starting it if missing. */
export interface WorkflowSignalOrStartRun {
  providerName: string;
  workflowKey: string;
  definitionId: string;
  expectedDefinitionGeneration?: bigint | number | undefined;
  input?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  createdBySubjectId?: string | undefined;
  runAs?: SubjectInput | undefined;
  signal?: WorkflowSignal | undefined;
}

/** Shape accepted when delivering an app event to workflow activations. */
export interface WorkflowDeliverEvent {
  appName?: string | undefined;
  providerName?: string | undefined;
  event?: WorkflowEvent | undefined;
  deliveredBySubjectId?: string | undefined;
}

/** Fakeable client contract for workflow calls. */
export interface Workflow {
  applyDefinition(
    request: WorkflowApplyDefinition,
  ): Promise<WorkflowDefinition>;
  getDefinition(
    request: WorkflowGetDefinition,
  ): Promise<WorkflowDefinition>;
  listDefinitions(
    request?: WorkflowListDefinitions,
  ): Promise<readonly WorkflowDefinition[]>;
  setDefinitionPaused(
    request: WorkflowSetDefinitionPaused,
  ): Promise<WorkflowDefinition>;
  setActivationPaused(
    request: WorkflowSetActivationPaused,
  ): Promise<WorkflowDefinition>;
  deleteDefinition(request: WorkflowDeleteDefinition): Promise<void>;
  startRun(request: WorkflowStartRun): Promise<WorkflowRun>;
  getRun(request: WorkflowGetRun): Promise<WorkflowRun>;
  listRuns(request?: WorkflowListRuns): Promise<{
    runs: readonly WorkflowRun[];
    nextPageToken?: string | undefined;
  }>;
  getRunEvents(request: WorkflowGetRunEvents): Promise<readonly WorkflowRunEvent[]>;
  getRunOutput(request: WorkflowGetRunOutput): Promise<GetWorkflowProviderRunOutputResponse>;
  cancelRun(request: WorkflowCancelRun): Promise<WorkflowRun>;
  signalRun(
    request: WorkflowSignalRun,
  ): Promise<SignalWorkflowRunResponse>;
  signalOrStartRun(
    request: WorkflowSignalOrStartRun,
  ): Promise<SignalWorkflowRunResponse>;
  deliverEvent(request: WorkflowDeliverEvent): Promise<WorkflowEvent>;
}

/**
 * Client for applying workflow definitions and controlling durable runs.
 *
 * Mutating calls reuse the request idempotency key unless the call provides one
 * explicitly.
 */
class WorkflowImpl implements Workflow {
  private readonly client: Client<typeof WorkflowProviderService>;
  private readonly context: Request["__requestContext"];
  private readonly idempotencyKey: string;

  constructor(request: Request);
  constructor(request: Request) {
    this.context = request.__requestContext;
    this.idempotencyKey = request.idempotencyKey.trim();

    const target = process.env[ENV_HOST_SERVICE_SOCKET]?.trim();
    if (!target) {
      throw new Error(
        `workflow: ${ENV_HOST_SERVICE_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_HOST_SERVICE_TOKEN]?.trim() ?? "";

    const transport = createHostServiceGrpcTransport(
      parseHostServiceTarget("workflow", target),
      hostServiceMetadataInterceptors(relayToken, ""),
    );
    this.client = createClient(WorkflowProviderService, transport);
  }

  /** Applies a durable workflow definition atomically. */
  async applyDefinition(
    request: WorkflowApplyDefinition,
  ): Promise<WorkflowDefinition> {
    return requireDefinition(
      workflowDefinitionFromProto(
        await this.client.applyDefinition({
          providerName: request.providerName,
          spec: workflowDefinitionSpecToProto(request.spec),
          context: this.context,
          idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
          requestedBySubjectId: request.requestedBySubjectId ?? "",
        }),
      ),
      "Workflow.applyDefinition returned no definition",
    );
  }

  /** Fetches one workflow definition. */
  async getDefinition(
    request: WorkflowGetDefinition,
  ): Promise<WorkflowDefinition> {
    return requireDefinition(
      workflowDefinitionFromProto(
        await this.client.getDefinition({
          definitionId: request.definitionId,
          context: this.context,
        }),
      ),
      "Workflow.getDefinition returned no definition",
    );
  }

  /** Lists workflow definitions visible to the request context. */
  async listDefinitions(
    _request: WorkflowListDefinitions = {},
  ): Promise<readonly WorkflowDefinition[]> {
    const response = await this.client.listDefinitions({
      context: this.context,
    });
    return response.definitions.map((definition) =>
      requireDefinition(workflowDefinitionFromProto(definition), "Workflow.listDefinitions returned an empty definition")
    );
  }

  /** Pauses or resumes a workflow definition. */
  async setDefinitionPaused(
    request: WorkflowSetDefinitionPaused,
  ): Promise<WorkflowDefinition> {
    return requireDefinition(
      workflowDefinitionFromProto(
        await this.client.setDefinitionPaused({
          definitionId: request.definitionId,
          paused: request.paused,
          context: this.context,
          requestedBySubjectId: request.requestedBySubjectId ?? "",
        }),
      ),
      "Workflow.setDefinitionPaused returned no definition",
    );
  }

  /** Pauses or resumes one activation within a workflow definition. */
  async setActivationPaused(
    request: WorkflowSetActivationPaused,
  ): Promise<WorkflowDefinition> {
    return requireDefinition(
      workflowDefinitionFromProto(
        await this.client.setActivationPaused({
          definitionId: request.definitionId,
          activationId: request.activationId,
          paused: request.paused,
          context: this.context,
          requestedBySubjectId: request.requestedBySubjectId ?? "",
        }),
      ),
      "Workflow.setActivationPaused returned no definition",
    );
  }

  /** Deletes a workflow definition. */
  async deleteDefinition(
    request: WorkflowDeleteDefinition,
  ): Promise<void> {
    await this.client.deleteDefinition({
      definitionId: request.definitionId,
      context: this.context,
    });
  }

  /** Starts a workflow run immediately. */
  async startRun(
    request: WorkflowStartRun,
  ): Promise<WorkflowRun> {
    return requireRun(
      workflowRunFromProto(
        await this.client.startRun({
          providerName: request.providerName,
          definitionId: request.definitionId,
          expectedDefinitionGeneration: generationToBigInt(request.expectedDefinitionGeneration),
          input: structFromObject(request.input),
          idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
          workflowKey: request.workflowKey ?? "",
          context: this.context,
          createdBySubjectId: request.createdBySubjectId ?? "",
          runAs: subjectToProto(request.runAs),
        }),
      ),
      "Workflow.startRun returned no run",
    );
  }

  /** Fetches one workflow run. */
  async getRun(
    request: WorkflowGetRun,
  ): Promise<WorkflowRun> {
    return requireRun(
      workflowRunFromProto(
        await this.client.getRun({
          runId: request.runId,
          context: this.context,
        }),
      ),
      "Workflow.getRun returned no run",
    );
  }

  /** Lists workflow runs visible to the request context. */
  async listRuns(
    request: WorkflowListRuns = {},
  ): Promise<{ runs: readonly WorkflowRun[]; nextPageToken?: string | undefined }> {
    const response = await this.client.listRuns({
      pageSize: request.pageSize ?? 0,
      pageToken: request.pageToken ?? "",
      status: request.status ?? 0,
      targetApp: request.targetApp ?? "",
      context: this.context,
    });
    return {
      runs: response.runs.map((run) => requireRun(workflowRunFromProto(run), "Workflow.listRuns returned an empty run")),
      nextPageToken: response.nextPageToken || undefined,
    };
  }

  /** Fetches workflow run events. */
  async getRunEvents(
    request: WorkflowGetRunEvents,
  ): Promise<readonly WorkflowRunEvent[]> {
    const response = await this.client.getRunEvents({
      runId: request.runId,
      context: this.context,
    });
    return response.events.map((event) =>
      requireRunEvent(workflowRunEventFromProto(event), "Workflow.getRunEvents returned an empty event")
    );
  }

  /** Fetches the terminal workflow run output. */
  async getRunOutput(
    request: WorkflowGetRunOutput,
  ): Promise<GetWorkflowProviderRunOutputResponse> {
    const response = await this.client.getRunOutput({
      runId: request.runId,
      context: this.context,
    });
    return {
      output: response.output === undefined
        ? undefined
        : jsonFromValue(response.output),
    };
  }

  /** Cancels a workflow run. */
  async cancelRun(
    request: WorkflowCancelRun,
  ): Promise<WorkflowRun> {
    return requireRun(
      workflowRunFromProto(
        await this.client.cancelRun({
          runId: request.runId,
          reason: request.reason ?? "",
          context: this.context,
        }),
      ),
      "Workflow.cancelRun returned no run",
    );
  }

  /** Signals an existing workflow run. */
  async signalRun(
    request: WorkflowSignalRun,
  ): Promise<SignalWorkflowRunResponse> {
    return requireSignalResponse(
      workflowRunSignalFromProto(
        await this.client.signalRun({
          runId: request.runId,
          signal: workflowSignalToProto(request.signal),
          context: this.context,
        }),
      ),
      "Workflow.signalRun returned no response",
    );
  }

  /** Signals a workflow run, or starts it when no run exists for the key. */
  async signalOrStartRun(
    request: WorkflowSignalOrStartRun,
  ): Promise<SignalWorkflowRunResponse> {
    return requireSignalResponse(
      workflowRunSignalFromProto(
        await this.client.signalOrStartRun({
          providerName: request.providerName,
          workflowKey: request.workflowKey,
          definitionId: request.definitionId,
          expectedDefinitionGeneration: generationToBigInt(request.expectedDefinitionGeneration),
          input: structFromObject(request.input),
          idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
          signal: workflowSignalToProto(request.signal),
          context: this.context,
          createdBySubjectId: request.createdBySubjectId ?? "",
          runAs: subjectToProto(request.runAs),
        }),
      ),
      "Workflow.signalOrStartRun returned no response",
    );
  }

  /** Delivers an event into workflow activation matching. */
  async deliverEvent(
    request: WorkflowDeliverEvent,
  ): Promise<WorkflowEvent> {
    return requireEvent(
      workflowEventFromProto(
        await this.client.deliverEvent({
          appName: request.appName ?? "",
          event: workflowEventToProto(request.event),
          deliveredBySubjectId: request.deliveredBySubjectId ?? "",
          context: this.context,
          providerName: request.providerName ?? "",
        }),
      ),
      "Workflow.deliverEvent returned no event",
    );
  }
}

export const Workflow = WorkflowImpl;

function generationToBigInt(value: bigint | number | undefined): bigint {
  if (value === undefined) {
    return 0n;
  }
  return typeof value === "bigint" ? value : BigInt(value);
}

function requireDefinition(value: WorkflowDefinition | undefined, message: string): WorkflowDefinition {
  if (value === undefined) {
    throw new Error(message);
  }
  return value;
}

function requireRun(value: WorkflowRun | undefined, message: string): WorkflowRun {
  if (value === undefined) {
    throw new Error(message);
  }
  return value;
}

function requireRunEvent(value: WorkflowRunEvent | undefined, message: string): WorkflowRunEvent {
  if (value === undefined) {
    throw new Error(message);
  }
  return value;
}

function requireSignalResponse(value: SignalWorkflowRunResponse | undefined, message: string): SignalWorkflowRunResponse {
  if (value === undefined) {
    throw new Error(message);
  }
  return value;
}

function requireEvent(value: WorkflowEvent | undefined, message: string): WorkflowEvent {
  if (value === undefined) {
    throw new Error(message);
  }
  return value;
}
