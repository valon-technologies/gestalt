import {
  createClient,
  type Client,
  type Interceptor,
} from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import {
  WorkflowManagerHost as WorkflowManagerHostService,
} from "./internal/gen/v1/workflow_pb.ts";
import type { Request } from "./api.ts";
import { optionalStruct } from "./protocol-internal.ts";
import type { JsonObjectInput } from "./protocol.ts";
import {
  managedWorkflowDefinitionFromProto,
  managedWorkflowRunFromProto,
  managedWorkflowRunSignalFromProto,
  workflowDefinitionSpecToProto,
  workflowEventToProto,
  workflowManagerDeliverEventResponseFromProto,
  workflowSignalToProto,
  type DeliverWorkflowEventResponse,
  type ManagedWorkflowDefinition,
  type ManagedWorkflowRun,
  type ManagedWorkflowRunSignal,
  type WorkflowDefinitionSpec,
  type WorkflowEvent,
  type WorkflowSignal,
} from "./workflow.ts";

/**
 * Environment variable containing the workflow-manager host-service target.
 *
 * @internal
 */
export const ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET";
/**
 * Environment variable containing the optional workflow-manager relay token.
 *
 * @internal
 */
export const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN =
  `${ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN`;
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER =
  "x-gestalt-host-service-relay-token";

export interface WorkflowManagerApplyDefinition {
  providerName: string;
  spec?: WorkflowDefinitionSpec | undefined;
  idempotencyKey?: string | undefined;
}

export interface WorkflowManagerGetDefinition {
  definitionId: string;
}

export interface WorkflowManagerListDefinitions {
  providerName: string;
}

export interface WorkflowManagerDeleteDefinition {
  definitionId: string;
  generation?: bigint | number | undefined;
}

export interface WorkflowManagerSetDefinitionPaused {
  definitionId: string;
  paused: boolean;
}

export interface WorkflowManagerSetActivationPaused {
  definitionId: string;
  activationId: string;
  paused: boolean;
}

export interface WorkflowManagerStartRun {
  providerName: string;
  definitionId: string;
  definitionGeneration?: bigint | number | undefined;
  activationId?: string | undefined;
  workflowKey?: string | undefined;
  input?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
}

export interface WorkflowManagerSignalRun {
  runId: string;
  signal?: WorkflowSignal | undefined;
}

export interface WorkflowManagerSignalOrStartRun {
  providerName: string;
  definitionId: string;
  definitionGeneration?: bigint | number | undefined;
  activationId?: string | undefined;
  workflowKey?: string | undefined;
  input?: JsonObjectInput | undefined;
  idempotencyKey?: string | undefined;
  signal?: WorkflowSignal | undefined;
}

export interface WorkflowManagerCancelRun {
  runId: string;
  reason?: string | undefined;
}

export interface WorkflowManagerDeliverEvent {
  providerName: string;
  event?: WorkflowEvent | undefined;
  idempotencyKey?: string | undefined;
}

/**
 * Client for defining and controlling durable workflows.
 *
 * The constructor accepts either a Gestalt request or an invocation token. Each
 * manager call forwards that token to the host service. When constructed from a
 * request, mutating operations reuse the request idempotency key unless the call
 * provides one explicitly.
 */
export class WorkflowManager {
  private readonly client: Client<typeof WorkflowManagerHostService>;
  private readonly invocationToken: string;
  private readonly idempotencyKey: string;

  constructor(request: Request);
  constructor(invocationToken: string);
  constructor(requestOrToken: Request | string) {
    this.invocationToken = normalizeInvocationToken(requestOrToken);
    this.idempotencyKey = normalizeIdempotencyKey(requestOrToken);

    const target = process.env[ENV_WORKFLOW_MANAGER_SOCKET];
    if (!target) {
      throw new Error(
        `workflow manager: ${ENV_WORKFLOW_MANAGER_SOCKET} is not set`,
      );
    }
    const relayToken =
      process.env[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN]?.trim() ?? "";

    const transport = createGrpcTransport({
      ...workflowManagerTransportOptions(target),
      interceptors: relayToken
        ? [workflowManagerRelayTokenInterceptor(relayToken)]
        : [],
    });
    this.client = createClient(WorkflowManagerHostService, transport);
  }

  async applyDefinition(
    request: WorkflowManagerApplyDefinition,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.applyDefinition({
        providerName: request.providerName,
        spec: workflowDefinitionSpecToProto(request.spec),
        invocationToken: this.invocationToken,
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      }),
    );
  }

  async getDefinition(
    request: WorkflowManagerGetDefinition,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.getDefinition({
        definitionId: request.definitionId,
        invocationToken: this.invocationToken,
      }),
    );
  }

  async listDefinitions(
    request: WorkflowManagerListDefinitions,
  ): Promise<readonly ManagedWorkflowDefinition[]> {
    const response = await this.client.listDefinitions({
      providerName: request.providerName,
      invocationToken: this.invocationToken,
    });
    return response.definitions.map(managedWorkflowDefinitionFromProto);
  }

  async deleteDefinition(
    request: WorkflowManagerDeleteDefinition,
  ): Promise<void> {
    await this.client.deleteDefinition({
      definitionId: request.definitionId,
      generation: BigInt(request.generation ?? 0),
      invocationToken: this.invocationToken,
    });
  }

  async setDefinitionPaused(
    request: WorkflowManagerSetDefinitionPaused,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.setDefinitionPaused({
        definitionId: request.definitionId,
        paused: request.paused,
        invocationToken: this.invocationToken,
      }),
    );
  }

  async setActivationPaused(
    request: WorkflowManagerSetActivationPaused,
  ): Promise<ManagedWorkflowDefinition> {
    return managedWorkflowDefinitionFromProto(
      await this.client.setActivationPaused({
        definitionId: request.definitionId,
        activationId: request.activationId,
        paused: request.paused,
        invocationToken: this.invocationToken,
      }),
    );
  }

  async startRun(
    request: WorkflowManagerStartRun,
  ): Promise<ManagedWorkflowRun> {
    const input = optionalStruct(request.input);
    return managedWorkflowRunFromProto(
      await this.client.startRun({
        providerName: request.providerName,
        definitionId: request.definitionId,
        definitionGeneration: BigInt(request.definitionGeneration ?? 0),
        activationId: request.activationId ?? "",
        workflowKey: request.workflowKey ?? "",
        ...(input !== undefined ? { input } : {}),
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
        invocationToken: this.invocationToken,
      }),
    );
  }

  async signalRun(
    request: WorkflowManagerSignalRun,
  ): Promise<ManagedWorkflowRunSignal> {
    return managedWorkflowRunSignalFromProto(
      await this.client.signalRun({
        runId: request.runId,
        signal: workflowSignalToProto(request.signal),
        invocationToken: this.invocationToken,
      }),
    );
  }

  async signalOrStartRun(
    request: WorkflowManagerSignalOrStartRun,
  ): Promise<ManagedWorkflowRunSignal> {
    const input = optionalStruct(request.input);
    return managedWorkflowRunSignalFromProto(
      await this.client.signalOrStartRun({
        providerName: request.providerName,
        definitionId: request.definitionId,
        definitionGeneration: BigInt(request.definitionGeneration ?? 0),
        activationId: request.activationId ?? "",
        workflowKey: request.workflowKey ?? "",
        ...(input !== undefined ? { input } : {}),
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
        signal: workflowSignalToProto(request.signal),
        invocationToken: this.invocationToken,
      }),
    );
  }

  async cancelRun(
    request: WorkflowManagerCancelRun,
  ): Promise<ManagedWorkflowRun> {
    return managedWorkflowRunFromProto(
      await this.client.cancelRun({
        runId: request.runId,
        reason: request.reason ?? "",
        invocationToken: this.invocationToken,
      }),
    );
  }

  async deliverEvent(
    request: WorkflowManagerDeliverEvent,
  ): Promise<DeliverWorkflowEventResponse> {
    return workflowManagerDeliverEventResponseFromProto(
      await this.client.deliverEvent({
        providerName: request.providerName,
        event: workflowEventToProto(request.event),
        invocationToken: this.invocationToken,
        idempotencyKey: request.idempotencyKey?.trim() || this.idempotencyKey,
      }),
    );
  }
}

function normalizeInvocationToken(requestOrToken: Request | string): string {
  const invocationToken =
    typeof requestOrToken === "string"
      ? requestOrToken
      : requestOrToken.invocationToken;
  const trimmed = invocationToken.trim();
  if (!trimmed) {
    throw new Error("workflow manager: invocation token is not available");
  }
  return trimmed;
}

function normalizeIdempotencyKey(requestOrToken: Request | string): string {
  if (typeof requestOrToken === "string") {
    return "";
  }
  return requestOrToken.idempotencyKey.trim();
}

function workflowManagerTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("workflow manager: transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(
        `workflow manager: tcp target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(
        `workflow manager: tls target ${JSON.stringify(rawTarget)} is missing host:port`,
      );
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(
        `workflow manager: unix target ${JSON.stringify(rawTarget)} is missing a socket path`,
      );
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(
      `workflow manager: unsupported target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`,
    );
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

function workflowManagerRelayTokenInterceptor(token: string): Interceptor {
  return (next) => async (req) => {
    req.header.set(WORKFLOW_MANAGER_RELAY_TOKEN_HEADER, token);
    return next(req);
  };
}
